package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type closureC1Stamp struct {
	data   []byte
	info   os.FileInfo
	exists bool
}

func closureC1Snapshot(t *testing.T, path string) closureC1Stamp {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return closureC1Stamp{}
	}
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return closureC1Stamp{data, info, true}
}
func closureC1Unchanged(t *testing.T, path string, before closureC1Stamp) {
	t.Helper()
	after := closureC1Snapshot(t, path)
	if before.exists != after.exists {
		t.Fatal("file existence changed", path)
	}
	if before.exists && (!bytes.Equal(before.data, after.data) || !os.SameFile(before.info, after.info) || !before.info.ModTime().Equal(after.info.ModTime()) || before.info.Mode() != after.info.Mode()) {
		t.Fatal("file changed after rejection", path)
	}
}
func closureC1Nodes(t *testing.T) []Node {
	return serverFixture(t, "provider", profile("Alpha", "a.closure.invalid", "synthetic-a"), profile("Beta", "b.closure.invalid", "synthetic-b"), profile("Gamma", "c.closure.invalid", "synthetic-c"))
}

func TestClosureC1PartialRestoreRejectsBeforePublication(t *testing.T) {
	for _, shape := range []string{"missing", "empty", "whitespace", "version-only", "null-servers", "empty-servers", "null", "subset"} {
		for _, binding := range []string{"single", "group", "pin", "inactive"} {
			for _, operation := range []string{"sync", "source", "prune"} {
				t.Run(shape+"/"+binding+"/"+operation, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "servers.json")
					original, err := SyncServers(path, closureC1Nodes(t), Config{})
					if err != nil {
						t.Fatal(err)
					}
					c := Config{Sources: []Source{{Name: "provider", URL: "https://provider.invalid/sub"}}, Default: "block", SOCKSPort: 1080}
					switch binding {
					case "single":
						c.Rules = []Rule{{Action: "proxy", NodeRef: original[0].ID, Domains: []string{"single.invalid"}}}
					case "group":
						c.Rules = []Rule{{Action: "proxy", NodeRefs: []string{original[0].ID, original[1].ID}, Domains: []string{"group.invalid"}}}
					case "pin":
						c.Picker.Pinned = []string{original[0].ID}
					case "inactive":
						c.Rules = []Rule{{Action: "block", NodeRef: original[0].ID, NodeRefs: []string{original[1].ID}, Domains: []string{"inactive.invalid"}}}
					}
					var partial []byte
					switch shape {
					case "missing":
						if err = os.Remove(path); err != nil {
							t.Fatal(err)
						}
					case "empty":
						partial = []byte{}
					case "whitespace":
						partial = []byte(" \n\t")
					case "version-only":
						partial = []byte(`{"version":1}`)
					case "null-servers":
						partial = []byte(`{"version":1,"servers":null}`)
					case "empty-servers":
						partial = []byte(`{"version":1,"servers":[]}`)
					case "null":
						partial = []byte(`null`)
					case "subset":
						cat, e := readCatalog(path)
						if e != nil {
							t.Fatal(e)
						}
						cat.Servers = cat.Servers[2:]
						partial, _ = json.Marshal(cat)
					}
					if shape != "missing" {
						if err = os.WriteFile(path, partial, 0600); err != nil {
							t.Fatal(err)
						}
					}
					before := closureC1Snapshot(t, path)
					nodes := closureC1Nodes(t)
					switch operation {
					case "sync":
						_, err = SyncServers(path, nodes, c)
					case "source":
						_, err = SyncSourceServers(path, "provider", nodes, c)
					case "prune":
						_, err = PruneServers(path, c)
					}
					if err == nil {
						t.Fatal("partial restore accepted; original bound IDs can be lost")
					}
					closureC1Unchanged(t, path, before)
				})
			}
		}
	}
}

