package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nadkop/internal/core"
)

func TestX2LoadCancellationCannotPublish(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprint(local), func(t *testing.T) {
			b := fixture(t)
			c := core.Config{Sources: []core.Source{{Name: "test", URL: "https://source.invalid/sub"}}}
			if local {
				c.Sources[0] = core.Source{Name: "test", File: b.nodes}
			}
			encoded, _ := json.Marshal(c)
			os.WriteFile(b.config, encoded, 0600)
			before := closureC1Files(t, b)
			ctx, cancel := context.WithCancel(context.Background())
			_, _, err := b.loadWith(ctx, func(context.Context, core.Source) ([]core.Node, error) {
				cancel()
				return core.ParseSubscription("test", []byte(s1RouterProfile))
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled result accepted", err)
			}
			closureC1SameFiles(t, b, before)
		})
	}
}

func TestX2LoadAll16StartAndPreserveOrder(t *testing.T) {
	b := fixture(t)
	c := core.Config{}
	for i := 0; i < 16; i++ {
		c.Sources = append(c.Sources, core.Source{Name: fmt.Sprintf("s%d", i), URL: "https://source.invalid/sub"})
	}
	data, _ := json.Marshal(c)
	os.WriteFile(b.config, data, 0600)
	started := make(chan string, 16)
	release := make(chan struct{})
	done := make(chan error, 1)
	var got []core.Node
	go func() {
		_, nodes, err := b.loadWith(context.Background(), func(ctx context.Context, s core.Source) ([]core.Node, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 12*time.Second {
				return nil, errors.New("fetch budget changed")
			}
			started <- s.Name
			<-release
			return core.ParseSubscription(s.Name, []byte(s1RouterProfile))
		})
		got = nodes
		done <- err
	}()
	for i := 0; i < 16; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("loader introduced waves")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(got) != 16 {
		t.Fatal(len(got))
	}
	for i, node := range got {
		if !strings.HasPrefix(node.Tag, fmt.Sprintf("s%d/", i)) {
			t.Fatal("source order changed", node.Tag)
		}
	}
}

func TestX2ActionDiagnosticsIdentityAndPrivacy(t *testing.T) {
	b := fixture(t)
	generation := filepath.Join(b.runtime, "generation.test")
	os.MkdirAll(generation, 0700)
	os.Symlink(generation, filepath.Join(b.runtime, "current"))
	path := filepath.Join(generation, "monitor-status.json")
	instances := map[string]any{"monitor": map[string]any{"running": true, "pid": os.Getpid()}}
	base := core.ActionStatus{Generation: generation, PID: os.Getpid(), ProcessStart: core.ProcessStart(os.Getpid()), State: "ok", StartedAt: 1, FinishedAt: 2, OK: true, Total: 3, Completed: 3}
	for _, tc := range []struct {
		name, want string
		edit       func(*core.ActionStatus)
	}{
		{"success", "success", func(*core.ActionStatus) {}},
		{"partial-failure", "error", func(s *core.ActionStatus) { s.Failed = 1; s.State = "error"; s.OK = false }},
		{"long-cycle", "pending", func(s *core.ActionStatus) { s.State = "running"; s.FinishedAt = 0; s.OK = false; s.Completed = 0 }},
		{"old-generation", "unknown", func(s *core.ActionStatus) { s.Generation += "-old" }},
		{"old-process", "unknown", func(s *core.ActionStatus) { s.ProcessStart += "0" }},
		{"different-pid", "unknown", func(s *core.ActionStatus) { s.PID++ }},
		{"future", "unknown", func(s *core.ActionStatus) { s.StartedAt = time.Now().Unix() + 999; s.FinishedAt = s.StartedAt + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.edit(&s)
			core.WriteActionStatus(path, s)
			check := b.actionCheck(path, "monitor", "Action", instances)
			if check.State != tc.want {
				t.Fatal(check)
			}
		})
	}
	os.WriteFile(path, []byte(`{"password":"canary","error":"https://secret.invalid/token"}`), 0600)
	check := b.actionCheck(path, "monitor", "Action", instances)
	data, _ := json.Marshal(check)
	if check.State != "unknown" || bytes.Contains(data, []byte("canary")) || bytes.Contains(data, []byte("secret")) {
		t.Fatal(string(data))
	}
	os.Remove(path)
	if b.actionCheck(path, "monitor", "Action", instances).State != "unknown" {
		t.Fatal("missing status reported success")
	}
}

func TestX2UpdaterPublishesRunningAndFinalResult(t *testing.T) {
	b := fixture(t)
	generation := filepath.Join(b.runtime, "generation.test")
	os.MkdirAll(generation, 0700)
	os.Symlink(generation, filepath.Join(b.runtime, "current"))
	for _, fail := range []bool{false, true} {
		err := b.updateOnce(context.Background(), func(context.Context) error {
			s, e := core.ReadActionStatus(filepath.Join(b.runtime, "update-status.json"))
			if e != nil || s.State != "running" {
				t.Fatal(s, e)
			}
			if fail {
				return errors.New("canary raw stderr")
			}
			return nil
		})
		if (err != nil) != fail {
			t.Fatal(err)
		}
		s, e := core.ReadActionStatus(filepath.Join(b.runtime, "update-status.json"))
		if e != nil || s.OK == fail || s.Failed != map[bool]int{true: 1, false: 0}[fail] {
			t.Fatal(s, e)
		}
		data, _ := os.ReadFile(filepath.Join(b.runtime, "update-status.json"))
		if bytes.Contains(data, []byte("canary")) {
			t.Fatal("secret stored")
		}
	}
}

// Used only by scripts/test-x2-save-faults.py in a private directory.
func TestX2SaveFaultChild(t *testing.T) {
	root := os.Getenv("X2_SAVE_FIXTURE")
	if root == "" {
		t.Skip("fault injection helper")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b := backend{filepath.Join(root, "config.json"), filepath.Join(root, "nodes.json"), filepath.Join(root, "run")}
	if os.Getenv("X2_SAVE_MODE") == "inspect" {
		c, n, _, err := b.read()
		t.Logf("X2_INSPECT blocked=%v error=%v config=%d nodes=%d", err != nil, err, len(c), len(n))
		return
	}
	_, _, rev, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	c := json.RawMessage(`{"sources":[{"name":"local","file":"` + b.nodes + `"}],"rules":[{"action":"proxy","domains":["test.invalid"],"node":"local/new"}]}`)
	n := json.RawMessage(`[{"tag":"new","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":12345}]}}]`)
	_, err = b.call(context.Background(), "save", request{Config: c, Nodes: n, Revision: rev})
	t.Logf("X2_SAVE error=%v", err)
}

func TestMonitorStatusUnavailableDistinguishesRunningProcess(t *testing.T) {
	b := fixture(t)
	path := filepath.Join(b.runtime, "missing-status.json")
	running := b.actionCheck(path, "monitor", "Monitor", map[string]any{"monitor": map[string]any{"running": true, "pid": os.Getpid()}})
	stopped := b.actionCheck(path, "monitor", "Monitor", nil)
	if running.State != "unknown" || stopped.State != "unknown" || running.Detail == stopped.Detail || !strings.Contains(running.Detail, "Process is running") || !strings.Contains(stopped.Detail, "Process is not running") {
		t.Fatalf("running: %+v; stopped: %+v", running, stopped)
	}
}
