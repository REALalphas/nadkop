package router

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nadkop/internal/core"
)

func fixture(t *testing.T) backend {
	t.Helper()
	dir := t.TempDir()
	b := backend{filepath.Join(dir, "config.json"), filepath.Join(dir, "nodes.json"), filepath.Join(dir, "run")}
	if err := os.WriteFile(b.config, []byte(`{"sources":[],"rules":[],"default":"direct","socksPort":1080}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.nodes, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSaveSharedAndMissingServerReferences(t *testing.T) {
	b := fixture(t)
	raw := []byte(`[{"tag":"named","protocol":"trojan","settings":{"servers":[{"address":"private.example","port":443,"password":"secret"}]}}]`)
	nodes, err := core.ParseSubscription("provider", raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(b.config), "servers.json")
	servers, err := core.SyncServers(path, nodes, core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ref := servers[0].ID
	// Missing nodes remain valid editable references; applying is a separate
	// operation and must fail until the connection returns.
	if _, err := core.SyncServers(path, nil, core.Config{Picker: core.PickerSettings{Pinned: []string{ref}}}); err != nil {
		t.Fatal(err)
	}
	_, _, rev, _ := b.read()
	cfg, _ := json.Marshal(map[string]any{"sources": []any{}, "rules": []any{
		map[string]any{"domains": []string{"one.example"}, "action": "proxy", "nodeRef": ref},
		map[string]any{"domains": []string{"two.example"}, "action": "proxy", "nodeRef": ref},
	}, "picker": map[string]any{"pinned": []string{ref}, "order": []string{ref}}})
	if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: json.RawMessage(`[]`), Revision: rev}); err != nil {
		t.Fatal(err)
	}
	saved, _, rev, _ := b.read()
	if bytes.Contains(saved, []byte("password")) || bytes.Count(saved, []byte(`"nodeRef"`)) != 2 {
		t.Fatal("rule contains a copied node instead of references")
	}
	// Legacy saved tags migrate to the same object, even while it is missing.
	cfg = []byte(`{"sources":[],"rules":[{"domains":["legacy.example"],"action":"proxy","node":"provider/named"}]}`)
	if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: json.RawMessage(`[]`), Revision: rev}); err != nil {
		t.Fatal(err)
	}
	saved, _, rev, _ = b.read()
	if !bytes.Contains(saved, []byte(ref)) || bytes.Contains(saved, []byte(`"node":`)) {
		t.Fatal("legacy reference not migrated")
	}
	cfg = []byte(`{"sources":[],"rules":[{"domains":["unknown.example"],"action":"proxy","nodeRef":"srv_00000000000000000000000000000000"}]}`)
	if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: json.RawMessage(`[]`), Revision: rev}); err == nil {
		t.Fatal("unknown reference accepted")
	}
	result, err := b.call(context.Background(), "save", request{Config: json.RawMessage(`{"sources":[],"rules":[]}`), Nodes: json.RawMessage(`[]`), Revision: rev})
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := core.ServerSummaries(path)
	if err != nil || len(remaining) != 0 || len(result.(map[string]any)["servers"].([]core.ServerSummary)) != 0 {
		t.Fatal("save did not clean the last unbound missing reference")
	}
}

func TestSaveRevisionAndValidation(t *testing.T) {
	b := fixture(t)
	ctx := context.Background()
	_, _, rev, _ := b.read()
	req := request{Config: json.RawMessage(`{"sources":[],"rules":[],"default":"block","socksPort":2080}`), Nodes: json.RawMessage(`[]`), Revision: rev}
	if _, err := b.call(ctx, "save", req); err != nil {
		t.Fatal(err)
	}
	if _, err := b.call(ctx, "save", req); err == nil {
		t.Fatal("stale save accepted")
	}
	c, n, newRev, _ := b.read()
	if !bytes.Contains(c, []byte(`"block"`)) || newRev == rev {
		t.Fatal("save not published")
	}
	for _, path := range []string{b.config, b.nodes} {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("secret permissions")
		}
	}
	req.Revision = newRev
	req.Config = json.RawMessage(`{"sources":[{"name":"leak","file":"/etc/shadow"}],"rules":[]}`)
	if _, err := b.call(ctx, "save", req); err == nil {
		t.Fatal("arbitrary local path accepted")
	}
	req.Config = json.RawMessage(`{"sources":[],"rules":[{"domains":["example.org"],"action":"proxy","node":"missing/node"}]}`)
	if _, err := b.call(ctx, "save", req); err == nil {
		t.Fatal("missing node accepted")
	}
	c2, n2, _, _ := b.read()
	if !bytes.Equal(c, c2) || !bytes.Equal(n, n2) {
		t.Fatal("invalid save changed files")
	}
}

func TestImportAndRedaction(t *testing.T) {
	b := fixture(t)
	raw := `[{"tag":"test","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080,"users":[{"user":"secret-user","pass":"secret-pass"}]}]}}]`
	result, err := b.call(context.Background(), "import", request{Content: raw})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if !bytes.Contains(data, []byte(`"tag":"test"`)) || bytes.Contains(data, []byte(`local/test`)) {
		t.Fatal(string(data))
	}
	if err := os.WriteFile(b.nodes, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"sources":[{"name":"manual","file":"` + b.nodes + `"}],"rules":[]}`
	if err := os.WriteFile(b.config, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = b.call(context.Background(), "nodes", request{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(result)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "127.0.0.1") || !strings.Contains(string(data), "manual/test") {
		t.Fatal("unsafe node summary: " + string(data))
	}
}

func TestImportErrorExplainsCause(t *testing.T) {
	b := fixture(t)
	for _, tc := range []struct{ content, reason string }{
		{"", "empty subscription"},
		{"vless://private-user@private-host:443?security=tls&type=unknown", "unsupported URI transport"},
		{"ss://broken@private-host:8388", "invalid Shadowsocks credentials"},
	} {
		_, err := b.call(context.Background(), "import", request{Content: tc.content})
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("missing import reason: %v", err)
		}
		if strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "private-host") {
			t.Fatal("import error leaked credentials or address")
		}
	}
}

func TestSaveEmptyLocalSource(t *testing.T) {
	b := fixture(t)
	_, _, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"sources": []map[string]string{{"name": "local", "file": b.nodes}}, "rules": []any{}})
	if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: json.RawMessage("[\n]"), Revision: rev}); err != nil {
		t.Fatal(err)
	}
	_, data, _, err := b.read()
	if err != nil || string(bytes.TrimSpace(data)) != "[]" {
		t.Fatalf("empty nodes must remain an array: %s, %v", data, err)
	}
	_, nodes, err := b.load(context.Background())
	if err != nil || len(nodes) != 0 {
		t.Fatalf("cannot reload an empty local source: %v", err)
	}
}

func TestProtocolAndLock(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"list"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var sig map[string]any
	if json.Unmarshal(out.Bytes(), &sig) != nil || sig["save"] == nil {
		t.Fatal("missing signature")
	}
	out.Reset()
	if err := Run([]string{"call", "service"}, strings.NewReader(`{"action":"restart; touch /tmp/pwn"}`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Unsupported service action") {
		t.Fatal(out.String())
	}
	b := fixture(t)
	unlock, err := b.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err = b.lock(); err == nil {
		t.Fatal("concurrent write accepted")
	}
}

func TestIdentityIsPersistentAndNotInStatus(t *testing.T) {
	b := fixture(t)
	result, err := b.call(context.Background(), "identity", request{})
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["id"].(string)
	result, err = b.call(context.Background(), "identity", request{})
	if err != nil || result.(map[string]any)["id"] != id {
		t.Fatal("RPC changed the installation ID")
	}
	result, err = b.call(context.Background(), "status", request{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), id) {
		t.Fatal("read-only status exposed the device ID")
	}
}

func TestFailedSubscriptionLoadDoesNotCleanCatalogue(t *testing.T) {
	b := fixture(t)
	nodes, err := core.ParseSubscription("provider", []byte(`[{"tag":"main","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(b.config), "servers.json")
	if _, err := core.SyncServers(path, nodes, core.Config{}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	cfg, _ := json.Marshal(core.Config{Sources: []core.Source{{Name: "provider", File: filepath.Join(filepath.Dir(b.config), "offline.json")}}})
	if err := os.WriteFile(b.config, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.load(context.Background()); err == nil {
		t.Fatal("offline source loaded")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("load failure removed cached servers")
	}
}

func TestSaveRejectsSubscriptionSeparators(t *testing.T) {
	b := fixture(t)
	nodes, err := core.ParseSubscription("provider", []byte(`[{"tag":"heading","protocol":"socks","settings":{"servers":[{"address":"0.0.0.0","port":1}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := core.SyncServers(filepath.Join(filepath.Dir(b.config), "servers.json"), nodes, core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, rev, _ := b.read()
	for _, rule := range []core.Rule{
		{Action: "proxy", Domains: []string{"test.example"}, NodeRef: servers[0].ID},
		{Action: "proxy", Domains: []string{"test.example"}, Node: servers[0].Tag},
	} {
		cfg, _ := json.Marshal(core.Config{Sources: []core.Source{{Name: "provider", URL: "https://example.test/sub"}}, Rules: []core.Rule{rule}})
		if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: json.RawMessage(`[]`), Revision: rev}); err == nil {
			t.Fatal("separator accepted as a proxy rule")
		}
	}
}

func TestSaveMultiServerRuleAndRetainItsMissingMembers(t *testing.T) {
	b := fixture(t)
	raw := []byte(`[{"tag":"A","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2081}]}},{"tag":"B","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2082}]}}]`)
	nodes, err := core.ParseSubscription("provider", raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(b.config), "servers.json")
	summaries, err := core.SyncServers(path, nodes, core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := core.Config{Default: "direct", SOCKSPort: 1080, Rules: []core.Rule{{Name: "Video", Action: "proxy", Domains: []string{"test.example"}, NodeRefs: []string{summaries[1].ID, summaries[0].ID}, Selection: &core.SelectionSettings{Mode: "url", URL: "https://probe.test/204"}}}}
	data, _ := json.Marshal(cfg)
	_, _, rev, _ := b.read()
	result, err := b.call(context.Background(), "save", request{Config: data, Nodes: json.RawMessage(`[]`), Revision: rev})
	if err != nil {
		t.Fatal(err)
	}
	saved, _, _, _ := b.read()
	var restored core.Config
	json.Unmarshal(saved, &restored)
	if len(restored.Rules[0].NodeRefs) != 2 || restored.Rules[0].NodeRefs[0] != summaries[1].ID || restored.Rules[0].Selection.URL != "https://probe.test/204" {
		t.Fatal("group order/policy lost")
	}
	if len(result.(map[string]any)["servers"].([]core.ServerSummary)) != 2 {
		t.Fatal("referenced members removed with source")
	}
}

func TestRuleValidationRPCResponse(t *testing.T) {
	_, err := core.ReadConfig(strings.NewReader(`{"rules":[{"domains":["one.test"],"action":"direct"},{"domains":[],"action":"block"}]}`))
	if err == nil {
		t.Fatal("accepted empty rule")
	}
	reply := rpcError(err)
	if reply["ruleIndex"] != 1 || reply["field"] != "matchers" || !strings.HasPrefix(reply["error"].(string), "Rule 2:") || reply["message"] == "" {
		t.Fatal(reply)
	}
}

func TestValidateDraftHasNoSideEffects(t *testing.T) {
	b := fixture(t)
	before, nodes, revision, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	result, err := b.call(context.Background(), "validate", request{Config: json.RawMessage(`{"rules":[{"domains":["example.test"],"action":"block"}]}`)})
	if err != nil || result.(map[string]any)["ok"] != true {
		t.Fatal(result, err)
	}
	if _, err = b.call(context.Background(), "validate", request{Config: json.RawMessage(`{"rules":[{"domains":[],"action":"block"}]}`)}); err == nil {
		t.Fatal("invalid draft accepted")
	}
	after, afterNodes, afterRevision, _ := b.read()
	if !bytes.Equal(before, after) || !bytes.Equal(nodes, afterNodes) || revision != afterRevision {
		t.Fatal("preflight modified configuration")
	}
}

func TestSaveRetainsDisabledFieldsAcrossReload(t *testing.T) {
	b := fixture(t)
	// A saved inactive selection still binds an existing catalogue identity.
	catalogue := []byte(`{"version":1,"servers":[{"id":"srv_11111111111111111111111111111111","source":"provider","name":"inactive","protocol":"socks","position":0,"tag":"provider/inactive","available":false,"fingerprint":"synthetic","endpoint":"synthetic"}]}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(b.config), "servers.json"), catalogue, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, rev, _ := b.read()
	raw := json.RawMessage(`{"rules":[{"domains":["active.test"],"ips":["unfinished IP draft"],"sets":["geosite:missing"],"conditions":{"domains":true,"ips":false,"sets":false},"nodeRef":"srv_11111111111111111111111111111111","selection":{"mode":"url","url":"https://keep.test/check"},"action":"block"}]}`)
	if _, err := b.call(context.Background(), "save", request{Config: raw, Nodes: json.RawMessage(`[]`), Revision: rev}); err != nil {
		t.Fatal(err)
	}
	disk, _, _, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := core.ReadConfig(bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	rule := restored.Rules[0]
	if rule.IPs[0] != "unfinished IP draft" || rule.Sets[0] != "geosite:missing" || rule.NodeRef == "" || rule.Selection.URL != "https://keep.test/check" || rule.Conditions.IPs {
		t.Fatal("inactive values changed across save/reload", rule)
	}
	if _, err = core.Compile(restored, nil); err != nil {
		t.Fatal("inactive draft blocked compile", err)
	}
}
