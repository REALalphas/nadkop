package router

import (
	"context"
	"strings"
)

// Coexistence with Podkop (itdoginfo/podkop, sing-box based transparent
// proxy). Live findings from the OpenWrt VM, 2026-11:
//   - Podkop uses its own nft table "inet PodkopTable" (capital P) and a
//     separate policy route (fwmark 0x100000/0x100000, table podkop/prio 105),
//     so no name collision with nadkop's "inet nadkop", mark 0x10000,
//     table 100, priority 10000.
//   - The two transparent modes are mutually exclusive: both claim the
//     198.18.0.0/15 + fc00::/18 FakeDNS ranges, both hook prerouting/mangle,
//     both intercept port 53 (nadkop :1053 TPROXY vs Podkop's
//     dnsmasq -> 127.0.0.42 sing-box fakeip). Enabling both means
//     double-mark/double-tproxy/blackhole.
//   - Podkop forces dnsmasq to sing-box fakeip by default
//     (server=127.0.0.42, its own podkop_* UCI backups), so nadkop
//     transparent DNS never sees client queries.
//   - Podkop owns the sing-box procd service (/etc/sing-box/config.json).
//     Nadkop's Zapret NFQUEUE (match mark & 0x60000000 == 0) desyncs
//     sing-box upstream TLS because Podkop marks its outbound packets with
//     0x100000 which passes that filter.
//
// Mitigation, as agreed: hard refusal of transparent routing while Podkop
// interception is live; non-fatal warning when Podkop is merely present
// (or present with zapret rules compiled in).
//
// Detection is intentionally convergent (4 independent signals). A single
// stale nft table left after "podkop stop" must not block nadkop forever,
// and one missing probe (e.g. no ip-full output) must not hide a live
// interception either.
type podkopState struct {
	// NFT table "inet PodkopTable" exists (live redirection rules).
	NFT bool
	// Policy routing for fwmark 0x100000 table 105/podkop is in place.
	Policy bool
	// DHCP server 127.0.0.42 (Podkop's sing-box dns-in) or podkop_* UCI
	// backup options present in dhcp.@dnsmasq — Podkop owns DNS.
	DNS bool
	// Podkop init script is installed.
	Installed bool
	present   bool
}

func (p podkopState) Present() bool {
	return p.present
}

func detectPodkop(ctx context.Context, run commandRunner) podkopState {
	var st podkopState
	out, err := run(ctx, "/usr/sbin/nft", "list", "table", "inet", "PodkopTable")
	if err == nil && len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "tproxy") || strings.Contains(line, "0x100000") || strings.Contains(line, "1602") {
				st.NFT = true
				break
			}
		}
	}
	for _, family := range []string{"-4", "-6"} {
		out, err := run(ctx, "/sbin/ip", family, "rule", "show")
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.ToLower(string(out)))
		for i := range fields {
			if fields[i] == "fwmark" && i+1 < len(fields) && strings.HasPrefix(fields[i+1], "0x100000/") {
				st.Policy = true
			}
			if fields[i] == "lookup" && i+1 < len(fields) && (fields[i+1] == "podkop" || fields[i+1] == "105") {
				st.Policy = true
			}
		}
		if st.Policy {
			break
		}
	}
	if dhcp, err := run(ctx, "/sbin/uci", "-q", "show", "dhcp"); err == nil {
		for _, line := range strings.Split(string(dhcp), "\n") {
			if strings.Contains(line, "podkop_") || strings.Contains(line, "127.0.0.42") {
				st.DNS = true
				break
			}
		}
	}
	if _, err := run(ctx, "/bin/sh", "-c", "test -x /etc/init.d/podkop"); err == nil {
		st.Installed = true
	}
	// An installed but never-started init script leaves no traffic
	// footprint; it must not warn on every check or block anything.
	st.present = st.NFT || st.Policy || st.DNS
	return st
}

// Active reports whether Podkop currently intercepts LAN/DNS traffic.
// The nft table alone may be a leftover of "podkop stop"; it counts as
// active only together with policy routing, DNS hijack, or a running
// sing-box service. DNS hijack alone counts: Podkop rewrites dnsmasq
// even in modes where tproxy tolerates foreign traffic.
func (p podkopState) Active() bool {
	if !p.Present() {
		return false
	}
	return (p.NFT && (p.Policy || p.DNS)) || p.Policy || p.DNS
}

func podkopRefusal() string {
	return "Podkop interception is active (sing-box TPROXY/DNS on this router); " +
		"disable Podkop or its DHCP/DNS hijack before enabling Nadkop transparent routing — " +
		"running two transparent proxies breaks LAN traffic"
}

// coexistenceWarning is a non-fatal diagnostic outcome: it marks its step
// "warning" instead of failing the whole check.
type coexistenceWarning struct{ detail string }

func (w *coexistenceWarning) Error() string { return w.detail }

func podkopWarning(planHasZapret bool) string {
	if planHasZapret {
		return "Podkop is installed: its sing-box upstream TLS passes Nadkop Zapret NFQUEUE " +
			"(mark 0x100000 does not bypass the 0x60000000 mask), which can break Podkop tunnels; " +
			"list Podkop VPN endpoints in Zapret bypass prefixes or disable Zapret rules"
	}
	return "Podkop is installed on this router; keep Nadkop transparent routing and " +
		"Podkop interception mutually exclusive to avoid double FakeDNS/TPROXY capture"
}
