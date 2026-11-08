package router

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"nadkop/internal/core"
)

func closureC1Binary(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("C1_NADKOP_BIN"); path != "" {
		return path
	}
	path := filepath.Join(t.TempDir(), "nadkop")
	cmd := exec.Command("go", "build", "-p", "1", "-o", path, "../../cmd/nadkop")
	cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI build: %v: %s", err, out)
	}
	return path
}
func closureC1Wait(t *testing.T, condition func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for own CLI lock")
		case <-tick.C:
		}
	}
}
func closureC1OpenFD(pid int, path string) bool {
	entries, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	for _, e := range entries {
		target, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, e.Name()))
		if target == path {
			return true
		}
	}
	return false
}
func closureC1Profile() []byte {
	return []byte(`[{"tag":"alpha","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}},{"tag":"beta","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2081}]}}]`)
}
func closureC1RaceFixture(t *testing.T, b backend, source core.Source) (core.Config, []core.ServerSummary) {
	t.Helper()
	nodes, err := core.ParseSubscription("provider", closureC1Profile())
	if err != nil {
		t.Fatal(err)
	}
	c := core.Config{Sources: []core.Source{source}, Rules: []core.Rule{}, Default: "block", SOCKSPort: 1080}
	ss, err := core.SyncServers(filepath.Join(filepath.Dir(b.config), "servers.json"), nodes, c)
	if err != nil {
		t.Fatal(err)
	}
	c.Rules = []core.Rule{{Domains: []string{"race.invalid"}, Action: "proxy", NodeRef: ss[0].ID}}
	data, _ := json.Marshal(c)
	if err = os.WriteFile(b.config, data, 0600); err != nil {
		t.Fatal(err)
	}
	return c, ss
}

// CLI paused inside a real HTTPS fetch has already read C0. RPC must reject a
// concurrent C1 save, before C1 can bind a record the C0 generator would prune.
func TestClosureC1CLIInFlightRejectsConcurrentRPCSave(t *testing.T) {
	binary := closureC1Binary(t)
	b := fixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			fmt.Fprint(w, string(closureC1Profile()))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	root := filepath.Dir(b.config)
	ca := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c, ss := closureC1RaceFixture(t, b, core.Source{Name: "provider", URL: server.URL, HWID: &core.HWIDSettings{Enabled: false}})
	before := closureC1Files(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output := filepath.Join(root, "generation")
	cmd := exec.CommandContext(ctx, binary, "generate", "-config", b.config, "-output", output)
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if key != "SSL_CERT_FILE" && key != "SSL_CERT_DIR" && !strings.HasSuffix(strings.ToUpper(key), "_PROXY") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+root)
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("CLI never fetched fixture", log.String())
	}
	c.Rules[0].NodeRef = ss[1].ID
	data, _ := json.Marshal(c)
	_, _, rev, _ := b.read()
	_, saveErr := b.call(context.Background(), "save", request{Config: data, Nodes: json.RawMessage(`[]`), Revision: rev})
	close(release)
	if err := cmd.Wait(); err != nil {
		t.Fatal("CLI failed after releasing fetch", err, log.String())
	}
	if saveErr == nil || !strings.Contains(saveErr.Error(), "Another configuration operation") {
		t.Fatal("RPC save crossed in-flight CLI snapshot", saveErr)
	}
	// HTTPS returned both nodes, so the successful CLI refresh is unchanged.
	closureC1SameFiles(t, b, before)
	if _, err := os.Stat(filepath.Join(output, "xray.json")); err != nil {
		t.Fatal("CLI generation missing", err)
	}
	if _, err := b.call(context.Background(), "save", request{Config: data, Nodes: json.RawMessage(`[]`), Revision: rev}); err != nil {
		t.Fatal("save retry after CLI failed", err)
	}
}

