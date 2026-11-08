package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGroupCompilationAndReferenceRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes := serverFixture(t, "provider", profile("A", "a.example", "secret-a"), profile("B", "b.example", "secret-b"))
	summaries, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Default: "direct", SOCKSPort: 1080, Rules: []Rule{{Name: "Video", Action: "proxy", Domains: []string{"test.example"}, NodeRefs: []string{summaries[0].ID, summaries[1].ID}, Selection: &SelectionSettings{Mode: "priority"}}}}
	data, _ := json.Marshal(cfg)
	cfg, err = ReadConfig(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := Compile(cfg, nodes)
	if err != nil {
		t.Fatal(err)
	}
	var manifest HealthManifest
	if json.Unmarshal(artifacts.Health, &manifest) != nil || len(manifest.Groups) != 1 || len(manifest.Groups[0].Members) != 2 {
		t.Fatal("missing monitor manifest")
	}
	if manifest.Groups[0].Members[0].ID != summaries[0].ID || manifest.Groups[0].Members[1].ID != summaries[1].ID {
		t.Fatal("priority reordered")
	}
	var x struct {
		Routing struct {
			Rules []struct {
				BalancerTag string
				OutboundTag string
			}
			Balancers []struct{ Selector []string }
		}
	}
	json.Unmarshal(artifacts.Xray, &x)
	if len(x.Routing.Balancers) != 1 || x.Routing.Balancers[0].Selector[0] != "__nadkop:block" {
		t.Fatal("group can fail open")
	}
	found := false
	for _, rule := range x.Routing.Rules {
		if rule.BalancerTag == manifest.Groups[0].Tag {
			found = true
			if rule.OutboundTag != "" {
				t.Fatal("group also has direct route")
			}
		}
	}
	if !found {
		t.Fatal("group route absent")
	}
	kept, err := SyncServers(path, nodes[:1], cfg)
	if err != nil || len(kept) != 2 {
		t.Fatal("missing group reference pruned", err)
	}
	artifacts, err = Compile(cfg, nodes[:1])
	if err != nil {
		t.Fatal("one missing member blocks remaining member", err)
	}
	json.Unmarshal(artifacts.Health, &manifest)
	if manifest.Groups[0].Members[1].Tag != "" {
		t.Fatal("missing member attached to a different server")
	}
	cfg.Rules[0].NodeRefs = nil
	cfg.Rules[0].NodeRef = summaries[0].ID
	data, _ = json.Marshal(cfg)
	cfg, err = ReadConfig(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rules[0].Active().Selection != nil {
		t.Fatal("single server uses dormant strategy")
	}
	artifacts, err = Compile(cfg, nodes[:1])
	if err != nil || len(artifacts.Health) != 0 {
		t.Fatal("single server starts monitor", err)
	}
}

func TestMemberSelectionModesAndFullFailure(t *testing.T) {
	group := HealthGroup{Mode: "priority", Members: []HealthMember{{ID: "a", Tag: "out-a"}, {ID: "b", Tag: "out-b"}, {ID: "c", Tag: ""}}}
	results := map[string]ProbeResult{"a": {Available: true, DelayMS: 60}, "b": {Available: true, DelayMS: 10}, "c": {Available: true, DelayMS: 1}}
	if SelectMember(group, results) != "out-a" {
		t.Fatal("priority used latency")
	}
	group.Mode = "ping"
	if SelectMember(group, results) != "out-b" {
		t.Fatal("ping did not pick fastest")
	}
	group.Mode = "url"
	if SelectMember(group, results) != "out-b" {
		t.Fatal("URL did not pick fastest")
	}
	results["a"] = ProbeResult{}
	results["b"] = ProbeResult{}
	if SelectMember(group, results) != "block" {
		t.Fatal("full failure fell back to direct")
	}
}

func TestMonitorCycleUsesEachURLAndSwitchesWithoutRestart(t *testing.T) {
	manifest := HealthManifest{Groups: []HealthGroup{
		{Tag: "priority", Mode: "priority", URL: "https://first.test", Members: []HealthMember{{"a", "out-a"}, {"b", "out-b"}}},
		{Tag: "url", Mode: "url", URL: "https://second.test", Members: []HealthMember{{"a", "out-a"}, {"b", "out-b"}}},
	}}
	var mu sync.Mutex
	calls := map[string]bool{}
	selected := map[string]string{}
	err := MonitorCycle(context.Background(), manifest, func(ctx context.Context, m HealthManifest, node HealthMember, target string) ProbeResult {
		mu.Lock()
		calls[target+node.ID] = true
		mu.Unlock()
		return ProbeResult{Available: target == "https://first.test" || node.ID == "b", DelayMS: 10}
	}, func(ctx context.Context, api, group, tag string) error { selected[group] = tag; return nil })
	if err != nil || len(calls) != 4 || selected["priority"] != "out-a" || selected["url"] != "out-b" {
		t.Fatal(calls, selected, err)
	}
}

func TestProbeUsesAuthenticatedSOCKSAndDoesNotFallBackToDirect(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.WriteHeader(204) }))
	defer target.Close()
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := proxy.Accept()
		if err != nil {
			accepted <- err
			return
		}
		defer c.Close()
		b := make([]byte, 2)
		if _, err = io.ReadFull(c, b); err != nil {
			accepted <- err
			return
		}
		methods := make([]byte, int(b[1]))
		io.ReadFull(c, methods)
		c.Write([]byte{5, 2})
		b = make([]byte, 2)
		io.ReadFull(c, b)
		user := make([]byte, int(b[1]))
		io.ReadFull(c, user)
		b = make([]byte, 1)
		io.ReadFull(c, b)
		pass := make([]byte, int(b[0]))
		io.ReadFull(c, pass)
		if string(user) != "server-id" || string(pass) != "password" {
			accepted <- fmt.Errorf("wrong auth")
			return
		}
		c.Write([]byte{1, 0})
		b = make([]byte, 4)
		io.ReadFull(c, b)
		var host string
		switch b[3] {
		case 1:
			b = make([]byte, 4)
			io.ReadFull(c, b)
			host = net.IP(b).String()
		case 3:
			io.ReadFull(c, b[:1])
			name := make([]byte, int(b[0]))
			io.ReadFull(c, name)
			host = string(name)
		default:
			accepted <- fmt.Errorf("unexpected address")
			return
		}
		port := make([]byte, 2)
		io.ReadFull(c, port)
		upstream, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(int(port[0])*256+int(port[1]))))
		if err != nil {
			accepted <- err
			return
		}
		defer upstream.Close()
		c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go io.Copy(upstream, c)
		io.Copy(c, upstream)
		accepted <- nil
	}()
	manifest := HealthManifest{SOCKS: proxy.Addr().String(), Password: "password"}
	result := Probe(context.Background(), manifest, HealthMember{"server-id", "out"}, target.URL)
	if !result.Available {
		t.Fatal("working proxy failed")
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	proxy.Close()
	result = Probe(context.Background(), manifest, HealthMember{"server-id", "out"}, target.URL)
	if result.Available || targetCalls != 1 {
		t.Fatal("failed proxy fell back to direct")
	}
}

