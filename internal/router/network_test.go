package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nadkop/internal/core"
)

func TestProductPolicyOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, rules, routes string
		rule, route, valid  bool
	}{
		{"empty", `[]`, `[]`, false, false, true},
		{"system", `[{"priority":0,"table":"local","src":"all"},{"priority":32766,"table":"main"}]`, `[{"table":"main","dst":"default","gateway":"192.0.2.1","dev":"eth0"}]`, false, false, true},
		{"owned", `[{"priority":10000,"table":100,"src":"all","fwmark":"0x10000","fwmask":"0x10000"}]`, `[{"table":100,"type":"local","dst":"default","dev":"lo"}]`, true, true, true},
		{"priority-collision", `[{"priority":10000,"table":200}]`, `[]`, false, false, false},
		{"table-collision", `[{"priority":10001,"table":100}]`, `[]`, false, false, false},
		{"mark-collision", `[{"priority":99,"table":200,"fwmark":"0x10000","fwmask":"0xffffffff"}]`, `[]`, false, false, false},
		{"wrong-mask", `[{"priority":10000,"table":100,"fwmark":"0x10000"}]`, `[]`, false, false, false},
		{"scoped-rule", `[{"priority":10000,"table":100,"fwmark":"0x10000","fwmask":"0x10000","iif":"eth0"}]`, `[]`, false, false, false},
		{"duplicate-rule", `[{"priority":10000,"table":100,"fwmark":"0x10000","fwmask":"0x10000"},{"priority":10000,"table":100,"fwmark":"0x10000","fwmask":"0x10000"}]`, `[]`, false, false, false},
		{"foreign-route", `[]`, `[{"table":100,"dst":"default","dev":"eth0","gateway":"192.0.2.1"}]`, false, false, false},
		{"duplicate-route", `[]`, `[{"table":100,"type":"local","dst":"default","dev":"lo"},{"table":100,"type":"local","dst":"default","dev":"lo"}]`, false, false, false},
		{"invalid-json", `no`, `[]`, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), "rule show") {
					return []byte(tc.rules), nil
				}
				return []byte(tc.routes), nil
			}
			rule, route, err := policyState(context.Background(), "-4", run)
			if (err == nil) != tc.valid || err == nil && (rule != tc.rule || route != tc.route) {
				t.Fatal(rule, route, err)
			}
		})
	}
}

func TestProductNetworkPreflightConflicts(t *testing.T) {
	plan := core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true, Interfaces: []string{"br-lan"}, IPv6: true}}
	for _, failure := range []string{"", "device", "offloading", "offloading-hw", "includes", "policy"} {
		t.Run(failure, func(t *testing.T) {
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				text := strings.Join(args, " ")
				switch {
				case strings.HasPrefix(text, "link show") && failure == "device":
					return nil, errors.New("absent")
				case strings.HasSuffix(text, "flow_offloading") && failure == "offloading":
					return []byte("1\n"), nil
				case strings.HasSuffix(text, "flow_offloading_hw") && failure == "offloading-hw":
					return []byte("1\n"), nil
				case strings.HasSuffix(text, "auto_includes") && failure == "includes":
					return []byte("0\n"), nil
				case strings.Contains(text, "rule show") && failure == "policy":
					return []byte(`[{"priority":10000,"table":200}]`), nil
				}
				return []byte(`[]`), nil
			}
			err := checkNetwork(context.Background(), plan, run)
			if (err == nil) != (failure == "") {
				t.Fatal(err)
			}
		})
	}
	if err := checkNetwork(context.Background(), core.RuntimeSettings{}, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("disabled routing inspected foreign policy")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProductNftTransactionAndForeignTableProtection(t *testing.T) {
	b := fixture(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rules.nft"), []byte("table inet nadkop {}\n"), 0600)
	os.WriteFile(filepath.Join(dir, "input.nft"), []byte("meta mark & 0x10000 == 0x10000 accept comment \"nadkop-tproxy\"\n"), 0600)
	var owned bool
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		text := strings.Join(args, " ")
		if strings.Contains(text, "list table") {
			if owned {
				return []byte("table inet nadkop {}"), nil
			}
			return nil, errors.New("missing")
		}
		return []byte("chain input {\n meta mark & 0x10000 == 0x10000 accept comment \"nadkop-tproxy\" # handle 123\n iifname \"other\" accept # handle 124\n}"), nil
	}
	path, err := b.nftTransaction(context.Background(), dir, run)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("delete rule inet fw4 input handle 123")) || bytes.Contains(data, []byte("handle 124")) || !bytes.Contains(data, []byte("insert rule inet fw4 input")) {
		t.Fatal(string(data))
	}
	owned = true
	if _, err := b.nftTransaction(context.Background(), dir, run); err == nil {
		t.Fatal("foreign table taken over")
	}
	os.MkdirAll(filepath.Join(b.runtime, "current"), 0700)
	os.WriteFile(filepath.Join(b.runtime, "current", "xray.json"), []byte(`{}`), 0600)
	path, err = b.nftTransaction(context.Background(), dir, run)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !bytes.HasPrefix(data, []byte("delete table inet nadkop\n")) {
		t.Fatal("replace is not transactional")
	}
}

