# Acceptance of fixes by the lead agent

Stage after the core 0.3.0-r4 / LuCI 0.3.0-r8 audit. The fixes apply to the
current sources; saved packages from the previous audit were not rebuilt.
All eleven original blockers are closed in the current sources. Final
commands, exclusions of historical studies, and limitations are given below.

## C3 — accepted: S3-F02, E05

The lead agent read the geodata/build script changes, new tests, and
[C3 report](C3-boundaries-build.md). Independently executed:

- 13 top-level Go tests with race detector: PASS. Verified rejection of external
  symlinks, normal expansion/compile, atomic current, full backup/restore,
  and update errors. `Command and result` (local stand artifact),
  `JSONL` (local stand artifact).
- 56/56 build paths scenarios on copies of real parsers of two SDKs: PASS.
  Verified SDK/output immutability on failure and positive plain paths.
  `Log` (local stand artifact).

S3-F02 is closed for restored symlinks: root/generation/dat do not follow
unsupported links; the pair is read relative to the held generation fd.
Root ancestors are trusted; hostile writer, hardlinks, and mounts are not isolated.
E05 is closed by early rejection of unsupported project/SDK path before changes.
Building from paths with spaces is not implemented; the limitation is documented
in README. Full package builds in these tests are replaced by a safe make stub.

The S4 real-geodata fixture was migrated to the standard InstallGeoFiles; a small synthetic
pair passed through the geo-install runner. This is a fixture integration check, not a new
resource measurement of large databases. The ShellCheck profile with the final C2 init is verified below;
the historical B1 profile and audit evidence were not overwritten.

## C4 — accepted: S6-D01, S6-D02; separately S6-A01

The lead agent read the product diff, runner/harness changes, and new
assertions; cross-checked the [C4 report](C4-luci.md) against the results JSON. Independently:

- New 13/13 scenarios: PASS, including all 13 RPCs with nonzero codes,
  restart prohibition after failed save, draft/revision, dynamic DOM, and ARIA tabs.
  `Result` (local stand artifact).
- 14/14 targeted scenarios of the previous S6: PASS; before/after source hashes
  match. `Result` (local stand artifact).

Additionally verified the agent's full S6 results 84/84 and the second SDK.
The keyboard assertion change matches roving tabindex: Tab exits the
tablist, arrows switch tabs. Assertions of the original D01/D02 are not weakened.
The checks run in Chromium with the real view and SDK rpc.js; backend/service
are replaced by mocks. Real restart/procd, VM, and screen reader are not verified by this.

S6-D01/S6-D02 are closed in the current sources. Low-severity S6-A01 is also fixed
and verified separately; it is not counted among the closed blockers.

SHA256 of the accepted C3/C4 files are recorded in the
`manifest` (local stand artifact).

## C2 — accepted with an addition: D-S2-02, D-S2-03

The lead agent read the [C2 report](C2-network.md), network/init diff, unit tests,
namespace runner, and its checks for preservation of other people's rules. Independent Go/race
run: 13 top-level tests PASS.
`Result and command` (local stand artifact).

During integration an additional failure was found: after a successful `mv current`
an error of the `network commit` command interrupted init before Xray registration, although the network
was already applied. Two first/replacement scenarios with a commit fault were added;
both failed on the init handed over by the agent —
`reproduction` (local stand artifact).
This is a new boundary scenario of the fix, not a result of the original baseline.

The lead agent fixed init: if current still points at candidate,
the journal cleanup error is logged, processes are registered, and the journal remains
for recovery. A current mismatch still aborts the operation. The Go commit
command keeps returning errors; its generation checks are not weakened.
The runner gained a commit command fault, a `--case` filter, and `--binary` selection.

Final rerun on BusyBox ash: **40 scenarios, 250/250 assertions PASS**,
including the original 38 C2 scenarios and two new ones. A repeated start after
removing the commit fault deletes the published journal without manual repair.
`Results` (local stand artifact),
`command` (local stand artifact),
`log` (local stand artifact).

Stop no longer depends on reading runtime to clean up policies. Errors of
ln/mv and network commands roll back their own changes; a failed rollback
preserves the journal and the error. Other people's selectors/table/rules are not deleted.
This is a verification of the real CLI/init with command mocks in bwrap: real kernel
LAN, nft/ip-full JSON on OpenWrt, and procd are not confirmed by this run.
Crash after current before procd registration and hostile concurrent writers
are not part of the proven guarantee; no general crash transaction is claimed.

The ShellCheck profile was rerun on the final build/init: PASS, four previous
pointed init exclusions; the build script passes without exclusions.
`Log` (local stand artifact).
Eight-scenario profile check on isolated modified copies: 8/8;
verified a new warning, a syntax error, a change to an allowed string, and removal of the
arity guard. `Results` (local stand artifact).
Init syntax passes in sh/ash. The historical profile was unchanged.
SHA256 C2: `manifest` (local stand artifact).