func TestClosureC1FullRestoreAndBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	original, err := SyncServers(path, closureC1Nodes(t), Config{})
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Default: "block", SOCKSPort: 1080, Sources: []Source{{Name: "provider", URL: "https://provider.invalid/sub"}}, Rules: []Rule{{Action: "proxy", NodeRef: original[0].ID, Domains: []string{"single.invalid"}}, {Action: "proxy", NodeRefs: []string{original[1].ID, original[2].ID}, Domains: []string{"group.invalid"}}}, Picker: PickerSettings{Pinned: []string{original[0].ID, original[2].ID}}}
	restored := filepath.Join(t.TempDir(), "servers.json")
	before := closureC1Snapshot(t, path)
	if err = os.WriteFile(restored, before.data, 0600); err != nil {
		t.Fatal(err)
	}
	restoredBefore := closureC1Snapshot(t, restored)
	nodes := closureC1Nodes(t)
	next, err := SyncServers(restored, nodes, c)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range next {
		if s.ID != original[i].ID {
			t.Fatal("full restore replaced ID")
		}
	}
	if _, err = Compile(c, nodes); err != nil {
		t.Fatal("full restore cannot compile", err)
	}
	closureC1Unchanged(t, restored, restoredBefore)
	// A provider disappearing still retains the record in a complete catalogue.
	if _, err = SyncServers(restored, nil, c); err != nil {
		t.Fatal(err)
	}
	missing, err := ServerSummaries(restored)
	if err != nil || len(missing) != 3 {
		t.Fatal("bound missing records lost", err)
	}
	for i, s := range missing {
		if s.ID != original[i].ID || s.Available {
			t.Fatal("missing ID changed")
		}
	}
	// Missing catalogue with no bindings is a supported fresh installation.
	empty := filepath.Join(t.TempDir(), "servers.json")
	if _, err = SyncServers(empty, closureC1Nodes(t), Config{}); err != nil {
		t.Fatal("bootstrap rejected", err)
	}
}

func TestClosureC1CatalogueBudgetRejectsLegalLongSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	small := closureC1Nodes(t)
	if _, err := SyncServers(path, small, Config{}); err != nil {
		t.Fatal(err)
	}
	before := closureC1Snapshot(t, path)
	payload := bytes.ReplaceAll(s4Fixture(4096, "json"), []byte(`"tag":"node-`), []byte(`"tag":"`+strings.Repeat("s4", 256)+"-node-"))
	if len(payload) > maxSubscription {
		t.Fatal("source itself exceeds budget")
	}
	nodes, err := ParseSubscription("S4", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SyncServers(path, nodes, Config{}); err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
		t.Fatal("oversized result not rejected", err)
	}
	closureC1Unchanged(t, path, before)
	if _, err = ServerSummaries(path); err != nil {
		t.Fatal("previous catalogue unreadable", err)
	}
	if _, err = SyncServers(path, closureC1Nodes(t), Config{}); err != nil {
		t.Fatal("valid retry failed", err)
	}
	closureC1Unchanged(t, path, before)
	t.Logf("source bytes=%d, nodes=%d; oversized result rejected, previous IDs/readability/retry preserved", len(payload), len(nodes))
}

func TestClosureC1CatalogueExactByteBudget(t *testing.T) {
	for _, size := range []int{maxSubscription - 1, maxSubscription, maxSubscription + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "servers.json")
			ss, err := SyncServers(path, closureC1Nodes(t)[:1], Config{})
			if err != nil {
				t.Fatal(err)
			}
			cat, err := readCatalog(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(cat)
			cat.Servers[0].Name += strings.Repeat("x", size-len(raw)-1)
			raw, _ = json.Marshal(cat)
			raw = append(raw, '\n')
			if len(raw) != size {
				t.Fatal("bad exact fixture", len(raw))
			}
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			before := closureC1Snapshot(t, path)
			_, readErr := ServerSummaries(path)
			if (readErr == nil) != (size <= maxSubscription) {
				t.Fatal("reader byte boundary", readErr)
			}
			// Removing source changes true to false (+1 byte), with the pinned ID retained.
			_, err = PruneServers(path, Config{Picker: PickerSettings{Pinned: []string{ss[0].ID}}})
			if size < maxSubscription {
				if err != nil {
					t.Fatal("exactly 4 MiB writer rejected", err)
				}
				after := closureC1Snapshot(t, path)
				if len(after.data) != maxSubscription {
					t.Fatal("wrong final size", len(after.data))
				}
				if _, err = ServerSummaries(path); err != nil {
					t.Fatal("exact write unreadable", err)
				}
			} else {
				if err == nil {
					t.Fatal("oversized catalogue accepted")
				}
				closureC1Unchanged(t, path, before)
			}
		})
	}
}

func TestClosureC1ConfigurationLockSurvivesRenameAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockConfiguration(path, false)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(root, "replacement.json")
	if err = os.WriteFile(tmp, []byte(`{"default":"block"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if second, e := LockConfiguration(path, true); e == nil {
		second()
		t.Fatal("rename bypassed held configuration lock")
	}
	unlock()
	retry, err := LockConfiguration(path, true)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	retry()
	lockPath := filepath.Join(root, ".state.lock")
	if err = os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(path, lockPath); err != nil {
		t.Fatal(err)
	}
	if u, e := LockConfiguration(path, true); e == nil {
		u()
		t.Fatal("symlink lock accepted")
	}
	if err = os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(lockPath, 0700); err != nil {
		t.Fatal(err)
	}
	if u, e := LockConfiguration(path, true); e == nil {
		u()
		t.Fatal("directory lock accepted")
	}
}
