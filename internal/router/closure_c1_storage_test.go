package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"nadkop/internal/core"
)

type closureC1File struct {
	data   []byte
	info   os.FileInfo
	exists bool
}

func closureC1Files(t *testing.T, b backend) map[string]closureC1File {
	t.Helper()
	out := map[string]closureC1File{}
	for _, p := range []string{b.config, b.nodes, filepath.Join(filepath.Dir(b.config), "servers.json")} {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			out[p] = closureC1File{}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = closureC1File{data, info, true}
	}
	return out
}
func closureC1SameFiles(t *testing.T, b backend, before map[string]closureC1File) {
	t.Helper()
	after := closureC1Files(t, b)
	for p, old := range before {
		n := after[p]
		if old.exists != n.exists || old.exists && (!bytes.Equal(old.data, n.data) || !os.SameFile(old.info, n.info) || !old.info.ModTime().Equal(n.info.ModTime()) || old.info.Mode() != n.info.Mode()) {
			t.Fatal("rejected operation changed file", p)
		}
	}
}
func closureC1RouterCatalogue(t *testing.T, b backend) []core.ServerSummary {
	t.Helper()
	n, err := core.ParseSubscription("provider", []byte(s1RouterProfile))
	if err != nil {
		t.Fatal(err)
	}
	ss, err := core.SyncServers(filepath.Join(filepath.Dir(b.config), "servers.json"), n, core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func TestClosureC1AddSourceRejectsOptionsAndURLBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name, location string
		options        core.Source
	}{
		{"long-url", "https://example.invalid/" + strings.Repeat("p", 4096), core.Source{}},
		{"long-title", "https://example.invalid/sub", core.Source{Title: strings.Repeat("T", 81)}},
		{"control-title", "https://example.invalid/sub", core.Source{Title: "bad\ntitle"}},
		{"unexpected-title", "https://example.invalid/sub", core.Source{Title: "Valid but not an import option"}},
		{"empty-hostname", "https://:443/sub", core.Source{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fixture(t)
			closureC1RouterCatalogue(t, b)
			before := closureC1Files(t, b)
			_, _, rev, _ := b.read()
			_, err := b.call(context.Background(), "addsource", request{Content: tc.location, Options: tc.options, Revision: rev})
			if err == nil {
				t.Fatal("invalid import accepted")
			}
			closureC1SameFiles(t, b, before)
		})
	}
}

func closureC1ConfigAtSize(t *testing.T, target int) []byte {
	t.Helper()
	ds := make([]string, 16000)
	for i := range ds {
		ds[i] = fmt.Sprintf("%05d.", i) + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + "." + strings.Repeat("d", 58)
	}
	c := core.Config{Default: "block", Sources: []core.Source{}, Rules: []core.Rule{{Action: "block", Domains: ds}}}
	compact, _ := json.Marshal(c)
	c, err := core.ReadConfig(bytes.NewReader(compact))
	if err != nil {
		t.Fatal(err)
	}
	indented, _ := json.MarshalIndent(c, "", "  ")
	remaining := target - len(indented) - 1
	if remaining < 0 || remaining > 5*len(ds) {
		t.Fatal("cannot create config boundary", remaining)
	}
	for i := range c.Rules[0].Domains {
		n := remaining
		if n > 5 {
			n = 5
		}
		c.Rules[0].Domains[i] += strings.Repeat("d", n)
		remaining -= n
	}
	compact, _ = json.Marshal(c)
	return compact
}
func closureC1NodesAtSize(t *testing.T, target int) []byte {
	t.Helper()
	endpoints := make([]map[string]any, 100)
	for i := range endpoints {
		endpoints[i] = map[string]any{"address": "127.0.0.1", "port": 443, "password": "synthetic"}
	}
	makeRaw := func() []byte {
		raw, _ := json.Marshal([]map[string]any{{"tag": "exit", "protocol": "trojan", "settings": map[string]any{"servers": endpoints}}})
		return raw
	}
	nodes, err := parseNodes(makeRaw())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.MarshalIndent(nodes, "", "  ")
	extra := target - len(out) - 1
	if extra < 0 {
		t.Fatal("bad node boundary")
	}
	endpoints[0]["password"] = "synthetic" + strings.Repeat("x", extra)
	return makeRaw()
}

