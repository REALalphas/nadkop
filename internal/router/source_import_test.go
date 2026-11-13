package router

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nadkop/internal/core"
)

func TestAddSourceCommitsOnlySourceAndRejectsBadImports(t *testing.T) {
	b := fixture(t)
	ctx := context.Background()
	path := filepath.Join(filepath.Dir(b.config), "provider.json")
	profile := []byte(`{"title":"My VPN + EU!","outbounds":[{"tag":"main","protocol":"trojan","settings":{"servers":[{"address":"example.test","port":443,"password":"source-password"}]}}]}`)
	if err := os.WriteFile(path, profile, 0600); err != nil {
		t.Fatal(err)
	}
	existing, _ := core.ParseSubscription("existing", profile)
	summaries, err := core.SyncServers(filepath.Join(filepath.Dir(b.config), "servers.json"), existing, core.Config{})
	if err != nil {
		t.Fatal(err)
	}
	originalID := summaries[0].ID
	// This source is configured, so importing another source must leave it live.
	configured, _ := json.Marshal(core.Config{Sources: []core.Source{{Name: "existing", File: filepath.Join(filepath.Dir(path), "existing.json")}}})
	if err := os.WriteFile(b.config, configured, 0600); err != nil {
		t.Fatal(err)
	}
	_, nodes, rev, _ := b.read()
	r := request{Content: path, Revision: rev}
	result, err := b.call(ctx, "addsource", r)
	if err != nil {
		t.Fatal(err)
	}
	reply := result.(map[string]any)
	source := reply["source"].(core.Source)
	if source.Name != "My_VPN_-_EU-" || source.File != path || reply["revision"] == rev {
		t.Fatal("source not committed")
	}
	if _, err = b.call(ctx, "addsource", r); err == nil {
		t.Fatal("stale revision accepted")
	}
	saved, savedNodes, rev, _ := b.read()
	if !bytes.Equal(nodes, savedNodes) || bytes.Contains(saved, []byte("source-password")) {
		t.Fatal("import copied payload or overwrote local nodes")
	}
	catalog, _ := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
	if len(catalog) != 2 || catalog[0].ID != originalID || !catalog[0].Available {
		t.Fatal("existing catalogue changed")
	}
	// Duplicate insertion is idempotent even if the source is currently offline.
	os.Remove(path)
	result, err = b.call(ctx, "addsource", request{Content: path, Revision: rev})
	if err != nil || result.(map[string]any)["existing"] != true {
		t.Fatal("duplicate failed", err)
	}
	for _, content := range []string{"/etc/shadow", "relative.json", "http://example.test/sub", "https://user:secret@example.test/sub", "/tmp/missing-provider.json"} {
		if _, err := b.call(ctx, "addsource", request{Content: content, Revision: rev}); err == nil {
			t.Fatalf("invalid import accepted: %s", content)
		}
	}
	after, afterNodes, afterRev, _ := b.read()
	if !bytes.Equal(saved, after) || !bytes.Equal(savedNodes, afterNodes) || afterRev != rev {
		t.Fatal("failed import changed config")
	}
	// Save can retain an explicit external JSON source even while it is missing.
	var c core.Config
	json.Unmarshal(saved, &c)
	c.Default = "block"
	encoded, _ := json.Marshal(c)
	if _, err := b.call(ctx, "save", request{Config: encoded, Nodes: savedNodes, Revision: rev}); err != nil {
		t.Fatal("external source could not be saved", err)
	}
}

func TestAddSourceFailedConfigWriteDoesNotReserveNamespace(t *testing.T) {
	b := fixture(t)
	path := filepath.Join(filepath.Dir(b.config), "provider.json")
	if err := os.WriteFile(path, []byte(`{"title":"Provider","outbounds":[{"tag":"main","protocol":"trojan","settings":{"servers":[{"address":"example.test","port":443,"password":"secret"}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	r := request{Content: path, Revision: rev}
	_, err = b.addSourceWith(context.Background(), r, func(string, []byte) error {
		return os.ErrPermission
	})
	if err == nil {
		t.Fatal("failed write accepted")
	}
	after, _, afterRev, err := b.read()
	if err != nil || !bytes.Equal(before, after) || rev != afterRev {
		t.Fatal("failed import changed config", err)
	}
	catalog, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
	if err != nil || len(catalog) != 0 {
		t.Fatal("failed import reserved catalogue records", catalog, err)
	}
	result, err := b.addSource(context.Background(), r)
	if err != nil || result.(map[string]any)["source"].(core.Source).Name != "Provider" {
		t.Fatal("retry changed namespace", result, err)
	}
}
