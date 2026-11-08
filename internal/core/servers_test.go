package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func serverFixture(t *testing.T, source string, profiles ...map[string]any) []Node {
	t.Helper()
	data, _ := json.Marshal(profiles)
	nodes, err := ParseSubscription(source, data)
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func profile(name, address, password string) map[string]any {
	return map[string]any{"remarks": name, "outbounds": []any{map[string]any{"tag": "proxy", "protocol": "trojan", "settings": map[string]any{"servers": []any{map[string]any{"address": address, "port": 443, "password": password}}}}}}
}

func refsByEndpoint(nodes []Node) map[string]string {
	result := map[string]string{}
	for _, n := range nodes {
		var settings struct{ Servers []struct{ Address string } }
		json.Unmarshal(n.Settings, &settings)
		if len(settings.Servers) > 0 {
			result[settings.Servers[0].Address] = n.serverID
		}
	}
	return result
}

func TestServerReferencesSurviveRefreshAndMissingNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes := serverFixture(t, "provider", profile("Same", "a.example", "secret-a"), profile("Same", "b.example", "secret-b"))
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	refs := refsByEndpoint(nodes)
	if refs["a.example"] == "" || refs["a.example"] == refs["b.example"] {
		t.Fatal("missing or merged references")
	}
	// Identical provider tags and labels change with ordering. Neither is an ID.
	nodes = serverFixture(t, "provider", profile("Renamed B", "b.example", "secret-b"), profile("Renamed A", "a.example", "secret-a"))
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	for address, ref := range refsByEndpoint(nodes) {
		if ref != refs[address] {
			t.Fatal("rename/reorder changed a reference")
		}
	}
	config := Config{Default: "direct", SOCKSPort: 1080, Rules: []Rule{{Action: "proxy", NodeRef: refs["b.example"], Domains: []string{"missing.example"}}}}
	nodes = serverFixture(t, "provider", profile("Renamed A", "a.example", "new-secret"))
	summaries, err := SyncServers(path, nodes, config)
	if err != nil || nodes[0].serverID != refs["a.example"] {
		t.Fatalf("credential rotation: %v", err)
	}
	missing := false
	for _, s := range summaries {
		if s.ID == refs["b.example"] && !s.Available {
			missing = true
		}
	}
	if !missing {
		t.Fatal("deleted server did not remain as a tombstone")
	}
	if _, err := Compile(config, nodes); err == nil {
		t.Fatal("missing reference silently switched to another server")
	}
	nodes = serverFixture(t, "provider", profile("B returns", "b.example", "secret-b"), profile("Renamed A", "new-a.example", "new-secret"))
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	if got := refsByEndpoint(nodes); got["b.example"] != refs["b.example"] || got["new-a.example"] != refs["a.example"] {
		t.Fatal("returning server or stable provider label lost its reference")
	}
	// One reference can be reused by independent rules without copying payloads.
	config.Rules = []Rule{{Action: "proxy", NodeRef: refs["a.example"], Domains: []string{"one.example"}}, {Action: "proxy", NodeRef: refs["a.example"], Domains: []string{"two.example"}}}
	artifacts, err := Compile(config, nodes)
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		Outbounds []any
		Routing   struct {
			Rules []struct{ OutboundTag string }
		}
	}
	json.Unmarshal(artifacts.Xray, &compiled)
	if len(compiled.Outbounds) != 4 || compiled.Routing.Rules[0].OutboundTag != compiled.Routing.Rules[1].OutboundTag {
		t.Fatal("shared reference duplicated a payload or routed differently")
	}
	public, _ := json.Marshal(summaries)
	if strings.Contains(string(public), "secret") || strings.Contains(string(public), "a.example") || strings.Contains(string(public), "fingerprint") {
		t.Fatal("public picker exposed private matching data")
	}
}

func TestServerMatchingNeverGuessesAmbiguousEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes := serverFixture(t, "provider", profile("Account A", "shared.example", "a"), profile("Account B", "shared.example", "b"))
	old, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	nodes = serverFixture(t, "provider", profile("Unknown C", "shared.example", "c"), profile("Unknown D", "shared.example", "d"))
	next, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, prior := range old {
		for _, current := range next {
			if prior.ID == current.ID && current.Available {
				t.Fatal("ambiguous endpoint rebound a saved reference")
			}
		}
	}
	// Even exact payloads from a different subscription are separate objects.
	nodes = serverFixture(t, "other", profile("Account A", "shared.example", "a"))
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	for _, prior := range old {
		if prior.ID == nodes[0].serverID {
			t.Fatal("source boundary ignored")
		}
	}
}

func TestServerGraphIdentityAndFlashWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes, err := ParseSubscription("provider", []byte(profileFixture))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := SyncServers(path, nodes, Config{})
	if err != nil || len(servers) != 2 {
		t.Fatalf("helpers leaked into the picker: %d, %v", len(servers), err)
	}
	refs := []string{nodes[0].serverID, nodes[2].serverID}
	changed := strings.ReplaceAll(strings.ReplaceAll(profileFixture, "fragment", "new-dialer"), "First / profile", "Renamed profile")
	nodes, err = ParseSubscription("provider", []byte(changed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	if nodes[0].serverID != refs[0] || nodes[2].serverID != refs[1] {
		t.Fatal("graph reference names affected connection identity")
	}
	stamp := time.Unix(1234567890, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(path, nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.ModTime().Equal(stamp) || stat.Mode().Perm() != 0600 {
		t.Fatal("unchanged refresh wrote flash or catalogue permissions are public")
	}
	before, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(path, nodes, Config{}); err == nil {
		t.Fatal("corrupt catalogue silently replaced references")
	}
	after, _ := os.ReadFile(path)
	if string(after) != `{"version":999}` || len(before) == 0 {
		t.Fatal("corrupt catalogue was overwritten")
	}
}

func TestServerCatalogueConcurrentRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		nodes := serverFixture(t, "provider", profile("A", "a.example", "secret"))
		wg.Add(1)
		go func() { defer wg.Done(); _, err := SyncServers(path, nodes, Config{}); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	servers, err := ServerSummaries(path)
	if err != nil || len(servers) != 1 {
		t.Fatalf("concurrent refresh duplicated server objects: %v", err)
	}
}

func TestServerCleanupKeepsOnlyLiveOrBoundRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes := serverFixture(t, "provider",
		profile("Rule", "rule.example", "a"), profile("Pin", "pin.example", "b"),
		profile("Legacy", "legacy.example", "c"), profile("Order only", "order.example", "d"),
		profile("Live", "live.example", "e"))
	initial, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Sources: []Source{{Name: "provider"}}, Rules: []Rule{
		{Action: "proxy", NodeRef: initial[0].ID},
		{Action: "proxy", Node: initial[2].Tag},
	}, Picker: PickerSettings{Pinned: []string{initial[1].ID}, Order: []string{initial[3].ID}}}
	// Preserve legacy aliases after the provider renames a connection.
	nodes[2].Tag = "provider/Renamed"
	if _, err := SyncServers(path, nodes, config); err != nil {
		t.Fatal(err)
	}
	live := serverFixture(t, "provider", profile("Live", "live.example", "e"))
	kept, err := SyncServers(path, live, config)
	if err != nil || len(kept) != 4 {
		t.Fatalf("cleanup: %+v %v", kept, err)
	}
	for _, s := range kept {
		if s.ID == initial[3].ID {
			t.Fatal("card order retained an unbound missing server")
		}
		if s.Available != (s.ID == initial[4].ID) {
			t.Fatal("wrong availability")
		}
	}
	// Dropping the last rule/pin removes the tombstone on save, without fetching.
	config.Rules, config.Picker.Pinned = nil, nil
	kept, err = PruneServers(path, config)
	if err != nil || len(kept) != 1 || kept[0].ID != initial[4].ID || !kept[0].Available {
		t.Fatalf("unbinding affected a live server: %+v %v", kept, err)
	}
	stamp := time.Unix(1234567890, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneServers(path, config); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.ModTime().Equal(stamp) {
		t.Fatal("unchanged cleanup wrote flash")
	}
	// Deleting a source also cleans its previously live, unbound records.
	config.Sources = nil
	kept, err = PruneServers(path, config)
	if err != nil || len(kept) != 0 {
		t.Fatalf("removed source retained garbage: %+v %v", kept, err)
	}
}

func TestScopedCleanupPreservesOtherSourcesAndBoundRemovedSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes := append(serverFixture(t, "first", profile("A", "a.example", "a")), serverFixture(t, "second", profile("B", "b.example", "b"))...)
	initial, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Sources: []Source{{Name: "first"}, {Name: "second"}}}
	kept, err := SyncSourceServers(path, "first", nil, config)
	if err != nil || len(kept) != 1 || kept[0].ID != initial[1].ID || !kept[0].Available {
		t.Fatalf("scoped cleanup affected another source: %+v %v", kept, err)
	}
	config.Sources = nil
	config.Rules = []Rule{{Action: "proxy", NodeRef: initial[1].ID}}
	kept, err = PruneServers(path, config)
	if err != nil || len(kept) != 1 || kept[0].ID != initial[1].ID || kept[0].Available {
		t.Fatalf("source deletion lost a saved rule: %+v %v", kept, err)
	}
}

