package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nadkop/internal/core"
)

func podkopRun(nft, rule, dhcp string, installed bool) commandRunner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		text := strings.Join(args, " ")
		switch filepath.Base(name) {
		case "ip":
			if strings.Contains(text, "link show") {
				return []byte("2: br-lan: <BROADCAST> mtu 1500"), nil
			}
			if strings.Contains(text, "rule show") {
				// policyState uses "ip -j ... rule show" (JSON), Podkop
				// detection uses plain "ip ... rule show" (text).
				if strings.Contains(text, "-j") {
					return []byte(`[]`), nil
				}
				if rule == "" || rule == "[]" {
					return []byte(""), nil
				}
				return []byte(rule), nil
			}
			return []byte(`[]`), nil
		case "nft":
			if strings.Contains(text, "PodkopTable") {
				if nft == "" {
					return nil, errors.New("missing")
				}
				return []byte(nft), nil
			}
			return nil, errors.New("missing")
		case "uci":
			if strings.Contains(text, "flow_offloading") {
				return []byte("0\n"), nil
			}
			if strings.Contains(text, "auto_includes") {
				return []byte("1\n"), nil
			}
			return []byte(dhcp), nil
		case "sh":
			if installed {
				return []byte(""), nil
			}
			return nil, errors.New("absent")
		}
		return []byte(`[]`), nil
	}
}

func transparentPlan() core.RuntimeSettings {
	return core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true, Interfaces: []string{"br-lan"}}}
}

func TestPodkopDetectionSignals(t *testing.T) {
	live := "table inet PodkopTable { chain mangle { type filter hook prerouting priority -150; meta mark set 0x00100000 tproxy ip to 127.0.0.1:1602 } }"
	rule := "105: from all fwmark 0x100000/0x100000 lookup podkop"
	dhcp := "dhcp.@dnsmasq[0].server='127.0.0.42'\ndhcp.@dnsmasq[0].podkop_noresolv='0'\n"
	for _, tc := range []struct {
		name            string
		nft, rule, dhcp string
		installed       bool
		present, active bool
	}{
		{"absent", "", "[]", "", false, false, false},
		{"installed-only", "", "[]", "", true, false, false},
		{"stale-table", live, "[]", "", false, true, false},
		{"live-full", live, rule, dhcp, true, true, true},
		{"policy-only", "", rule, "", false, true, true},
		{"dns-only", "", "[]", dhcp, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := detectPodkop(context.Background(), podkopRun(tc.nft, tc.rule, tc.dhcp, tc.installed))
			if st.Present() != tc.present || st.Active() != tc.active {
				t.Fatalf("got present=%v active=%v", st.Present(), st.Active())
			}
		})
	}
}

func TestTransparentRefusesLivePodkop(t *testing.T) {
	live := "table inet PodkopTable { chain mangle { meta mark set 0x00100000 tproxy ip to 127.0.0.1:1602 } }"
	rule := "105: from all fwmark 0x100000/0x100000 lookup podkop"
	// Full interception: nft + policy + DNS hijack.
	err := checkNetwork(context.Background(), transparentPlan(),
		podkopRun(live, rule, "dhcp.@dnsmasq[0].server='127.0.0.42'\n", true))
	if err == nil || !strings.Contains(err.Error(), "Podkop") {
		t.Fatalf("live Podkop allowed: %v", err)
	}
	// Policy routing alone is enough to refuse: tproxy already redirects.
	err = checkNetwork(context.Background(), transparentPlan(), podkopRun("", rule, "", false))
	if err == nil || !strings.Contains(err.Error(), "Podkop") {
		t.Fatalf("Podkop policy allowed: %v", err)
	}
	// A leftover nft table without policy or DNS hijack must not block.
	err = checkNetwork(context.Background(), transparentPlan(), podkopRun(live, "[]", "", false))
	if err != nil {
		t.Fatalf("stale table refused: %v", err)
	}
	// Installed but never started must not block either.
	err = checkNetwork(context.Background(), transparentPlan(), podkopRun("", "[]", "", true))
	if err != nil {
		t.Fatalf("idle install refused: %v", err)
	}
}

func TestCoexistenceStepWarnsWithoutFailing(t *testing.T) {
	b := fixture(t)
	if err := os.MkdirAll(b.runtime, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"sources":[],"rules":[{"name":"VPN","action":"zapret","domains":["test.example"]}],"wanInterface":"eth0","zapretArgs":["--dpi-desync=fake"]}`
	if err := os.WriteFile(b.config, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	live := "table inet PodkopTable { mark 0x00100000 tproxy ip to 127.0.0.1:1602 }"
	report := b.diagnose(context.Background(), func(ctx context.Context, name string, args ...string) ([]byte, error) {
		text := strings.Join(args, " ")
		if filepath.Base(name) == "ubus" {
			return []byte(`{}`), nil
		}
		if filepath.Base(name) == "nft" {
			if strings.Contains(text, "PodkopTable") {
				return []byte(live), nil
			}
			if len(args) > 0 && args[0] == "-c" {
				return nil, nil
			}
			return nil, errors.New("missing")
		}
		if filepath.Base(name) == "ip" {
			return []byte(`[]`), nil
		}
		return nil, nil
	})
	if !report.OK {
		t.Fatalf("warning failed the check: %+v", report)
	}
	found := false
	for _, s := range report.Steps {
		if s.ID == "coexistence" {
			found = true
			if s.State != "warning" || !strings.Contains(s.Detail, "NFQUEUE") {
				t.Fatalf("bad coexistence step: %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("no coexistence step: %+v", report)
	}
}
