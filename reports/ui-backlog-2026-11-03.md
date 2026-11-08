# What the Nadkop UI is missing

Date: 2026-11-03. Basis: current LuCI 0.3.0-r9 and backend 0.3.0-r5 sources, previously accepted C1–C4/X1. This is a task list after reading the code, with no new browser or user acceptance. The items below are not declared implemented and are not new confirmed release blockers.

P1 — first, P2 — next stage. Previously verified fixes for focus, save statuses, Zapret accessibility and tabs are not repeated here.

Update 2026-11-04: UI-01 was implemented and verified separately; contract, commands,
results and limitations are in the [auto-tuning report](zapret-tuner-2026-11-03.md)
and the [documentation](../docs/zapret-tuner.md). The table below keeps the original statement.

| ID | Priority | What is missing | What the user should get |
| --- | --- | --- | --- |
| UI-01 | P1 | Automatic Zapret strategy tuning. "Settings → Zapret" currently has only WAN and manual nfqws arguments. | Running a check of selected sites, progress, stop, comparison against strategy-free access, per-site/protocol results, a verified strategy and separate application. A failure must not be called a found strategy. |
| UI-02 | P1 | Protection of the unsaved draft on page close/refresh. There is a change status, but no beforeunload handler or draft recovery. | A warning when leaving a modified page. A button to return to the saved state with confirmation. If draft recovery appears — separately decide storage of secret-bearing fields; do not write them to localStorage automatically. |
| UI-03 | P1 | Checking a specific destination against the rules. Diagnostics verifies configuration, engines and network; there is no "site/IP → which rule will match" field. | Entering a domain or IP, the number and name of the first matched rule, the chosen action and the reason. Distinguish route calculation from a real network check; mark DNS-dependent results explicitly. |
| UI-04 | P1 | An explicit distinction between the current draft, saved settings and the applied generation. Save/apply statuses are already fixed, but there is no persistent state comparison. | A short "has unapplied changes" marker after saving without applying and after reopening the page. Rely on revisions/generation, not on a single local text or date. |
| UI-05 | P1 | Network-setting errors are not pinned to a specific field as consistently as rule errors. LAN/WAN, busy ports and flow offloading are mostly verified by the backend on apply. | Pointing at the problem field with navigation to it. An invalid choice is blocked with a reason; other fields' values are preserved. The backend confirms actual port occupancy and network state. |
| UI-06 | P2 | Per-subscription refresh state. The header currently shows the source and server count, edit and delete; there is no separate history of the last attempt and success. | The last successful refresh, a short error of the last attempt, and refresh of a specific subscription. Distinguish subscription refresh from availability probing of its servers. |
| UI-07 | P2 | Server-check freshness marker. The probe result lives in the open editor's memory; the card has no timestamp and no explicit distinction between a draft check and the monitor's active choice. | Time and method of the check, a stale state, a clear marker of the active server. Do not promise an ICMP ping or URL check if a different method ran. |
| UI-08 | P2 | Backup/restore from Nadkop itself. The "Import" tab imports nodes; there is no full application-state export here. | Export and verification of the full config/nodes/catalogue/HWID set and geodatabases per the chosen contract. A partial import must not silently change IDs and bindings. Account for the existing OpenWrt backup and the documented restore limitations. |
| UI-09 | P2 | Convenient management of a large rule list. There is add, rename, ordering and delete; there is no rule search or copy. | Search by name/condition without changing the real order. Rule copying as an explicit action. Temporarily disabling a rule will require separate schema/runtime support; merely hiding the card is not enough. |
| UI-10 | P2 | Import outcome before replacing local nodes. Text/JSON is currently imported into the editor, but there is no "what will be added/removed" comparison or mode choice. | A preview of counts and valid/erroneous entries; an explicit replace-or-merge choice when both modes are implemented. Stored bindings are verified before writing. |

## Code basis

- [LuCI view](../openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js): `save`, `sources`, `probeText`, `renderGeoSettings`, formation of the settings/nodes/diagnostics tabs and bottom buttons.
- [Diagnostics](../internal/router/diagnostics.go): config/sources/routes/xray/nfqws/nftables steps; no separate user route query.
- [Network configuration](../internal/core/network.go): interface, port and DNS constraints to align with UI validation.
- [Acceptance of previous changes](blocker-closure/acceptance.md), [implemented X1](improvements-X/implementation.md): already closed issues and confirmation limits.

## Next story: Zapret auto-tuning

UI-01 is split into a separate implementation. It needs a cancellable background task, trustworthy network probes through the strategy under test, recovery of temporary rules/processes on error and cancellation, a clear outcome, preservation of manual arguments and separate application of the found result. Full enumeration and network testing on the dedicated stand; do not replace working settings for the sake of the experiment.

Acceptance criteria: confirmed results instead of progress imitation, a clear reason when no suitable strategy exists, failure and cancellation tests, reruns, desktop/mobile in LuCI themes, no secrets in the log. Universal effectiveness for any provider does not follow from successful tests on one stand.