func TestClosureC1SaveExactSerializedBudgets(t *testing.T) {
	for _, kind := range []string{"config", "nodes"} {
		for _, size := range []int{limit - 1, limit, limit + 1} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				b := fixture(t)
				closureC1RouterCatalogue(t, b)
				config := []byte(`{"sources":[],"rules":[],"default":"block"}`)
				rawNodes := []byte(`[]`)
				if kind == "config" {
					config = closureC1ConfigAtSize(t, size)
				} else {
					rawNodes = closureC1NodesAtSize(t, size)
					config, _ = json.Marshal(core.Config{Default: "block", Sources: []core.Source{{Name: "local", File: b.nodes}}, Rules: []core.Rule{}})
				}
				c, err := core.ReadConfig(bytes.NewReader(config))
				if err != nil {
					t.Fatal("invalid compact candidate", err)
				}
				nodes, err := parseNodes(rawNodes)
				if err != nil {
					t.Fatal(err)
				}
				var expected []byte
				if kind == "config" {
					expected, _ = json.MarshalIndent(c, "", "  ")
				} else {
					expected, _ = json.MarshalIndent(nodes, "", "  ")
				}
				if len(expected)+1 != size {
					t.Fatal("wrong serialized fixture", len(expected)+1, size)
				}
				_, _, rev, _ := b.read()
				req := request{Config: config, Nodes: rawNodes, Revision: rev}
				envelope, _ := json.Marshal(req)
				if len(envelope) > limit {
					t.Fatal("request already exceeds envelope budget", len(envelope))
				}
				before := closureC1Files(t, b)
				_, err = b.call(context.Background(), "save", req)
				if size > limit {
					if err == nil || !strings.Contains(err.Error(), "exceed 4 MiB") {
						t.Fatal("oversized serialization accepted", err)
					}
					closureC1SameFiles(t, b, before)
					return
				}
				if err != nil {
					t.Fatal("valid serialized boundary rejected", err)
				}
				savedC, savedN, _, err := b.read()
				if err != nil {
					t.Fatal(err)
				}
				actual := savedC
				if kind == "nodes" {
					actual = savedN
				}
				if len(actual) != size {
					t.Fatal("wrong published size", len(actual), size)
				}
				if _, _, err = b.load(context.Background()); err != nil {
					t.Fatal("accepted boundary cannot reload", err)
				}
			})
		}
	}
}

