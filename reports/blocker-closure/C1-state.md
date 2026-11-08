# C1 — configuration, stable IDs, and write budgets

Implementer: gpt-6.1-sol, high. Date: 2026-11-02. No new agents launched.
All five assigned fixes are **applied to the product**; targeted checks of
the fixed behavior passed. Final closure of the shared registries and the overall
regression remain with the lead agent. Three conflicts with the new required behavior
remain in existing tests outside the permitted editing scope; the exact
verified integration is given below; the files themselves were not modified.

## Original behavior: what was reproduced independently

- **D-S2-01:** the real CLI and router test binary were built from the saved baseline.
  The original strace scenario with a delayed first flock was repeated: RPC save and CLI
  generate succeeded; the saved `srv_22222222222222222222222222222222` is absent
  from the catalogue; provider return issues a new ID and does not restore the old one.
  `build/blocker-closure/C1/baseline-race-verified.log` and
  `baseline-race-verified/save-vs-stale-generator/evidence.json` contain the outcomes.
  The original runner was used via import with all outputs redirected into C1;
  historical evidence was not rewritten. The first evidence parsing wrongly expected
  missingNodeRefs without the result wrapper: kept in baseline-race.log; then only
  the parsing was fixed and a new independent run was executed.
- **S3-F01:** before fixes, `TestS3PartialCatalogueMustNotRegenerateBoundIDs` revealed
  acceptance of all eight missing/empty/null/subset forms and loss of the original IDs.
  Log `baseline-s3-repro.log`, exit 1.
- **S1-F1:** before fixes, real loopback HTTPS tests confirmed successful
  addsource with a 4129-byte URL, an 81-character title, and a title with newline, followed by
  ReadConfig failure. Valid import and malformed controls were also executed.
  `baseline-s1-repro.log`, exit 0 means reproduction of the defects by the old test.
- **S1-F2:** the same original run wrote config of 4,243,741 bytes with a compact
  input of 4,096,193 and nodes of 4,284,523 bytes with an input of 3,842,109. Rereading
  fails. This was independently reproduced before the fix.
- **S4-D01:** the original TestS4Performance with catalogue-byte-budget: an allowed
  3,190,785-byte / 4096-node source wrote a 7,818,180-byte servers.json;
  the following summary and sync actually failed. `baseline-s4-repro.log`, exit 1.

## Applied changes

**D-S2-01.** LockConfiguration in core/config.go uses a separate inode
`.state.lock` next to config: renaming config does not release mutual exclusion.
CLI generate and nodes take the lock **before os.Open(config)** and hold it until
the operation completes. RPC keeps the previous ui.lock, then takes the same state
lock in non-blocking mode. Probe also synchronizes the catalogue via load and
now goes through b.lock in rpc.go. When busy, RPC fails without writes;
CLI waits and then reads the current config. Order is ui → state → catalogue;
SyncServers/CheckServerPrune do not re-acquire state.

Updater calls Apply → RPC service with a single outer acquisition. The prepared
RPC service passes NADKOP_PREPARED to init; this path does not rerun CLI generate.
A direct init start without prepared goes to Apply before procd. This was
verified along the call chain; no real host init/service was started.

**S3-F01.** The reader no longer converts existing empty/null/schema-incomplete
files into a new catalogue. After reading and **before reconcile**, all
saved nodeRef, nodeRefs, and pins are checked, including inactive fields. Any single missing
ID is enough for rejection. The check applies to full, scoped sync, and prune.
A missing catalogue without bindings remains valid bootstrap. Full restore
preserves the original IDs; disappeared nodes stay missing-records with the same IDs.

**S1-F1.** Addsource rejects Title in options per the existing
"headers/format/HWID only" declaration, URLs >4096, and URLs without hostname. The full future
configuration is serialized, checked against the final budget, and passed through
ReadConfig **before sync and writing**. The preliminary Prune is replaced by a read-only summary:
an import error no longer clears the catalogue before the future configuration is checked.
SyncImportedSourceServers merges cleanup of removed sources and addition
into a single candidate publication. The shared SyncSourceServers keeps the previous
behavior: other sources are not marked missing even with an empty Config.

