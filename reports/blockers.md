# Blockers — closure summary

**All 11 original blockers are closed in the current sources.** Fixes C1–C4
were implemented by Sol 6.1 High agents and accepted by the lead agent after integration and
verification. Low-priority S6-A01 was fixed separately. Summary date: 2026-11-03
(Europe/Moscow). Change base — core 0.3.0-r4 / LuCI 0.3.0-r8.

[Acceptance, teams and constraints](blocker-closure/acceptance.md),
[team](blocker-closure/team.md), [assignments and models](blocker-closure/assignments.json).
The stored APK/IPK packages were not rebuilt and do not contain these fixes.
Blocker closure in the sources is not a release announcement.

## Closed blockers

| ID | Fixed behavior | Evidence |
| --- | --- | --- |
| D-S2-01 | A shared lock before config read serializes CLI/RPC and preserves bound IDs | [C1](blocker-closure/C1-state.md) |
| S3-F01 | Schema and all stored bindings/pins are verified before reconcile; a partial restore is rejected without publishing new IDs | [C1](blocker-closure/C1-state.md) |
| S1-F1 | Addsource validates the future config, URL and options before writing the catalogue/config | [C1](blocker-closure/C1-state.md) |
| S1-F2 | The final serialized size of config/nodes, including the newline, is checked before writing | [C1](blocker-closure/C1-state.md) |
| S4-D01 | The catalogue writer honors the 4 MiB reader limit; a rejection preserves the previous catalogue and IDs | [C1](blocker-closure/C1-state.md) |
| D-S2-02 | Stop clears its own policies regardless of runtime readability and reports errors | [C2](blocker-closure/C2-network.md) |
| D-S2-03 | The journal and rollback restore the network on publish errors; an incomplete recovery is persisted for retry | [C2 and lead-agent addendum](blocker-closure/acceptance.md) |
| S3-F02 | Geodata is confined to direct generation.*; generation/dat symlinks are rejected, files are opened relative to the directory fd | [C3](blocker-closure/C3-boundaries-build.md) |
| E05 | Unsupported project/SDK paths are rejected before any SDK/output changes; building from paths with spaces is not supported | [C3](blocker-closure/C3-boundaries-build.md) |
| S6-D01 | Busy persists across DOM rebuilds, and a late draft edit does not get a false saved status | [C4](blocker-closure/C4-luci.md) |
| S6-D02 | All 13 RPCs reject a nonzero ubus code; no restart is requested after a failed save | [C4](blocker-closure/C4-luci.md) |

S6-A01 was verified separately: arrows, Home/End and roving tabindex of tabs.
The original reproductions are preserved in the [error registry](errors.md),
the [historical summary](agent-audit/summary.md) and
the `pre-fix registry copy` (local stand artifact).

## Final verification

- Contract Go run with race detector: **159 PASS / 0 FAIL / 11 SKIP**, exit 0.
  `Exact command and list` (local stand artifact).
- Fresh `go build`, `go vet` and three product UI/subscriptions/picker runners: exit 0.
- C2: 13 targeted Go/race tests and 40 CLI/init scenarios,
  **250/250 assertions**; including the cleanup error of an already published journal.
- C3: 13 targeted Go/race tests and 56/56 build-path scenarios against the parsers
  of two SDKs in the lead-agent rerun. The full build was replaced with a make stub.
- C4: 13/13 new and 14/14 targeted S6 scenarios in the lead-agent rerun;
  the full S6 at the implementer — 84/84. Backend/service were replaced with mocks.
- The final ShellCheck profile and 8/8 checks of its rejections passed.
  S4 catalogue-byte-budget and the stock geodata fixture were verified separately.

Three historical studies **S3-R01, S3-F03, S3-F04 are not fixed** and are explicitly
excluded from the contract run: restore/bootstrap distinction by HWID,
deliberately weak modes, and foreign UIDs of restored files. Their FAILs were rerun
on the unchanged baseline; assertions were not weakened. They were never part of
the 11 blockers. **The unfiltered test set does not fully pass.**
The 11 SKIP items include opt-in load/geodatabases/protocols, real Xray checks and
subprocess helpers; the full breakdown is in [acceptance](blocker-closure/acceptance.md).

## Historical B1–B3 and external conditions

B1–B3 are closed on the original audit snapshot; their VM/package results are not
a verification of the new sources. [B1](agent-audit/B1-shellcheck.md) analyzed the previous
nine ShellCheck warnings and the profile with seven exclusions. On the final
sources the profile was rerun: four previous init exclusions, zero for the build script;
new warnings and errors are rejected.

[B2](agent-audit/B2-updater.md) confirmed two real hourly updater firings
on a dedicated OpenWrt 25.12.6 VM and the restore of its state.
[B3](agent-audit/B3-install-boot.md) confirmed install, upgrade and cold boot
of the old APK/IPK on x86_64 VMs 24.10.8/25.12.6: 19/19 boot and 39/39 NFQUEUE on each.
These checks were not repeated on the new snapshot.

For the new sources, APK/IPK rebuild, install/boot,
verification of real kernel/LAN/procd, a physical router and external VPNs were not performed.
A shared crash transaction for config/nodes/catalogue (F-S2-01) is neither promised nor added.
Geodata boundaries trust root ancestors and do not isolate hostile writer/hardlinks/mounts.
The stored 398-file baseline was verified unchanged.

## Improvement analysis X

Sol 6.1 High completed the interface and performance review; a separate
Terra 6.1 Max critiqued the subtotal. [Outcome](improvements-X/final.md):
1 proposal kept, 10 refined, 3 excluded for now.
The [critique](improvements-X/terra-critique.md) revealed no new confirmed blockers.
No proposals were implemented; product positioning, purpose and composition are preserved.