func TestClosureC1SavePreflightsCatalogueBeforeConfigAndNodes(t *testing.T) {
	for _, kind := range []string{"budget", "missing-pin", "missing-inactive"} {
		t.Run(kind, func(t *testing.T) {
			b := fixture(t)
			ss := closureC1RouterCatalogue(t, b)
			path := filepath.Join(filepath.Dir(b.config), "servers.json")
			ref := ss[0].ID
			if kind == "budget" {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var cat map[string]any
				if err = json.Unmarshal(raw, &cat); err != nil {
					t.Fatal(err)
				}
				record := cat["servers"].([]any)[0].(map[string]any)
				compact, _ := json.Marshal(cat)
				record["name"] = record["name"].(string) + strings.Repeat("x", limit-len(compact)-1)
				compact, _ = json.Marshal(cat)
				compact = append(compact, '\n')
				if len(compact) != limit {
					t.Fatal("wrong catalogue boundary")
				}
				if err = os.WriteFile(path, compact, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				ref = "srv_00000000000000000000000000000000"
			}
			c := core.Config{Default: "block", Sources: []core.Source{}, Rules: []core.Rule{}, Picker: core.PickerSettings{Pinned: []string{ref}}}
			if kind == "missing-inactive" {
				c.Picker.Pinned = nil
				c.Rules = []core.Rule{{Action: "block", Domains: []string{"inactive.invalid"}, NodeRef: ref}}
			}
			config, _ := json.Marshal(c)
			_, _, rev, _ := b.read()
			before := closureC1Files(t, b)
			_, err := b.call(context.Background(), "save", request{Config: config, Nodes: json.RawMessage(`[]`), Revision: rev})
			if err == nil {
				t.Fatal("invalid catalogue candidate accepted")
			}
			closureC1SameFiles(t, b, before)
		})
	}
}

func TestClosureC1RPCSharesConfigurationLock(t *testing.T) {
	b := fixture(t)
	_, _, rev, _ := b.read()
	lockPath := filepath.Join(filepath.Dir(b.config), ".state.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	before := closureC1Files(t, b)
	for _, method := range []string{"save", "nodes", "addsource", "check", "service", "probe"} {
		_, err = b.call(context.Background(), method, request{Revision: rev, Config: json.RawMessage(`{"rules":[],"sources":[]}`), Nodes: json.RawMessage(`[]`), Action: "restart", Content: "/missing.json", Refs: []string{"srv_00000000000000000000000000000000"}, Mode: "ping"})
		if err == nil || !strings.Contains(err.Error(), "Another configuration operation") {
			t.Fatal(method, "did not share CLI lock", err)
		}
		closureC1SameFiles(t, b, before)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	// Failed outer acquisition must also release ui.lock, so the retry can save.
	if _, err = b.call(context.Background(), "save", request{Revision: rev, Config: json.RawMessage(`{"rules":[],"sources":[]}`), Nodes: json.RawMessage(`[]`)}); err != nil {
		t.Fatal("retry deadlocked or ui.lock leaked", err)
	}
}

func TestClosureC1AddSourceChecksFutureConfigBeforeCatalogueWrites(t *testing.T) {
	b := fixture(t)
	// The existing compact config is readable, but its normalized on-disk form
	// leaves only one byte. Any added source must be rejected before pruning IDs.
	config := closureC1ConfigAtSize(t, limit-1)
	if err := os.WriteFile(b.config, config, 0600); err != nil {
		t.Fatal(err)
	}
	closureC1RouterCatalogue(t, b)
	path := filepath.Join(filepath.Dir(b.config), "new-provider.json")
	if err := os.WriteFile(path, []byte(s1RouterProfile), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, rev, _ := b.read()
	before := closureC1Files(t, b)
	_, err := b.call(context.Background(), "addsource", request{Content: path, Revision: rev})
	if err == nil || !strings.Contains(err.Error(), "exceed 4 MiB") {
		t.Fatal("future config overflow not rejected", err)
	}
	closureC1SameFiles(t, b, before)
}

func TestClosureC1AddSourceCatalogueOverflowKeepsConfigAndNodes(t *testing.T) {
	b := fixture(t)
	closureC1RouterCatalogue(t, b)
	entries := make([]map[string]any, 4096)
	for i := range entries {
		entries[i] = map[string]any{"tag": strings.Repeat("s4", 256) + fmt.Sprintf("-node-%05d", i), "protocol": "socks", "settings": map[string]any{"servers": []map[string]any{{"address": "127.0.0.1", "port": 1000 + i}}}}
	}
	raw, _ := json.Marshal(entries)
	if len(raw) > limit {
		t.Fatal("source exceeds reader budget")
	}
	path := filepath.Join(filepath.Dir(b.config), "oversized-catalogue.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, rev, _ := b.read()
	before := closureC1Files(t, b)
	_, err := b.call(context.Background(), "addsource", request{Content: path, Revision: rev})
	if err == nil || !strings.Contains(err.Error(), "catalogue exceeds 4 MiB") {
		t.Fatal("future catalogue overflow not rejected", err)
	}
	closureC1SameFiles(t, b, before)
}

func TestClosureC1SaveRetainsKnownInactiveBindingsAndDraftFields(t *testing.T) {
	b := fixture(t)
	ss := closureC1RouterCatalogue(t, b)
	_, _, rev, _ := b.read()
	raw := json.RawMessage(fmt.Sprintf(`{"rules":[{"domains":["active.test"],"ips":["unfinished IP draft"],"sets":["geosite:missing"],"conditions":{"domains":true,"ips":false,"sets":false},"nodeRef":%q,"selection":{"mode":"url","url":"https://keep.test/check"},"action":"block"}]}`, ss[0].ID))
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
	if rule.IPs[0] != "unfinished IP draft" || rule.Sets[0] != "geosite:missing" || rule.NodeRef != ss[0].ID || rule.Selection.URL != "https://keep.test/check" || rule.Conditions.IPs {
		t.Fatal("inactive fields changed", rule)
	}
	if _, err := core.Compile(restored, nil); err != nil {
		t.Fatal("inactive fields blocked compile", err)
	}
	summaries, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
	if err != nil || len(summaries) != 1 || summaries[0].ID != ss[0].ID || summaries[0].Available {
		t.Fatal("inactive selected ID not retained", err)
	}
}