**S1-F2.** encodeSavedJSON checks MarshalIndent + final newline separately
for config and nodes before modifying any file. CheckServerPrune checks in advance
bindings and the final bytes of the future pruned catalogue before config/nodes
writes. A failure of these guards preserves bytes, inode, mtime, mode, and presence of all
three files. This is a preliminary check, not a new multi-file crash transaction.

**S4-D01.** The writer checks len(serialized catalogue)+1 against the same 4-MiB
reader limit before creating temp or rename. An oversized result is explicitly rejected;
the previous catalogue is not truncated and stays readable with the previous IDs. A subsequent
valid refresh works. An already corrupted oversized catalogue from the old version
is not automatically restored.

## Changed files

Product:

- cmd/nadkop/main.go
- internal/core/config.go
- internal/core/servers.go
- internal/router/rpc.go
- internal/router/source_import.go

Permitted existing audit tests:

- internal/router/s1_security_test.go — old defect-PASS assertions replaced with
  a failure and file-immutability requirement; added a successful HTTPS URL of exactly 4096.
- internal/core/s4_performance_test.go — instead of expecting a successful oversized write,
  failure with an unchanged previous catalogue and successful read/retry is required.

New tests:

- cmd/nadkop/closure_c1_cli_test.go
- internal/core/closure_c1_state_test.go
- internal/router/closure_c1_storage_test.go
- internal/router/closure_c1_concurrency_test.go

Report: reports/blocker-closure/C1-state.md. All permanent C1 artifacts are only in
build/blocker-closure/C1/. applied.diff, new-tests.diff, and changed-files.json
contain the exact changes and SHA256; baseline was not rewritten. C1 did not edit go.mod/dependencies,
versions, README, UI, network/init/build/geodata, or historical audit reports.
Shared registries/summary in the shared workspace already differ from
baseline; C1 did not write them. s3_backup_test.go and rpc_test.go match baseline
by SHA256 — protected-files.json records the comparison, not authorship of other people's edits.

## Targeted checks and results

All Go commands with GOMAXPROCS=2 and -p 1, without a full SDK/build or deployments.

```sh
build/blocker-closure/C1/run-targeted.sh
C1_NADKOP_BIN="$PWD/build/blocker-closure/C1/nadkop" GOMAXPROCS=2 \
  go test -race -p 1 -parallel 1 -count 1 -timeout 180s -json \
  -run '^TestClosureC1|^TestS1|^TestServer|^TestScoped|^TestAddSource|^TestS2SaveRefreshShareUILock$' \
  ./cmd/nadkop ./internal/core ./internal/router
GOMAXPROCS=2 go test -race -p 1 -parallel 1 -count 1 -timeout 60s -json \
  -run '^TestClosureC1RPCSharesConfigurationLock$|^TestS1Probe|^TestProbeRequestValidation$' ./internal/router
```

- closure-final.jsonl: **17 top-level PASS**, 159 PASS events with subcases,
  0 FAIL. One top-level is a subprocess entry helper; it was separately executed by two
  CLI children. These are not 159 independent criteria.
- targeted-verified.jsonl: **49 top-level PASS**, 246 PASS events, 0 FAIL;
  1 SKIP — TLS child outside the fixture, actually executed by the parent for six HTTPS
  scenarios. In the same file after the JSON — a successful S4 byte-budget run.
- race-final.jsonl: **42 top-level PASS**, 216 PASS events, 1 helper SKIP,
  0 FAIL, no race detector messages. Core 19.048 s, router 103.008 s.
- After adding the probe lock: probe-lock-race.jsonl — 3 PASS, 0 FAIL,
  race detector clean. probe-lock-final.log — requests/lock passed;
  TestProbeRealXray skipped as VM-only, which is not presented as a real Xray test.
- s1-url-title-final.log — final URL/title assertions, exit 0.
- s4-budget-verified.log — the 4096-long-tags result rejected; the previous
  catalogue keeps size/inode/mtime/hash; summary and valid retry successful.
- import-regression.log, scoped-regression.log keep the namespace checks,
  other-source availability, tombstones, stable IDs, and cleanup.

New negative/positive boundaries:

- 8 partial catalogue forms × 4 binding kinds × 3 operations = 96 core subcases:
  missing, empty, whitespace, version-only, null servers, empty servers, JSON null,
  nonempty subset; single/group/pin/inactive; sync/scoped/prune. Rejection without
  file modification, including a missing file that stays missing.
