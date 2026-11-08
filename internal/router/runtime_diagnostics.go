package router

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Runtime diagnostics inspect the applied generation, never the unsaved editor.
// Fixed commands and summaries keep credentials and raw command output private.
func (b backend) runtimeChecks(ctx context.Context, run commandRunner, instances map[string]any, zapretRequired, applied bool, queuePath string) []diagnosticStep {
	running := func(name string) bool {
		instance, ok := instances[name].(map[string]any)
		return ok && instance["running"] == true
	}
	checks := []diagnosticStep{}
	add := func(id, title, state, detail string) {
		checks = append(checks, diagnosticStep{ID: id, Title: title, State: state, Detail: detail})
	}
	if running("xray") {
		add("runtime-xray", "Xray", "ok", "Xray process is running under Nadkop service supervision.")
	} else {
		add("runtime-xray", "Xray", "error", "Xray process is not running. Press 'Start'; if the start fails, check the saved configuration below.")
	}
	if !applied {
		add("runtime-zapret", "Zapret", "info", "Configuration is not applied yet. Select Zapret in a rule action, specify domains, IPs, or sets, and press 'Save and apply'.")
	} else if !zapretRequired {
		add("runtime-zapret", "Zapret", "info", "No applied Zapret rules. Select 'Action -> Zapret', specify domains, IPs, or sets, and press 'Save and apply'. Zapret does not start for 'Server' rules.")
	} else if running("nfqws") {
		add("runtime-zapret", "Zapret", "ok", "The nfqws process is running; the applied configuration contains Zapret rules.")
	} else {
		add("runtime-zapret", "Zapret", "error", "Zapret rules are applied, but the nfqws process is not running. Press 'Restart' and run the configuration check below; on repeated failure check the OpenWrt system log.")
	}
	if zapretRequired {
		queue, err := os.ReadFile(queuePath)
		attached := false
		for _, line := range strings.Split(string(queue), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == "200" {
				attached = true
			}
		}
		if err == nil && attached {
			add("runtime-queue", "Zapret queue", "ok", "nfqws is attached to NFQUEUE 200.")
		} else {
			add("runtime-queue", "Zapret queue", "error", "NFQUEUE 200 is not attached. Restart Nadkop; check that nfqws starts and kmod-nft-queue is installed.")
		}
		if _, err := run(ctx, "/usr/sbin/nft", "list", "table", "inet", "nadkop"); err == nil {
			add("runtime-firewall", "Zapret interception rules", "ok", "The inet nadkop table is installed in nftables.")
		} else {
			add("runtime-firewall", "Zapret interception rules", "error", "The inet nadkop table is missing. Press 'Save and apply' and check the nftables step below.")
		}
	}
	info, err := os.Stat(filepath.Join(b.runtime, "current", "health.json"))
	if err == nil && info.Size() > 0 {
		if running("monitor") {
			add("runtime-monitor", "Server selection", "ok", "Monitor is running: HTTP request via each server, 5s timeout; 15s pause after each cycle. If all servers fail, the route is blocked.")
		} else {
			add("runtime-monitor", "Server selection", "error", "The applied configuration has groups, but the monitor is not running. Restart Nadkop; on repeated failure check the system log.")
		}
		checks = append(checks, b.actionCheck(filepath.Join(b.runtime, "current", "monitor-status.json"), "monitor", "Last server selection cycle", instances))
	} else {
		add("runtime-monitor", "Server selection", "info", "No applied multi-server rules. Automatic checks start when a second server is added to a rule and settings are applied.")
	}
	plan, err := readRuntime(filepath.Join(b.runtime, "current"))
	if err == nil && plan.Transparent.Enabled {
		state, detail := "ok", "TCP/UDP and DNS of selected LAN devices are routed to Xray."
		if !running("xray") {
			state, detail = "error", "Xray is stopped; intercepted connections cannot pass. Restart the service."
		}
		if _, err := run(ctx, "/usr/sbin/nft", "list", "chain", "inet", "nadkop", "prerouting"); err != nil {
			state, detail = "error", "LAN interception chain is missing. Apply the configuration again."
		}
		for _, family := range []string{"-4", "-6"} {
			if family == "-6" && !plan.Transparent.IPv6 {
				continue
			}
			rule, route, err := policyState(ctx, family, run)
			if err != nil || !rule || !route {
				state, detail = "error", "Policy routing for table 100 is missing or conflicting. Run the check and apply settings."
			}
		}
		chain, err := run(ctx, "/usr/sbin/nft", "list", "chain", "inet", "fw4", "input")
		if err != nil || !strings.Contains(string(chain), `comment "nadkop-tproxy"`) {
			state, detail = "error", "The TPROXY ingress allowance is missing in firewall4. Apply settings."
		}
		add("runtime-lan", "LAN and DNS routing", state, detail)
	} else {
		add("runtime-lan", "LAN and DNS routing", "info", "Transparent routing is not applied. Enable it and select LAN devices in settings.")
	}
	if err == nil && plan.UpdateIntervalHours > 0 {
		if running("updater") {
			add("runtime-updater", "Subscription updates", "ok", "Automatic updates are enabled with validation before replacing the configuration.")
		} else {
			add("runtime-updater", "Subscription updates", "error", "Updates are enabled, but the process is not running. Restart the service.")
		}
		checks = append(checks, b.actionCheck(filepath.Join(b.runtime, "update-status.json"), "updater", "Last subscription update", instances))
	}
	return checks
}
