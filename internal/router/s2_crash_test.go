package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"nadkop/internal/core"
)

// TestS2Child is a subprocess-only entry point. Its entire backend is owned by
// the caller; router.Run and the host's fixed /etc/nadkop paths are never used.
func TestS2Child(t *testing.T) {
	root := os.Getenv("S2_FIXTURE")
	mode := os.Getenv("S2_MODE")
	if root == "" || mode == "" {
		t.Skip("S2 subprocess helper")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b := backend{filepath.Join(root, "config.json"), filepath.Join(root, "nodes.json"), filepath.Join(root, "run")}
	var result any
	var err error
	switch mode {
	case "save":
		var req request
		data, e := os.ReadFile(filepath.Join(root, "request.json"))
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(data, &req); e != nil {
			t.Fatal(e)
		}
		result, err = b.call(context.Background(), "save", req)
	case "atomic":
		data, e := os.ReadFile(filepath.Join(root, "payload.json"))
		if e != nil {
			t.Fatal(e)
		}
		err = atomicWrite(filepath.Join(root, "atomic.json"), data)
	case "atomic-enospc":
		target := filepath.Join(root, "atomic.json")
		old := []byte(`{"value":"old"}`)
		if e := os.WriteFile(target, old, 0600); e != nil {
			t.Fatal(e)
		}
		fillErr := os.WriteFile(filepath.Join(root, "s2-filler"), make([]byte, 1<<20), 0600)
		err = atomicWrite(target, []byte(`{"value":"new"}`))
		after, _ := os.ReadFile(target)
		unchanged := bytes.Equal(old, after)
		if e := os.Remove(filepath.Join(root, "s2-filler")); e != nil {
			t.Fatal(e)
		}
		retryErr := atomicWrite(target, []byte(`{"value":"new"}`))
		result = map[string]any{"fillENOSPC": errors.Is(fillErr, syscall.ENOSPC), "writeENOSPC": errors.Is(err, syscall.ENOSPC), "beforeHash": s2Hash(old), "afterHash": s2Hash(after), "unchanged": unchanged, "jsonValid": json.Valid(after), "retryOK": retryErr == nil}
	case "inspect":
		result = s2Inspect(b)
	case "restart":
		var c core.Config
		var nodes []core.Node
		c, nodes, err = b.load(context.Background())
		if err == nil {
			_, err = core.CompileWithGeo(c, nodes, b.geoRoot())
		}
		result = map[string]any{"nodeCount": len(nodes), "compiled": err == nil}
	case "refresh-old":
		data, e := os.ReadFile(filepath.Join(root, "old-config.json"))
		if e != nil {
			t.Fatal(e)
		}
		var c core.Config
		c, err = core.ReadConfig(bytes.NewReader(data))
		if err == nil {
			var nodes []core.Node
			data, e = os.ReadFile(filepath.Join(root, "refresh-nodes.json"))
			if e != nil {
				t.Fatal(e)
			}
			nodes, err = core.ParseSubscription("provider", data)
			if err == nil {
				result, err = core.SyncServers(filepath.Join(root, "servers.json"), nodes, c)
			}
		}
	default:
		t.Fatal("unknown S2 helper mode")
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	encoded, _ := json.Marshal(map[string]any{"mode": mode, "error": message, "result": result})
	fmt.Println("S2_RESULT " + string(encoded))
}

func s2Inspect(b backend) map[string]any {
	result := map[string]any{}
	cdata, ndata, revision, err := b.read()
	result["readOK"], result["revision"] = err == nil, revision
	if err != nil {
		result["readError"] = err.Error()
		return result
	}
	result["configJSON"], result["nodesJSON"] = json.Valid(cdata), json.Valid(ndata)
	c, err := core.ReadConfig(bytes.NewReader(cdata))
	result["configValid"] = err == nil
	if err != nil {
		result["configError"] = err.Error()
	}
	nodes, nerr := parseNodes(ndata)
	result["nodesValid"] = nerr == nil
	if nerr != nil {
		result["nodesError"] = nerr.Error()
	}
	tags := []string{}
	for _, n := range nodes {
		tags = append(tags, n.Tag)
	}
	result["localTags"] = tags
	servers, serr := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
	result["catalogueValid"] = serr == nil
	if serr != nil {
		result["catalogueError"] = serr.Error()
	}
	ids := map[string]bool{}
	for _, s := range servers {
		ids[s.ID] = true
	}
	missing := []string{}
	missingTags := []string{}
	for _, r := range c.Rules {
		refs := append([]string{}, r.NodeRefs...)
		if r.NodeRef != "" {
			refs = append(refs, r.NodeRef)
		}
		for _, ref := range refs {
			if !ids[ref] {
				missing = append(missing, ref)
			}
		}
		if r.Node != "" {
			for _, source := range c.Sources {
				if source.File != b.nodes || !strings.HasPrefix(r.Node, source.Name+"/") {
					continue
				}
				found := false
				for _, tag := range tags {
					found = found || r.Node == source.Name+"/"+tag
				}
				if !found {
					missingTags = append(missingTags, r.Node)
				}
			}
		}
	}
	result["missingNodeRefs"], result["missingLocalTags"] = missing, missingTags
	result["catalogueIDs"] = ids
	return result
}

func s2Fixture(t *testing.T) backend {
	t.Helper()
	root := t.TempDir()
	b := backend{filepath.Join(root, "config.json"), filepath.Join(root, "nodes.json"), filepath.Join(root, "run")}
	if err := os.WriteFile(b.config, []byte(`{"sources":[],"rules":[],"default":"direct"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.nodes, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.runtime, 0700); err != nil {
		t.Fatal(err)
	}
	return b
}

func s2Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func TestS2AtomicPermissionAndRenameFailures(t *testing.T) {
	for _, kind := range []string{"permission", "rename-directory"} {
		t.Run(kind, func(t *testing.T) {
			b := s2Fixture(t)
			old, _ := os.ReadFile(b.config)
			if kind == "permission" {
				if os.Geteuid() == 0 {
					t.Skip("requires unprivileged fixture owner")
				}
				parent := filepath.Dir(b.config)
				if err := os.Chmod(parent, 0500); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(parent, 0700)
			} else {
				if err := os.Remove(b.config); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(b.config, 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := atomicWrite(b.config, []byte(`{"default":"block"}`))
			if err == nil {
				t.Fatal("fault was not returned")
			}
			if kind == "permission" {
				got, e := os.ReadFile(b.config)
				if e != nil || !bytes.Equal(got, old) {
					t.Fatal("old config changed", e)
				}
				t.Logf("kind=%s errno=%v old=%s after=%s valid=%v", kind, err, s2Hash(old), s2Hash(got), json.Valid(got))
			} else {
				st, e := os.Stat(b.config)
				if e != nil || !st.IsDir() {
					t.Fatal("rename overwrote directory")
				}
				t.Logf("kind=%s errno=%v directoryRetained=true", kind, err)
			}
		})
	}
}

func TestS2SaveRefreshShareUILock(t *testing.T) {
	b := s2Fixture(t)
	unlock, err := b.lock()
	if err != nil {
		t.Fatal(err)
	}
	beforeC, _ := os.ReadFile(b.config)
	beforeN, _ := os.ReadFile(b.nodes)
	_, _, rev, _ := b.read()
	for _, method := range []string{"save", "nodes", "service"} {
		_, err = b.call(context.Background(), method, request{Config: beforeC, Nodes: beforeN, Revision: rev, Action: "restart"})
		if err == nil || !strings.Contains(err.Error(), "Another save") {
			t.Fatalf("%s crossed lock: %v", method, err)
		}
		t.Logf("%s rejected under shared ui.lock: %v", method, err)
	}
	unlock()
	if _, err = b.call(context.Background(), "save", request{Config: beforeC, Nodes: beforeN, Revision: rev}); err != nil {
		t.Fatal("lock not released", err)
	}
}

func TestS2IncompleteRuntimeAndPrepareFailure(t *testing.T) {
	b := s2Fixture(t)
	gen := filepath.Join(b.runtime, "generation.old")
	if err := os.Mkdir(gen, 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"synthetic":"working-generation"}`)
	if err := os.WriteFile(filepath.Join(gen, "xray.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gen, filepath.Join(b.runtime, "current")); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"", "{", `{"unknown":1}`, `{} {}`} {
		candidate := t.TempDir()
		if body != "" {
			if err := os.WriteFile(filepath.Join(candidate, "runtime.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := readRuntime(candidate)
		if err == nil {
			t.Fatalf("accepted incomplete plan %q", body)
		}
		t.Logf("runtimeLength=%d rejected=%v", len(body), err)
	}
	for _, failure := range []string{"compile", "xray"} {
		c := core.Config{Default: "direct", SOCKSPort: 1080}
		if failure == "compile" {
			c.Rules = []core.Rule{{Domains: []string{"s2.example"}, Action: "proxy", Node: "missing/server"}}
		}
		calls := 0
		run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			calls++
			return nil, errors.New("S2 injected xray failure")
		}
		_, err := b.prepareSteps(context.Background(), c, nil, run, nil)
		if err == nil {
			t.Fatal("expected prepare failure")
		}
		got, _ := os.ReadFile(filepath.Join(b.runtime, "current", "xray.json"))
		if !bytes.Equal(got, old) {
			t.Fatal("old runtime changed")
		}
		dirs, _ := filepath.Glob(filepath.Join(b.runtime, "generation.*"))
		if len(dirs) != 1 {
			t.Fatalf("temporary generation leaked: %v", dirs)
		}
		t.Logf("failure=%s error=%v runnerCalls=%d currentHash=%s", failure, err, calls, s2Hash(got))
	}
}

func TestS2NetworkRollbackBoundaries(t *testing.T) {
	for _, failure := range []string{"ipv4-rule", "ipv6-route", "nft", "rollback-delete"} {
		t.Run(failure, func(t *testing.T) {
			b := s2Fixture(t)
			var calls []string
			owned := map[string]bool{}
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				call := filepath.Base(name) + " " + strings.Join(args, " ")
				calls = append(calls, call)
				if strings.Contains(call, " show") {
					return []byte(`[]`), nil
				}
				if (failure == "ipv4-rule" && strings.Contains(call, "-4 rule add")) || (failure == "ipv6-route" && strings.Contains(call, "-6 route add")) || strings.HasPrefix(call, "nft -f") {
					return nil, errors.New("S2 injected apply failure")
				}
				if failure == "rollback-delete" && strings.Contains(call, " del ") {
					return nil, errors.New("S2 injected rollback failure")
				}
				if len(args) > 2 && (args[1] == "route" || args[1] == "rule") {
					key := args[0] + "/" + args[1]
					if args[2] == "add" {
						owned[key] = true
					}
					if args[2] == "del" {
						delete(owned, key)
					}
				}
				return nil, nil
			}
			err := b.applyNetwork(context.Background(), core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true, IPv6: true}}, filepath.Join(b.runtime, "apply.nft"), run)
			if err == nil {
				t.Fatal("expected error")
			}
			if failure != "rollback-delete" && len(owned) != 0 {
				t.Fatal("new policies survived successful rollback", owned)
			}
			if failure == "rollback-delete" && len(owned) != 4 {
				t.Fatal("rollback fault did not reach all four owned policies", owned)
			}
			t.Logf("failure=%s error=%v remaining=%v commands=%q", failure, err, owned, calls)
		})
	}
}