- Real CLI nodes and generate: 8 partial forms × single/group/pin plus complete
  forms. Partial does not change config/nodes/catalogue and does not publish runtime; complete
  preserves IDs. Separately, full restore compiles successfully; bootstrap is allowed.
- Config, nodes, and catalogue: final **4,194,303 / 4,194,304 / 4,194,305**
  bytes with newline. Allowed sizes were actually written and reread; +1 failed
  without changes to related files. For catalogue, the available true→false
  transition checks exactly the last added letter, not writer padding bypass.
- A real CLI blocked on the state lock does not read the old incomplete JSON;
  after commit it reads the new valid configuration (nodes and generate).
- A real HTTPS fetch holds the CLI after reading config: RPC save fails at that time.
  Opposite order: RPC save holds state during preflight,
  CLI waits until reading config; after save the ID of the disappeared beta is retained,
  generation fails on missing server; beta return generates runtime with
  the same ID. Successful retries confirm absence of nested-lock deadlock.
- Renaming config does not bypass the lock; symlink and directory instead of the state lock are rejected.
- Addsource config amplification and oversized catalogue are rejected with the previous
  config/nodes/catalogue intact. Valid local import, duplicate, HTTPS, and boundary
  URL pass. Inactive draft fields with a **known** ID are preserved; compile
  ignores them; the catalogue retains the original missing ID.

## Required test integration by the lead agent

Did not apply edits to other people's/forbidden test files. Exact diff:
`build/blocker-closure/C1/required-test-integration.patch`.

1. internal/core/s3_backup_test.go, TestS3GroupOnlyPartialCatalogueBlocksUnresolvedMembers:
   the old test requires successful sync with a missing catalogue and group bindings.
   This contradicts the new explicit requirement. A sync error is required, plus verification
   that the catalogue file did not appear. The old test was actually executed and the FAIL is kept
   in historical-group-conflict.log.
2. Same file, TestS3CLIGenerateAfterRestore/missing-catalogue: the old
   `reused != 3` condition wrongly requires return of IDs from a deleted catalogue even
   after a correct early rejection. Required: 0 records, no file, and
   no runtime at exit 1. Before integration the test FAILs; complete subtest PASSes.
3. internal/router/rpc_test.go, TestSaveRetainsDisabledFieldsAcrossReload: the fixture
   contains a fictitious inactive nodeRef without a catalogue. To preserve the
   draft/disabled-fields check, a valid missing-record with this ID must be seeded in advance.
   The new closure check already verifies exactly this positive path.

All three changes were verified **without writing other people's files** via Go -overlay:

```sh
GOMAXPROCS=2 S3_NADKOP_BIN="$PWD/build/blocker-closure/C1/nadkop" \
  go test -overlay "$PWD/build/blocker-closure/C1/test-integration-overlay.json" \
  -p 1 -parallel 1 -count 1 -timeout 60s -json \
  -run '^TestSaveRetainsDisabledFieldsAcrossReload$|^TestS3GroupOnlyPartialCatalogueBlocksUnresolvedMembers$|^TestS3CLIGenerateAfterRestore$' \
  ./internal/core ./internal/router
```

integration-overlay.jsonl: **3 top-level PASS / 5 PASS events**, 0 FAIL.
Before integration, the overall regression on these old assertions stays red.
The previous extended run with these conflicts is not hidden:
targeted-final.jsonl (exit 1). Intermediate fixture test errors and pointer
stamp comparisons are also kept in closure-second.log and s4-budget-final.log;
they were fixed without weakening checks and reverified.

## Limitations and remaining issues

- No general crash-atomicity guarantee for config/nodes/catalogue was added.
  Cross-file fsync/SIGKILL cases F-S2-01 remain a separate historical boundary.
- Restoring an already oversized or incomplete catalogue requires the original backup;
  IDs are not guessed and the file is not silently rewritten. Picker.Order is not a binding and
  by itself does not require preserving every missing ID, as before.
- No real OpenWrt service/procd/Xray launch, live RPC/VM, host firewall,
  routes/sysctl changes, or external subscriptions. Sources/credentials are synthetic;
  HTTP/TLS loopback only. Procfs is used to observe own children.
- Full overall regression, SDK builds, and final registries were not run by C1.
  The lead agent needs the three specified test integrations and an overall run.
