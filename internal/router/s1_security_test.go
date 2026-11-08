package router

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nadkop/internal/core"
)

const s1RouterHWID = "S1-SYNTHETIC-DEVICE-123"
const s1RouterSecret = "S1-SYNTHETIC-PASSWORD"
const s1RouterProfile = `{"outbounds":[{"tag":"exit","protocol":"trojan","settings":{"servers":[{"address":"127.0.0.1","port":443,"password":"S1-SYNTHETIC-PASSWORD"}]},"streamSettings":{"network":"tcp","security":"tls"}}]}`

func s1RouterFixture(t *testing.T) backend {
	t.Helper()
	dir := t.TempDir()
	b := backend{filepath.Join(dir, "config.json"), filepath.Join(dir, "nodes.json"), filepath.Join(dir, "run")}
	s1RouterWrite(t, b.config, []byte(`{"sources":[],"rules":[],"default":"block","socksPort":1080}`))
	s1RouterWrite(t, b.nodes, []byte(`[]`))
	return b
}
func s1RouterWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func s1RouterNoSecrets(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{s1RouterHWID, s1RouterSecret, "S1-SYNTHETIC-TOKEN", "S1-SYNTHETIC-AUTH", "S1-SYNTHETIC-RAW-OUTPUT", "S1-SYNTHETIC-ADDRESS"} {
		if bytes.Contains(data, []byte(s)) {
			t.Fatalf("public/error output contains synthetic secret marker %q", s)
		}
	}
}
func s1RouterArtifact(t *testing.T, name string, value any) {
	t.Helper()
	dir := os.Getenv("S1_ARTIFACT_DIR")
	if dir == "" {
		return
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := os.CreateTemp(dir, ".s1-artifact-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		t.Fatal(err)
	}
	if err = tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

func TestS1ACLMatchesRPCSecretBoundary(t *testing.T) {
	data, err := os.ReadFile("../../openwrt/luci-app-nadkop/root/usr/share/rpcd/acl.d/luci-app-nadkop.json")
	if err != nil {
		t.Fatal(err)
	}
	type grant struct {
		UBus map[string][]string `json:"ubus"`
	}
	var acl map[string]struct {
		Read  grant `json:"read"`
		Write grant `json:"write"`
	}
	if err = json.Unmarshal(data, &acl); err != nil {
		t.Fatal(err)
	}
	scope := acl["luci-app-nadkop"]
	if got := scope.Read.UBus["nadkop"]; len(got) != 1 || got[0] != "status" {
		t.Fatalf("unexpected read ACL: %v", got)
	}
	reads, writes := map[string]bool{}, map[string]bool{}
	for _, m := range scope.Read.UBus["nadkop"] {
		reads[m] = true
	}
	for _, m := range scope.Write.UBus["nadkop"] {
		writes[m] = true
	}
	for method := range signature {
		if method == "status" {
			continue
		}
		if reads[method] || !writes[method] {
			t.Fatalf("method %s lacks write-only ACL", method)
		}
	}
	if reads["*"] || writes["*"] || len(writes) != len(signature)-1 {
		t.Fatal("wildcard or incomplete ACL")
	}
	t.Logf("RPC methods=%d; read-only methods=1 (status); write-only methods=%d; actual rpcd session enforcement not exercised on host", len(signature), len(writes))
}

type s1CountReader struct {
	r io.Reader
	n int
}

func (r *s1CountReader) Read(p []byte) (int, error) { n, err := r.r.Read(p); r.n += n; return n, err }

func TestS1RPCObjectLimitsSessionsAndUnknownFields(t *testing.T) {
	invalid := []string{"", "null", "[]", "true", "{} {}", `{"unknown":"S1-SYNTHETIC-PASSWORD"}`, `{"options":{"unexpected":true}}`, `{"options":{"hwid":{"unexpected":true}}}`, `{"refs":42}`, `{"ubus_rpc_session":12}`, `{"content":"bad"`}
	for i, body := range invalid {
		var out bytes.Buffer
		if err := Run([]string{"call", "S1-unknown"}, strings.NewReader(body), &out); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if json.Unmarshal(out.Bytes(), &v) != nil || v["error"] != "Invalid or oversized request" {
			t.Fatalf("invalid case %d: %s", i, out.String())
		}
		s1RouterNoSecrets(t, v)
	}
	body := "{}" + strings.Repeat(" ", limit-2)
	for _, tc := range []struct {
		body  string
		error string
	}{{body, "Unknown method"}, {body + " ", "Invalid or oversized request"}} {
		var out bytes.Buffer
		if err := Run([]string{"call", "S1-unknown"}, strings.NewReader(tc.body), &out); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		_ = json.Unmarshal(out.Bytes(), &v)
		if v["error"] != tc.error {
			t.Fatal("wrong RPC size boundary")
		}
	}
	counted := &s1CountReader{r: strings.NewReader(body + strings.Repeat(" ", 1024))}
	var out bytes.Buffer
	if err := Run([]string{"call", "S1-unknown"}, counted, &out); err != nil {
		t.Fatal(err)
	}
	if counted.n != limit+1 {
		t.Fatalf("reader consumed %d instead of %d", counted.n, limit+1)
	}
	content := `[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":12345}]}}]`
	for _, session := range []string{"", "S1-SYNTHETIC-SESSION"} {
		req := map[string]any{"content": content}
		if session != "" {
			req["ubus_rpc_session"] = session
		}
		data, _ := json.Marshal(req)
		out.Reset()
		if err := Run([]string{"call", "import"}, bytes.NewReader(data), &out); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		_ = json.Unmarshal(out.Bytes(), &v)
		if v["nodes"] == nil || v["error"] != nil {
			t.Fatal("session protocol parsing failed")
		}
	}
	out.Reset()
	if err := Run([]string{"call", "S1-unknown"}, strings.NewReader(`{"content":"first","content":"second"}`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Unknown method") {
		t.Fatal("duplicate-key behavior changed")
	}
	if err := Run([]string{"invalid"}, strings.NewReader("{}"), io.Discard); err == nil {
		t.Fatal("invalid invocation accepted")
	}
	t.Logf("malformed/unknown/type cases=%d; exact RPC limit=%d; overflow consumes exactly %d bytes; session is rpcd-owned, accepted syntactically; duplicate fields accepted", len(invalid), limit, counted.n)
}

func TestS1ProbeValidationLimitsAndErrors(t *testing.T) {
	refs := []string{}
	for i := 0; i < 5; i++ {
		refs = append(refs, fmt.Sprintf("srv_%032x", i+1))
	}
	for _, mode := range []string{"priority", "ping", "url"} {
		r := request{Refs: refs[:4], Mode: mode, URL: "https://127.0.0.1:1/check"}
		if _, err := validateProbe(r); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []request{{Refs: nil, Mode: "url"}, {Refs: refs, Mode: "url"}, {Refs: []string{refs[0], refs[0]}, Mode: "url"}, {Refs: []string{"srv_SHORT"}, Mode: "priority"}, {Refs: []string{strings.ToUpper(refs[0])}, Mode: "priority"}, {Refs: refs[:1], Mode: "bad"}}
	for _, address := range []string{"file:///tmp/s1", "https://user:S1-SYNTHETIC-PASSWORD@127.0.0.1", "https://127.0.0.1/#S1-SYNTHETIC-TOKEN", "https://127.0.0.1/\n", "https://127.0.0.1/" + strings.Repeat("p", 2048), "https://127.0.0.1:bad/"} {
		invalid = append(invalid, request{Refs: refs[:1], Mode: "url", URL: address})
	}
	for i, r := range invalid {
		_, err := validateProbe(r)
		if err == nil {
			t.Fatalf("invalid probe case %d accepted", i)
		}
		s1RouterNoSecrets(t, rpcError(err))
	}
	b := s1RouterFixture(t)
	if _, err := b.call(context.Background(), "service", request{Action: "start; S1-SYNTHETIC-PASSWORD"}); err == nil {
		t.Fatal("service action injection accepted")
	}
	if _, err := os.Stat(b.runtime); !os.IsNotExist(err) {
		t.Fatal("invalid service action touched runtime")
	}
	t.Logf("valid 4-reference requests=3; invalid refs/mode/URL cases=%d; service metacharacters rejected before lock/command", len(invalid))
}

func TestS1StatusAndDiagnosticsRedactStoredSecrets(t *testing.T) {
	b := s1RouterFixture(t)
	dir := filepath.Dir(b.config)
	profilePath := filepath.Join(dir, "provider.json")
	s1RouterWrite(t, profilePath, []byte(s1RouterProfile))
	cfg := core.Config{Default: "block", Sources: []core.Source{{Name: "S1Local", File: profilePath}, {Name: "S1Remote", URL: "https://127.0.0.1:1/S1-SYNTHETIC-TOKEN", Headers: map[string]string{"Authorization": "S1-SYNTHETIC-AUTH"}, HWID: &core.HWIDSettings{Enabled: true, ID: s1RouterHWID}}}}
	data, _ := json.Marshal(cfg)
	s1RouterWrite(t, b.config, data)
	s1RouterWrite(t, b.nodes, []byte(s1RouterProfile))
	s1RouterWrite(t, filepath.Join(dir, "hwid"), []byte(s1RouterHWID+"\n"))
	value, err := b.call(context.Background(), "get", request{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(value)
	if !bytes.Contains(got, []byte("S1-SYNTHETIC-TOKEN")) || !bytes.Contains(got, []byte(s1RouterSecret)) {
		t.Fatal("positive control: write-only get did not expose synthetic stored data")
	}
	value, err = b.call(context.Background(), "identity", request{})
	if err != nil || value.(map[string]any)["id"] != s1RouterHWID {
		t.Fatal("write-only identity positive control failed")
	}
	current := filepath.Join(b.runtime, "current")
	if err := os.MkdirAll(current, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"xray.json", "health.json"} {
		s1RouterWrite(t, filepath.Join(current, name), []byte(s1RouterProfile+"S1-SYNTHETIC-TOKEN "+s1RouterHWID))
	}
	s1RouterWrite(t, filepath.Join(current, "runtime.json"), []byte(`{"transparent":{"enabled":false},"socksPort":1080,"updateIntervalHours":6}`))
	s1RouterWrite(t, filepath.Join(current, "zapret-hosts.txt"), []byte("S1-SYNTHETIC-ADDRESS\n"))
	queue := filepath.Join(dir, "queue")
	s1RouterWrite(t, queue, []byte("200 1 0 2 S1-SYNTHETIC-PASSWORD\n"))
	stub := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "/bin/ubus" {
			return []byte(`{"nadkop":{"instances":{"xray":{"running":true,"pid":123,"command":["S1-SYNTHETIC-AUTH"]},"monitor":{"running":true,"pid":124}}},"other":{"secret":"S1-SYNTHETIC-TOKEN"}}`), nil
		}
		return []byte("S1-SYNTHETIC-RAW-OUTPUT"), errors.New("S1-SYNTHETIC-RAW-OUTPUT")
	}
	value = b.statusWith(context.Background(), stub, queue)
	s1RouterNoSecrets(t, value)
	// Diagnose exclusively a synthetic local source; the failing runner never executes a host command.
	cfg.Sources = cfg.Sources[:1]
	data, _ = json.Marshal(cfg)
	s1RouterWrite(t, b.config, data)
	calls := 0
	report := b.diagnose(context.Background(), func(_ context.Context, name string, _ ...string) ([]byte, error) {
		calls++
		if name != "/usr/bin/xray" {
			t.Fatalf("unexpected command %s", name)
		}
		return []byte("S1-SYNTHETIC-RAW-OUTPUT " + s1RouterSecret), errors.New(s1RouterSecret)
	})
	if report.OK || calls != 1 || report.Error == "" {
		t.Fatal("diagnostic failure positive control failed")
	}
	s1RouterNoSecrets(t, report)
	s1RouterWrite(t, profilePath, []byte(`{"outbounds":[{"tag":"x","protocol":"trojan","settings":[],"password":"S1-SYNTHETIC-PASSWORD"}]}`))
	report = b.diagnose(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("command after malformed local subscription")
		return nil, nil
	})
	if report.OK {
		t.Fatal("malformed subscription accepted")
	}
	s1RouterNoSecrets(t, report)
	s1RouterWrite(t, b.config, []byte("S1-SYNTHETIC-PASSWORD invalid JSON"))
	_, err = b.call(context.Background(), "get", request{})
	if err == nil {
		t.Fatal("corrupt JSON reflected")
	}
	s1RouterNoSecrets(t, rpcError(err))
	t.Log("write-only get/identity positive controls contain synthetic secrets; status and diagnostics omit URL token/auth/HWID/password/raw command output; malformed disk JSON not reflected")
}

func TestS1SaveAndAddSourceRejectUnsafeFilesAndStaleRevisions(t *testing.T) {
	b := s1RouterFixture(t)
	before, nodes, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(b.config)
	target := filepath.Join(dir, "provider.json")
	s1RouterWrite(t, target, []byte(s1RouterProfile))
	link := filepath.Join(dir, "link.json")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{link, dir + "/../provider.json", "relative.json", dir + "/bad.txt", "http://127.0.0.1:1", "https://user:S1-SYNTHETIC-PASSWORD@127.0.0.1:1"} {
		_, err = b.call(context.Background(), "addsource", request{Content: content, Revision: rev})
		if err == nil {
			t.Fatal("unsafe source accepted")
		}
		s1RouterNoSecrets(t, rpcError(err))
	}
	_, err = b.call(context.Background(), "save", request{Config: json.RawMessage(`{}`), Nodes: json.RawMessage(`[]`), Revision: "S1-stale"})
	if err == nil {
		t.Fatal("stale save accepted")
	}
	_, err = b.call(context.Background(), "addsource", request{Content: target, Revision: "S1-stale"})
	if err == nil {
		t.Fatal("stale addsource accepted")
	}
	after, afterNodes, afterRev, _ := b.read()
	if !bytes.Equal(before, after) || !bytes.Equal(nodes, afterNodes) || rev != afterRev {
		t.Fatal("negative requests changed stored state")
	}
	value, err := b.call(context.Background(), "addsource", request{Content: target, Revision: rev})
	if err != nil {
		t.Fatal(err)
	}
	rev = value.(map[string]any)["revision"].(string)
	value, err = b.call(context.Background(), "addsource", request{Content: target, Revision: rev})
	if err != nil || value.(map[string]any)["existing"] != true {
		t.Fatal("duplicate source not idempotent")
	}
	_, err = b.call(context.Background(), "save", request{Config: json.RawMessage(`{"sources":[],"rules":[],"default":"block"}`), Nodes: json.RawMessage(`[]`), Revision: rev})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{b.config, b.nodes} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatal("saved files not private")
		}
	}
	t.Log("unsafe file/URL imports, stale revisions rejected without changes; valid source and duplicate/save controls pass; stored config/nodes mode=0600")
}

// This child is only activated by the TLS parent and gets a fresh x509 system-root
// cache. SSL_CERT_FILE trusts this fixture's local certificate; production uses
// its original nil transport. No global HTTP transport or CA state is changed.
func TestS1RouterTLSChild(t *testing.T) {
	scenario := os.Getenv("S1_CHILD_CASE")
	if scenario == "" {
		t.Skip("isolated helper requires synthetic fixture environment")
	}
	b := backend{os.Getenv("S1_CHILD_CONFIG"), os.Getenv("S1_CHILD_NODES"), os.Getenv("S1_CHILD_RUNTIME")}
	r := request{Content: os.Getenv("S1_CHILD_URL"), Options: core.Source{HWID: &core.HWIDSettings{Enabled: true, ID: s1RouterHWID}, Headers: map[string]string{"Authorization": "S1-SYNTHETIC-AUTH"}}}
	switch scenario {
	case "max-url":
		r.Content += strings.Repeat("p", 4096-len(r.Content))
	case "long-url":
		r.Content += "/" + strings.Repeat("p", 4096)
	case "long-title":
		r.Options.Title = strings.Repeat("T", 81)
	case "control-title":
		r.Options.Title = "S1\nTITLE"
	}
	before, _, revision, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	r.Revision = revision
	input, _ := json.Marshal(r)
	if len(input) > limit {
		t.Fatal("repro exceeds RPC envelope limit")
	}
	value, callErr := b.call(context.Background(), "addsource", r)
	saved, _, _, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	_, parseErr := core.ReadConfig(bytes.NewReader(saved))
	result := map[string]any{"case": scenario, "inputBytes": len(input), "urlBytes": len(r.Content), "savedBytes": len(saved), "addsourceOK": callErr == nil, "savedConfigReadable": parseErr == nil}
	if callErr != nil {
		result["addsourceError"] = callErr.Error()
		s1RouterNoSecrets(t, rpcError(callErr))
	}
	if parseErr != nil {
		result["savedConfigError"] = parseErr.Error()
	}
	if scenario != "valid" && scenario != "max-url" {
		if callErr == nil || !bytes.Equal(before, saved) {
			t.Fatal("invalid source accepted or changed saved configuration")
		}
	} else {
		if callErr != nil {
			t.Fatalf("TLS addsource failed: %v", callErr)
		}
		if value.(map[string]any)["source"].(core.Source).URL != r.Content {
			t.Fatal("source not saved")
		}
		_, _, loadErr := b.load(context.Background())
		result["backendLoadOK"] = loadErr == nil
		if loadErr != nil {
			result["backendLoadError"] = loadErr.Error()
		}
		if parseErr != nil || loadErr != nil {
			t.Fatal("valid source did not reload")
		}
	}
	data, _ := json.Marshal(result)
	s1RouterWrite(t, os.Getenv("S1_CHILD_RESULT"), data)
}

func TestS1ReproAddSourcePublishesInvalidConfig(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "S1-SYNTHETIC-AUTH" || r.Header.Get("X-HWID") != s1RouterHWID {
			t.Error("synthetic authorization/HWID missing")
		}
		if r.URL.Path == "/malformed" {
			fmt.Fprint(w, `{"outbounds":[{"tag":"exit","protocol":"trojan","settings":[],"password":"S1-SYNTHETIC-PASSWORD"}]}`)
			return
		}
		fmt.Fprint(w, s1RouterProfile)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "max-url", "malformed", "long-url", "long-title", "control-title"} {
		t.Run(scenario, func(t *testing.T) {
			b := s1RouterFixture(t)
			dir := filepath.Dir(b.config)
			ca := filepath.Join(dir, "s1-ca.pem")
			s1RouterWrite(t, ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			emptyRoots := filepath.Join(dir, "ca-dir")
			if err := os.Mkdir(emptyRoots, 0700); err != nil {
				t.Fatal(err)
			}
			resultPath := filepath.Join(dir, "result.json")
			address := server.URL + "/" + scenario
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestS1RouterTLSChild$", "-test.count=1", "-test.timeout=4s")
			for _, env := range os.Environ() {
				name, _, _ := strings.Cut(env, "=")
				if !strings.HasPrefix(name, "S1_CHILD_") && name != "SSL_CERT_FILE" && name != "SSL_CERT_DIR" && !strings.HasSuffix(strings.ToUpper(name), "_PROXY") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			cmd.Env = append(cmd.Env, "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+emptyRoots, "S1_CHILD_CASE="+scenario, "S1_CHILD_CONFIG="+b.config, "S1_CHILD_NODES="+b.nodes, "S1_CHILD_RUNTIME="+b.runtime, "S1_CHILD_URL="+address, "S1_CHILD_RESULT="+resultPath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("isolated TLS child: %v: %s", err, output)
			}
			data, err := os.ReadFile(resultPath)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err = json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			s1RouterArtifact(t, "s1-addsource-"+scenario+".json", result)
			if scenario == "valid" || scenario == "max-url" || scenario == "malformed" {
				t.Logf("CONTROL %s", data)
			} else {
				t.Logf("REJECTED S1-F1 %s", data)
			}
		})
	}
	t.Logf("local HTTPS requests=%d; valid and 4096-byte URL controls fetched/reloaded; invalid write-ACL inputs rejected before saving", requests.Load())
}

func TestS1ReproSaveSerializedConfigExceedsReadLimit(t *testing.T) {
	b := s1RouterFixture(t)
	_, _, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	domains := make([]string, 16384)
	for i := range domains {
		domains[i] = fmt.Sprintf("%05d.", i) + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + "." + strings.Repeat("d", 58)
	}
	compact, err := json.Marshal(core.Config{Default: "block", Sources: []core.Source{}, Rules: []core.Rule{{Action: "block", Domains: domains}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ReadConfig(bytes.NewReader(compact)); err != nil {
		t.Fatalf("invalid repro config: %v", err)
	}
	r := request{Config: compact, Nodes: json.RawMessage(`[]`), Revision: rev}
	input, _ := json.Marshal(r)
	if len(input) > limit {
		t.Fatalf("repro RPC input %d exceeds %d", len(input), limit)
	}
	before := closureC1Files(t, b)
	_, err = b.call(context.Background(), "save", r)
	if err == nil || !strings.Contains(err.Error(), "exceed 4 MiB") {
		t.Fatalf("oversized config not rejected: %v", err)
	}
	closureC1SameFiles(t, b, before)
	t.Logf("REJECTED S1-F2: compact config %d bytes, RPC %d bytes; config/nodes/catalogue unchanged", len(compact), len(input))
}

func TestS1ReproSaveSerializedNodesExceedReadLimit(t *testing.T) {
	b := s1RouterFixture(t)
	_, _, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	endpoints := make([]map[string]any, 8192)
	for i := range endpoints {
		endpoints[i] = map[string]any{"address": "127.0.0.1", "port": 443, "password": strings.Repeat("S", 420)}
	}
	nodes, err := json.Marshal([]map[string]any{{"tag": "exit", "protocol": "trojan", "settings": map[string]any{"servers": endpoints}}})
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(core.Config{Default: "block", Sources: []core.Source{{Name: "S1Local", File: b.nodes}}, Rules: []core.Rule{}})
	if err != nil {
		t.Fatal(err)
	}
	r := request{Config: config, Nodes: nodes, Revision: rev}
	input, _ := json.Marshal(r)
	if len(input) > limit {
		t.Fatal("repro exceeds RPC request limit")
	}
	before := closureC1Files(t, b)
	if _, err := b.call(context.Background(), "save", r); err == nil || !strings.Contains(err.Error(), "exceed 4 MiB") {
		t.Fatalf("oversized nodes not rejected: %v", err)
	}
	closureC1SameFiles(t, b, before)
	t.Logf("REJECTED S1-F2: compact nodes %d bytes, RPC %d bytes; config/nodes/catalogue unchanged", len(nodes), len(input))
}