func TestProductPolicyRollbackOnApplyFailure(t *testing.T) {
	for _, failure := range []string{"", "v4-rule", "v6-route", "nft"} {
		t.Run(failure, func(t *testing.T) {
			b := fixture(t)
			var calls []string
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				text := filepath.Base(name) + " " + strings.Join(args, " ")
				calls = append(calls, text)
				if strings.Contains(text, " show") {
					return []byte(`[]`), nil
				}
				if failure == "v4-rule" && strings.Contains(text, "-4 rule add") || failure == "v6-route" && strings.Contains(text, "-6 route add") || failure == "nft" && name == "/usr/sbin/nft" {
					return nil, errors.New("injected failure")
				}
				return nil, nil
			}
			err := b.applyNetwork(context.Background(), core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true, IPv6: true}}, "/temporary/apply.nft", run)
			if (err == nil) != (failure == "") {
				t.Fatal(err)
			}
			text := strings.Join(calls, "\n")
			if failure != "" && !strings.Contains(text, "-4 route del local default dev lo table 100") {
				t.Fatal("route not rolled back", text)
			}
			if failure == "nft" && (!strings.Contains(text, "-6 rule del") || !strings.Contains(text, "-4 rule del")) {
				t.Fatal("policy rules survived rollback", text)
			}
			if failure == "" && strings.Contains(text, " del ") {
				t.Fatal("successful apply rolled back", text)
			}
		})
	}
}

func TestProductStopRemovesOwnedPoliciesAndCurrent(t *testing.T) {
	b := fixture(t)
	gen := filepath.Join(b.runtime, "generation.test")
	os.MkdirAll(gen, 0700)
	encoded, _ := json.Marshal(core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true, Interfaces: []string{"br-lan"}, IPv6: true, Port: 12345, DNSPort: 1053}, SOCKSPort: 1080})
	os.WriteFile(filepath.Join(gen, "runtime.json"), encoded, 0600)
	os.WriteFile(filepath.Join(gen, "xray.json"), []byte(`{}`), 0600)
	os.Symlink(gen, filepath.Join(b.runtime, "current"))
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		text := filepath.Base(name) + " " + strings.Join(args, " ")
		calls = append(calls, text)
		if strings.Contains(text, "rule show") {
			return []byte(`[{"priority":10000,"table":100,"src":"all","fwmark":"0x10000","fwmask":"0x10000"}]`), nil
		}
		if strings.Contains(text, "route show") {
			return []byte(`[{"table":100,"type":"local","dst":"default","dev":"lo"}]`), nil
		}
		if strings.Contains(text, "list table") {
			return []byte("table inet nadkop {}"), nil
		}
		return nil, nil
	}
	if err := b.stopNetwork(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(b.runtime, "current")); !os.IsNotExist(err) {
		t.Fatal("firewall reload can restore a stopped service")
	}
	for _, family := range []string{"-4", "-6"} {
		if !strings.Contains(strings.Join(calls, "\n"), family+" rule del") {
			t.Fatal("policy survived stop", calls)
		}
	}
	if _, err := os.Stat(filepath.Join(gen, "runtime.json")); err != nil {
		t.Fatal("generation removed")
	}
}

func TestProductListenerParser(t *testing.T) {
	data := []byte("  sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n 0: 0100007F:0438 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 12345 1\n 1: 0100007F:0438 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 222 1\n")
	if got := occupiedSockets(data, true, map[int]bool{1080: true}); len(got) != 1 || got[0] != "12345" {
		t.Fatal(got)
	}
	if got := occupiedSockets(data, false, map[int]bool{1080: true}); len(got) != 2 {
		t.Fatal(got)
	}
	if got := occupiedSockets(data, true, map[int]bool{1081: true}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestProductStrictRuntimeAndRPCRequests(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"unknown":true}`, `{"transparent":{"enabled":true}}`} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "runtime.json"), []byte(raw), 0600)
		if _, err := readRuntime(dir); err == nil {
			t.Fatal("invalid runtime accepted", raw)
		}
	}
	for _, raw := range []string{"", `null`, `[]`, `{} {}`, `{"unknown":true}`, `{"content":"` + strings.Repeat("x", limit) + `"}`} {
		var out bytes.Buffer
		if err := Run([]string{"call", "unknown"}, strings.NewReader(raw), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Invalid or oversized request") {
			t.Fatal("invalid RPC request accepted", out.String())
		}
	}
}

func TestProductZapretDryRunUsesCandidateFiles(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`["--qnum=200","--uid=2147483647:2147483647","--hostlist=/var/run/nadkop/current/zapret-hosts.txt","--dpi-desync=fake"]`)
	os.WriteFile(filepath.Join(dir, "nfqws-args.json"), data, 0600)
	var called bool
	err := checkZapret(context.Background(), dir, func(_ context.Context, name string, args ...string) ([]byte, error) {
		called = true
		if name != "/usr/sbin/nfqws" || args[0] != "--dry-run" || !strings.Contains(strings.Join(args, " "), "--uid=0:0") || !strings.Contains(strings.Join(args, " "), "--hostlist="+dir+"/") || strings.Contains(strings.Join(args, " "), "/current/") {
			t.Fatal(args)
		}
		return nil, nil
	})
	if err != nil || !called {
		t.Fatal(err)
	}
	err = checkZapret(context.Background(), dir, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("private-value"), fmt.Errorf("private-value")
	})
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("missing or unredacted error", err)
	}
}

func TestProductAtomicWriteDoesNotFollowTargetSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	os.WriteFile(outside, []byte("keep"), 0600)
	target := filepath.Join(dir, "target")
	os.Symlink(outside, target)
	if err := atomicWrite(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "keep" {
		t.Fatal("target symlink followed")
	}
	info, _ := os.Lstat(target)
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
