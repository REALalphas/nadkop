# Summary of additional checks

The post-audit fix stage is complete: **all 11 original blockers are closed in
the sources**, C1–C4 accepted by the lead agent; S6-A01 fixed separately.
[Acceptance evidence](../blocker-closure/acceptance.md),
[current registry](../blockers.md). Contract race run:
**159 PASS / 0 FAIL / 11 SKIP**. Three historical studies S3-R01/F03/F04
are explicitly excluded and remain unsuccessful; the unfiltered set does not fully
pass. No new APK/IPK built; no device tested on the new code.

The separate Sol 6.1 High analysis and Terra 6.1 Max critique are complete:
[outcome X](../improvements-X/final.md), [critique](../improvements-X/terra-critique.md).
One proposal kept, ten refined, three excluded for now;
proposals were not implemented, no new confirmed blockers.

## Historical outcome of the original audit

Further counts and limits describe the unchanged snapshot before the new
authorized fix stage. They are preserved for comparison with the original logs.

Core **0.3.0-r4**, LuCI **0.3.0-r8**. Full product readiness is **not confirmed**.
Open: **10 application defects: 4 high and 6 medium**, a separate
medium builder defect E05, and low accessibility S6-A01. Conditional
mode/UID remarks and crash/restore boundaries are not included in these counts.

All B1–B3 and S1–S6 are complete; their tests/logs were read by the lead agent. B2
confirmed the real hourly cycles of the local updater. No pending results.
No proposed patch was applied.
The user requirement "do not modify code after tests" is preserved.

| Task | Accepted result | Report |
| --- | --- | --- |
| B1 Terra Max | ShellCheck reviewed; profile PASS with 7 point exceptions. Original exit 1/9 warning preserved. E05 open separately | [B1](B1-shellcheck.md) |
| B2 Terra Max | Closed: real schedule, success after 3603.52 s, failure on next tick; route/PID/nodeRef preserved; VM restored | [B2](B2-updater.md) |
| B3 Terra Max | IPK 24.10.8 and APK 25.12.6 x86_64 VMs: install/upgrade/boot; 19/19 cold boot and 39/39 NFQUEUE on each | [B3](B3-install-boot.md) |
| S1 Sol Max | Normal and race: 15 top-level / 46 events PASS, helper separately; tests reproduce 2 P2 defects | [S1](S1-security.md) |
| S2 Sol Max | 48 syscall + 12 init/network + genuine ENOSPC. 227 assertions confirm behavior along with 3 defects | [S2](S2-crash-consistency.md) |
| S3 Sol Max | 10 PASS / 6 FAIL / 0 SKIP top-level. Full backup preserves 3/3 IDs; 2 defects and conditional remarks | [S3](S3-backup-restore.md) |
| S4 Sol Max | 132 measurements / 250 iterations; 20 scenarios PASS, 1 defect, 1 RSS stop, 3 SKIP | [S4](S4-performance.md) |
| S5 Sol Max | 81 scenario rows; 50 HTTP and 44 UDP responses; local Xray 26.3.27. No new defects | [S5](S5-protocol-compatibility.md) |
| S6 Sol Max | 84 scenarios: 73 PASS / 11 FAIL; 2 P2 defects and P3 ARIA tabs remark | [S6](S6-luci-resilience.md) |

## What blocks full acceptance

High: D-S2-01 — loss of bound ID on CLI/RPC; D-S2-03 — incomplete
network/current rollback; S3-F01 — new IDs on incomplete catalogue restore;
S4-D01 — catalogue publication above its own read limit.

Medium: S1-F1/F2 — invalid or oversized config/nodes after
successful write; D-S2-02 — incomplete stop on corrupt runtime; S3-F02 —
external geodata symlinks; S6-D01/D02 — incorrect saved UI state
on dynamic DOM and ubus errors. Separate E05 concerns building from a path with spaces.

Commands, impact, evidence limits, and unapplied proposals are in
the [error registry](../errors.md), [blockers](../blockers.md), and individual reports.
S6-A01 is not assigned as a standalone blocker. Six S3 FAILs and eleven S6 FAILs
do not equal the same number of distinct errors.

## Directions for the next architectural fix

These are decisions for a separate version change, not completed fixes:

1. Shared CLI/RPC locking before config read, and verification of saved bound IDs
   before reconcile; explicit set verification on partial restore.
2. Unified field and final serialized-size check before the first
   config/nodes/catalogue write; consistent reader/writer budgets.
3. Explicit network transaction with full rollback regardless of the old current;
   stop must explicitly handle unreadable runtime.
4. Geodata boundary check when opening generation/dat; explicit
   mode/owner and restored-identity policy.
5. Busy for dynamic DOM, version-based draft dirty, and rejection of nonzero
   ubus codes for all RPCs.

After the changes, a new code snapshot and a separate test stage are needed. A proposal
diff, dry-run, or green repro test alone does not close a defect.

## Integrity and limits

The lead agent re-confirmed SHA256 of **327/327 protected files** and
**six packages**. Acceptance reviews:
`build/agent-audit/parent/{B1,B2,S1,S2,S3,S4,S5,S6}-review/`.
Manifest: `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json`, SHA256
`09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.

B3 — real x86_64 OpenWrt VMs, S5 — real local Xray. S2 network failures
use command mocks, S6 — mock backend with genuine SDK rpc.js. S3 —
synthetic tar restore, not a device sysupgrade. B2 — real installed
updater/HTTP/SOCKS on the VM, local source; not a timed HTTPS/provider outage.
Physical router,
external VPNs, flash power loss, and target resource budgets were not tested.