// Save is paused at catalogue preflight while holding the shared state lock.
// A real CLI must wait before reading C0 and then reconcile against saved C1.
func TestClosureC1CLIReadsAfterRPCSaveRetainsBoundMissingID(t *testing.T) {
	binary := closureC1Binary(t)
	b := fixture(t)
	root := filepath.Dir(b.config)
	source := filepath.Join(root, "provider.json")
	c, ss := closureC1RaceFixture(t, b, core.Source{Name: "provider", File: source})
	var all []json.RawMessage
	if err := json.Unmarshal(closureC1Profile(), &all); err != nil {
		t.Fatal(err)
	}
	onlyA, _ := json.Marshal(all[:1])
	if err := os.WriteFile(source, onlyA, 0600); err != nil {
		t.Fatal(err)
	}
	// The complete catalogue still contains beta, which the RPC is about to bind.
	catLock, err := os.OpenFile(filepath.Join(root, "servers.json.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer catLock.Close()
	if err = syscall.Flock(int(catLock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(catLock.Fd()), syscall.LOCK_UN)
	c.Rules[0].NodeRef = ss[1].ID
	data, _ := json.Marshal(c)
	_, _, rev, _ := b.read()
	saveDone := make(chan error, 1)
	go func() {
		_, err := b.call(context.Background(), "save", request{Config: data, Nodes: json.RawMessage(`[]`), Revision: rev})
		saveDone <- err
	}()
	probe, err := os.OpenFile(filepath.Join(root, ".state.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	closureC1Wait(t, func() bool {
		err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
			return false
		}
		return err == syscall.EWOULDBLOCK
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output := filepath.Join(root, "generation")
	cmd := exec.CommandContext(ctx, binary, "generate", "-config", b.config, "-output", output)
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	closureC1Wait(t, func() bool { return closureC1OpenFD(cmd.Process.Pid, filepath.Join(root, ".state.lock")) })
	if err = syscall.Flock(int(catLock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-saveDone:
		if err != nil {
			t.Fatal("RPC save failed", err)
		}
	case <-ctx.Done():
		t.Fatal("save/CLI nested lock deadlock")
	}
	err = cmd.Wait()
	if err == nil {
		t.Fatal("CLI ignored newly saved binding to missing beta")
	}
	if !strings.Contains(log.String(), "selected server is missing") {
		t.Fatal("unexpected CLI rejection", log.String())
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("runtime published with missing binding")
	}
	summaries, err := core.ServerSummaries(filepath.Join(root, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	retained := false
	for _, s := range summaries {
		if s.ID == ss[1].ID {
			retained = !s.Available
		}
	}
	if !retained {
		t.Fatal("RPC-bound missing ID lost by stale CLI")
	}
	// Returning beta recovers the SAME ID, without editing the saved rule.
	if err = os.WriteFile(source, closureC1Profile(), 0600); err != nil {
		t.Fatal(err)
	}
	retry := exec.CommandContext(ctx, binary, "generate", "-config", b.config, "-output", output)
	if out, err := retry.CombinedOutput(); err != nil {
		t.Fatalf("returning server cannot generate: %v: %s", err, out)
	}
	summaries, err = core.ServerSummaries(filepath.Join(root, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	retained = false
	for _, s := range summaries {
		if s.ID == ss[1].ID {
			retained = s.Available
		}
	}
	if !retained {
		t.Fatal("returned server assigned new ID")
	}
	t.Log("actual CLI waited for RPC before reading config; missing bound ID retained; returning node generated using original ID")
}

func TestClosureC1CLIPartialRestoreKeepsFilesAndFullRestoreKeepsIDs(t *testing.T) {
	binary := closureC1Binary(t)
	for _, shape := range []string{"complete", "missing", "empty", "whitespace", "version-only", "null-servers", "empty-servers", "null", "subset"} {
		for _, binding := range []string{"single", "group", "pin"} {
			t.Run(shape+"/"+binding, func(t *testing.T) {
				b := fixture(t)
				root := filepath.Dir(b.config)
				source := filepath.Join(root, "provider.json")
				if err := os.WriteFile(source, closureC1Profile(), 0600); err != nil {
					t.Fatal(err)
				}
				c, ss := closureC1RaceFixture(t, b, core.Source{Name: "provider", File: source})
				switch binding {
				case "group":
					c.Rules[0].NodeRef = ""
					c.Rules[0].NodeRefs = []string{ss[0].ID, ss[1].ID}
				case "pin":
					c.Rules = nil
					c.Picker.Pinned = []string{ss[1].ID}
				}
				config, _ := json.Marshal(c)
				if err := os.WriteFile(b.config, config, 0600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "servers.json")
				var partial []byte
				switch shape {
				case "missing":
					if err := os.Remove(path); err != nil {
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
					raw, _ := os.ReadFile(path)
					var cat map[string]any
					if err := json.Unmarshal(raw, &cat); err != nil {
						t.Fatal(err)
					}
					records := cat["servers"].([]any)
					if binding == "single" {
						cat["servers"] = records[1:]
					} else {
						cat["servers"] = records[:1]
					}
					partial, _ = json.Marshal(cat)
				}
				if shape != "missing" && shape != "complete" {
					if err := os.WriteFile(path, partial, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before := closureC1Files(t, b)
				for _, command := range []string{"nodes", "generate"} {
					output := filepath.Join(root, "generation")
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					out, err := exec.CommandContext(ctx, binary, command, "-config", b.config, "-output", output).CombinedOutput()
					cancel()
					if shape == "complete" {
						if err != nil {
							t.Fatalf("complete restore rejected: %v: %s", err, out)
						}
					} else {
						if err == nil {
							t.Fatal("CLI partial restore accepted", command)
						}
						if _, err := os.Stat(output); !os.IsNotExist(err) {
							t.Fatal("partial restore published runtime")
						}
					}
					closureC1SameFiles(t, b, before)
				}
			})
		}
	}
}