## C1 — accepted: D-S2-01, S3-F01, S1-F1, S1-F2, S4-D01

Read the [C1 report](C1-state.md), product diff, and regression assertions.
The independent final run includes 17 top-level C1 tests, real CLI
and loopback HTTPS, both CLI/RPC orderings, early rejection of partial restore,
full restore with the same IDs, and exact 4 MiB ±1 byte serialization boundaries.
Successful retries are confirmed; tests verify bytes/inode/mtime/mode and absence of
a new file on failure. The overall final race run is below.

The parent applied three proposed expectation integrations: group-only partial
restore requires early rejection without a catalogue, CLI does not create an orphaned catalogue,
and inactive selection in the positive save test receives a real saved ID.
Additionally the overall run revealed an outdated diagnostic stage check: missing
IDs are now rejected in sources/load. The expectation was updated, and the routes check
is preserved as a separate legacy-tag case. Targeted diagnostics passed.
Test edits do not weaken the original safe-failure check.

S4 byte-budget was rerun separately: valid 4096 long tags were rejected before
publishing an oversized catalogue; the previous catalogue, IDs, and successful valid retry
are preserved. `Command` (local stand artifact),
`log` (local stand artifact).
The S4 geodata fixture is integrated and verified on a small synthetic pair:
`result` (local stand artifact).
CLI usage is supplemented with the working network rollback/commit.

C1 limitations: no general crash transaction across multiple state files was added;
an old oversized/incomplete catalogue is restored from backup; IDs are not guessed.
The .state.lock lock file requires a trusted writable configuration directory.

## Final regression and preserved limitations

All commands were run by the lead agent on a single integrated code set.
`GOMAXPROCS=2`; `C1_NADKOP_BIN` and `S3_NADKOP_BIN` pointed at the freshly built
`build/blocker-closure/parent/final/nadkop`. Results:

- `go build -p 1` and `go vet -p 1 ./...`: exit 0.
- Final `go test -race -p 1 -parallel 1 -count 1 -timeout 240s -json -skip
  '^(TestS3PartialHWIDCannotSilentlyReinitialize|TestS3RestoredWeakSensitiveModes|TestS3RestoredForeignOwners)$'
  ./...`: **159 top-level PASS, 0 FAIL, 11 SKIP**, exit 0, with no race detector messages.
  `Exact command/list` (local stand artifact),
  `full JSONL` (local stand artifact).
- Product UI, subscriptions, and picker: all three runners exit 0 in Chromium.
  `Commands and outcomes` (local stand artifact).
- Targeted C2/C3/C4 reruns, ShellCheck, and negative profile checks
  are listed above; they were run on the accepted versions of the respective files.

The three excluded S3 tests are **not closed defects**: S3-R01 (missing HWID
indistinguishable from bootstrap), S3-F03 (intentionally weak modes), S3-F04 (foreign UID).
They were explicitly separated from the 11 blockers in the original registry. The lead agent
reran them on the unchanged baseline: the same FAILs. Assertions are preserved.
`Cross-check` (local stand artifact),
`baseline JSONL` (local stand artifact).
The unfiltered overall run has exit 1 and **is not declared green**:
`original total` (local stand artifact).
Besides the three historical studies it revealed a diagnostic-stage expectation,
fixed and reverified in the final run above.

The 11 SKIP entries cover opt-in large geodatabases/load/protocols, real Xray/VM
checks, and subprocess entry helpers. S4 budget/fixture and the TLS child were executed
separately; the remaining skips are not declared completed external acceptance.
Kernel/LAN measurement, APK/IPK rebuild, install/boot of new packages,
physical router, and external VPNs were not run on the new snapshot.

Snapshot of the integrated code: `manifest` (local stand artifact).
After the final run, 394 code/test files were cross-checked — no changes.
All 398 baseline files are also intact: `verification` (local stand artifact).
Historical packages and audit evidence are not used as proof of
acceptance of the new code. Closure of the 11 blockers is not a release announcement.

## Completion of analysis X and handover

Two Sol 6.1 High analysts prepared reports and a subtotal; a separate
Terra 6.1 Max performed the critique. Read the [X summary](../improvements-X/final.md),
[critique](../improvements-X/terra-critique.md), and
[assignment registry with models/effort](../improvements-X/assignments.json).
Of 14 proposals, one is kept, ten are refined, three are excluded for now.
Proposals were not implemented; product positioning, purpose, and composition
are preserved. There are no new confirmed blockers. The narrow X reproduction of the known
F-S2-01 is not a new contract blocker or a power loss/VM check.

The latest X addition is accounted for: the summary was read after updating the acceptance and
the contract race run; the current SHA256 of final.md is saved in
[assignments.json](assignments.json). No new agents or product checks
were launched for reconciling documents. Shared registries were updated after tests;
the historical integrated-source.json is preserved without overwriting.
Final cross-check of sources, documents, and links:
`handback-verification.json` (local stand artifact).
