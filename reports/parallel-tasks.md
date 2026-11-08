# Additional parallel checks — GPT 6.1 Sol Max

At the user's request, six separate GPT 6.1 Sol agents are launched
with reasoningEffort=max. They complement the current B1–B3 on Terra Max.

| Task | Scope | Result criterion | Report |
| --- | --- | --- | --- |
| S1 — RPC and subscription security | ACL, secret leaks, HTTPS redirects, JSON limits/structure, file/symlink boundary | Synthetic positive and negative checks with evidence; reproducible defects separately | agent-audit/S1-security.md |
| S2 — crash durability | Stops between writes/publication, write/fsync/rename errors, constrained space | State of config/nodes/catalogue/current after failure, reference preservation and atomicity boundaries | agent-audit/S2-crash-consistency.md |
| S3 — backup | sysupgrade backup contents, restore of config/nodes/servers/HWID/geodata, loss/corruption of parts | Restore of the synthetic set and the same IDs/rules; no silent identity reset | agent-audit/S3-backup-restore.md |
| S4 — load and resources | Normal/large subscriptions, rules and geodatabases, repeated generation and updates | Measurable CPU/RAM/time with volumes, limits and a reproducible runner; no promises for untested hardware | agent-audit/S4-performance.md |
| S5 — protocols and transports | VLESS/Trojan/Shadowsocks, TCP/WS/gRPC/XHTTP, TLS/REALITY and outbound chains | Real local client/server Xray smoke tests; parser/preflight and full exchange marked separately | agent-audit/S5-protocol-compatibility.md |
| S6 — LuCI resilience | Slow/broken RPCs, tabs and revisions, repeated clicks, draft save, keyboard/mobile | Reproducible headless browser checks and exact results, without writing user settings | agent-audit/S6-luci-resilience.md |

## Work split

- All 327 production files are protected by the manifest
  `runs/resume-0.3.0-r4-ui-r8/frozen-source.json` and are not fixed after tests.
- Each agent writes only its own report and unique S*-prefixed test files.
  Artifacts and logs go to `build/agent-audit/S*/`. Others' tests and shared runners are not modified.
- While running, B2 used the VM 203.0.113.10 exclusively; B3 used its own VMs.
  S1–S6 do not touch these VMs and do not change host firewall, routes, sysctl, clock.
- All data is synthetic. User subscriptions, HWIDs and credentials are not
  used and are not included in reports.
- An actual application error is recorded with a reproduction. The proposed
  patch is stored separately, without applying it to the frozen application.
- Resource tests constrain their own processes; the host disk is not filled,
  heavy VMs and shared SDKs are not rebuilt concurrently.

Links and identifiers of launched tasks are preserved in
`agent-audit/sol-max-assignments.json`.

## Interim statuses

S5 accepted: after resumption on the same model, the final
report was published and verified. 81 scenarios, 50 HTTP exchanges and 44 UDP responses on local Xray 26.3.27;
import/parameter checks and expected rejections are counted separately. The first provider
limit is preserved as history. [Report](agent-audit/S5-protocol-compatibility.md).

S3 completed with open defects S3-F01/S3-F02: 10 PASS / 6 FAIL / 0 SKIP
at the top level. The full backup passed; the partial catalogue and external geodata
symlinks need a decision after the freeze. Conditional mode/UID notes and HWID
loss are separated from confirmed defects. [Report](agent-audit/S3-backup-restore.md).

S2 completed with three open contract defects D-S2-01/02/03.
227 assertions confirm behavior records, including defects; network
ran with external mocks. F-S2-01 is a separate crash-consistency boundary.
[Report](agent-audit/S2-crash-consistency.md).

S1 accepted: two open P2 write defects in addsource/save. Normal/race runs pass
as behavior tests, including defect reproduction. [Report](agent-audit/S1-security.md).

S4 accepted: one open high S4-D01 (catalogue size), a separate protective
RSS stop and three skips. [Report](agent-audit/S4-performance.md).

S6 accepted: 73 PASS / 11 FAIL, two P2 causes and a separate P3 accessibility A01.
View/SDK rpc.js were verified with a mock backend. [Report](agent-audit/S6-luci-resilience.md).

All S1–S6 and B1–B3 are complete, results accepted. B2 confirmed real hourly
success/failure cycles of the local source, restored the VM; the lead agent separately
verified the current state over read-only SSH. [Report B2](agent-audit/B2-updater.md).
Acceptance of the whole product remains blocked by the open defects.
[Summary](agent-audit/summary.md).
