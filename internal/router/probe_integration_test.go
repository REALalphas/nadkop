package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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

// Executed on the disposable OpenWrt VM; never changes its real configuration.
func TestProbeRealXray(t *testing.T) {
	if _, err := os.Stat("/usr/bin/xray"); err != nil {
		t.Skip("requires Xray; run compiled test on the OpenWrt VM")
	}
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host != "127.0.0.2" {
			t.Errorf("probe bypassed upstream: %s", host)
		}
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	work := t.TempDir()
	upstream := filepath.Join(work, "upstream.json")
	cfg := fmt.Sprintf(`{"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom","sendThrough":"127.0.0.2"}]}`, port)
	if err = os.WriteFile(upstream, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/usr/bin/xray", "run", "-config", upstream)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	alive := true
	defer func() {
		if alive {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	ready := false
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if e == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		t.Fatal("upstream did not listen")
	}
	b := fixture(t)
	nodes := fmt.Sprintf(`[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":%d}]}}]`, port)
	if err = os.WriteFile(b.nodes, []byte(nodes), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{"sources":[{"name":"lab","file":%q}],"rules":[],"default":"direct"}`, b.nodes)
	if err = os.WriteFile(b.config, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	if _, _, err = b.load(ctx); err != nil {
		t.Fatal(err)
	}
	summaries, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
	if err != nil || len(summaries) != 1 {
		t.Fatal(summaries, err)
	}
	ref := summaries[0].ID
	request := request{Refs: []string{ref}, Mode: "url", URL: target.URL}
	result, err := b.probe(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	values := result.(map[string]any)["results"].(map[string]core.ProbeResult)
	if !values[ref].Available || calls.Load() != 1 {
		t.Fatal("live proxy was not tested", values, calls.Load())
	}
	cmd.Process.Kill()
	cmd.Wait()
	alive = false
	result, err = b.probe(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	values = result.(map[string]any)["results"].(map[string]core.ProbeResult)
	if values[ref].Available || calls.Load() != 1 {
		t.Fatal("unavailable proxy fell back to direct", values, calls.Load())
	}
	files, _ := filepath.Glob(filepath.Join(b.runtime, "probe.*"))
	for _, file := range files {
		if strings.HasSuffix(file, "probe.lock") {
			continue
		}
		t.Errorf("probe left files behind: %s", file)
	}
	saved, _ := os.ReadFile(b.config)
	if string(saved) != config {
		t.Fatal("draft probe changed saved configuration")
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "settings") {
		t.Fatal("credentials leaked in response")
	}
}