func TestGroupValidationAndSingleNormalization(t *testing.T) {
	id := "srv_" + strings.Repeat("1", 32)
	other := "srv_" + strings.Repeat("2", 32)
	rule := Rule{Action: "proxy", Domains: []string{"test.example"}, NodeRefs: []string{id, other}, Selection: &SelectionSettings{Mode: "url", URL: "https://probe.test/204"}}
	for _, mutate := range []func(*Rule){
		func(r *Rule) { r.NodeRef = id },
		func(r *Rule) { r.NodeRefs = []string{id, id} },
		func(r *Rule) { r.Selection.Mode = "random" },
		func(r *Rule) { r.Selection.URL = "https://user:password@probe.test/204" },
	} {
		copyRule := rule
		copySelection := *rule.Selection
		copyRule.Selection = &copySelection
		mutate(&copyRule)
		data, _ := json.Marshal(Config{Rules: []Rule{copyRule}})
		if _, err := ReadConfig(strings.NewReader(string(data))); err == nil {
			t.Fatal("invalid group accepted", string(data))
		}
	}
	rule.NodeRefs = []string{id}
	data, _ := json.Marshal(Config{Rules: []Rule{rule}})
	cfg, err := ReadConfig(strings.NewReader(string(data)))
	if err != nil || cfg.Rules[0].NodeRef != id || len(cfg.Rules[0].NodeRefs) != 0 || cfg.Rules[0].Active().Selection != nil {
		t.Fatal("single normalization failed", err)
	}
}

// Model OpenWrt's resolved /tmp/run/nadkop root without touching host runtime files.
func TestMonitorStatusTmpRunRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp", "run", "nadkop")
	generation := filepath.Join(root, "generation.test")
	if err := os.MkdirAll(generation, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(generation, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	manifest := HealthManifest{API: "127.0.0.1:61080", SOCKS: "127.0.0.1:61081", Groups: []HealthGroup{{Tag: "test"}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generation, "health.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Monitor(ctx, filepath.Join(root, "current", "health.json")) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.After(3 * time.Second)
	for {
		status, err := ReadActionStatus(filepath.Join(generation, "monitor-status.json"))
		if err == nil {
			if status.Generation != generation || status.PID != os.Getpid() {
				t.Fatalf("unexpected status: %+v", status)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("monitor status unavailable: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
