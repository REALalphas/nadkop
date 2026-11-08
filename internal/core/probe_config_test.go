package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeIsIsolatedAndMissingReferenceFailsClosed(t *testing.T) {
	nodes, err := ParseSubscription("lab", []byte(`[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := SyncServers(filepath.Join(t.TempDir(), "servers.json"), nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	missing := "srv_" + strings.Repeat("a", 32)
	setup, err := CompileProbe(nodes, []string{refs[0].ID, missing}, 20123)
	if err != nil {
		t.Fatal(err)
	}
	if setup.Members[0].Tag != "lab/exit" || setup.Members[1].Tag != "" {
		t.Fatal(setup.Members)
	}
	var cfg struct {
		Inbounds []struct {
			Listen   string
			Port     int
			Settings struct {
				Auth     string
				Accounts []any
			}
		}
		Routing struct {
			Rules []struct{ OutboundTag string }
		}
		API any
	}
	if err = json.Unmarshal(setup.Xray, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Port != 20123 || cfg.Inbounds[0].Listen != "127.0.0.1" || cfg.Inbounds[0].Settings.Auth != "password" || len(cfg.Inbounds[0].Settings.Accounts) != 2 || cfg.API != nil {
		t.Fatalf("bad isolated listener: %+v", cfg)
	}
	if len(cfg.Routing.Rules) != 2 || cfg.Routing.Rules[1].OutboundTag != "block" {
		t.Fatal("missing reference can use default route")
	}
}

func TestSourceTitleDoesNotChangeNamespace(t *testing.T) {
	cfg, err := ReadConfig(strings.NewReader(`{"sources":[{"name":"stable","title":"My subscription EU","file":"/etc/nadkop/nodes.json"}],"rules":[]}`))
	if err != nil || cfg.Sources[0].Name != "stable" || cfg.Sources[0].Title != "My subscription EU" {
		t.Fatal(cfg, err)
	}
	_, err = ReadConfig(strings.NewReader(`{"sources":[{"name":"stable","title":"bad\nname","file":"/etc/nadkop/nodes.json"}],"rules":[]}`))
	if err == nil {
		t.Fatal("accepted control characters")
	}
}

func TestProbeExcludesBrokenUnselectedOutboundAndKeepsDependencies(t *testing.T) {
	nodes, err := ParseSubscription("probe", []byte(`[
  {"tag":"chosen","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]},"streamSettings":{"sockopt":{"dialerProxy":"helper"}}},
  {"tag":"helper","protocol":"freedom","settings":{}},
  {"tag":"broken","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2081}]},"streamSettings":{"network":"unsupported-for-test"}}
 ]`))
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := SyncServers(filepath.Join(t.TempDir(), "servers.json"), nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	for _, summary := range summaries {
		if summary.Tag == "probe/chosen" {
			ref = summary.ID
		}
	}
	if ref == "" {
		t.Fatal("selected reference is missing")
	}
	setup, err := CompileProbe(nodes, []string{ref}, 20123)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(setup.Xray), "probe/broken") || !strings.Contains(string(setup.Xray), "probe/helper") {
		t.Fatal("unrelated outbound included or dependency lost")
	}
	xray, err := exec.LookPath("xray")
	if err != nil {
		t.Log("Xray is unavailable; live config validation skipped")
		return
	}
	full, err := Compile(Config{Default: "direct", SOCKSPort: 20123}, nodes)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "probe.json")
	if err = os.WriteFile(path, full.Xray, 0600); err != nil {
		t.Fatal(err)
	}
	if err = exec.Command(xray, "run", "-test", "-config", path).Run(); err == nil {
		t.Fatal("invalid unselected transport did not reproduce the previous Xray failure")
	}
	if err = os.WriteFile(path, setup.Xray, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(xray, "run", "-test", "-config", path).CombinedOutput(); err != nil {
		t.Fatalf("selected server and dependencies failed Xray validation: %v: %s", err, output)
	}
}