func TestServerCleanupMakesRoomAtCatalogueLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	catalog := serverCatalog{Version: 1, Servers: make([]serverRecord, 4096)}
	config := Config{}
	for i := range catalog.Servers {
		id := fmt.Sprintf("srv_%032x", i)
		catalog.Servers[i] = serverRecord{ServerSummary: ServerSummary{ID: id, Source: "old", Tag: "old/node", Available: true}}
		config.Picker.Pinned = append(config.Picker.Pinned, id)
	}
	data, _ := json.Marshal(catalog)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	nodes := serverFixture(t, "new", profile("New", "new.example", "secret"))
	if _, err := SyncServers(path, nodes, config); err == nil {
		t.Fatal("bound references exceeded catalogue limit")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("failed cleanup mutated catalogue")
	}
	kept, err := SyncServers(path, nodes, Config{})
	if err != nil || len(kept) != 1 || !kept[0].Available {
		t.Fatalf("garbage blocked fresh subscription: %v", err)
	}
}

func TestProviderOrderSeparatorsAndPublicConnectionDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	separator := func(name string) map[string]any { return profile(name, "0.0.0.0", "placeholder") }
	nodes := serverFixture(t, "provider", separator("Section / one"), profile("Node A", "a.example", "secret-a"), separator("Section two"), profile("Node B", "b.example", "secret-b"))
	nodes[1].StreamSettings = json.RawMessage(`{"network":"grpc"}`)
	first, err := SyncServers(path, nodes, Config{})
	if err != nil || len(first) != 4 {
		t.Fatalf("provider headings merged: %+v %v", first, err)
	}
	if first[0].Name != "Section / one" || !first[0].Separator || !first[2].Separator || first[0].ID == first[2].ID {
		t.Fatal("headings lost their labels/identity")
	}
	if first[1].Transport != "grpc" || first[1].Port != 443 || first[1].Position != 1 {
		t.Fatal("public connection details missing")
	}
	ids := map[string]string{}
	for _, s := range first {
		ids[s.Name] = s.ID
	}
	// A subsequent refresh changes order without changing stable references.
	nodes = []Node{nodes[2], nodes[3], nodes[0], nodes[1]}
	next, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range next {
		if s.ID != ids[s.Name] || s.Position != i {
			t.Fatalf("provider order/IDs lost: %+v", next)
		}
	}
	cached, _ := ServerSummaries(path)
	if cached[0].ID != next[0].ID {
		t.Fatal("get returned old insertion order")
	}
	config := Config{Default: "direct", SOCKSPort: 1080, Rules: []Rule{{Action: "proxy", NodeRef: first[1].ID, Domains: []string{"test.example"}}}}
	artifacts, err := Compile(config, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(artifacts.Xray, []byte("0.0.0.0")) || bytes.Contains(artifacts.Xray, []byte("Section")) {
		t.Fatal("display separators reached Xray")
	}
	config.Rules[0].NodeRef = first[0].ID
	if _, err := Compile(config, nodes); err == nil {
		t.Fatal("separator can be selected as a proxy")
	}
	public, _ := json.Marshal(next)
	if bytes.Contains(public, []byte("secret")) || bytes.Contains(public, []byte("a.example")) {
		t.Fatal("connection summary exposed endpoint or credentials")
	}
}
