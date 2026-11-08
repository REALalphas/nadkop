# Errors and limitations of the current run

The current fix stage is complete: **all 11 original blockers are closed in
the sources**. The lead agent accepted C1–C4; S6-A01 was fixed separately.
[Acceptance](blocker-closure/acceptance.md), [current list](blockers.md).
Contract race run: **159 PASS / 0 FAIL / 11 SKIP**. The three previous
studies S3-R01/F03/F04 are explicitly excluded and remain failing; their FAILs
were rerun on the baseline. The unfiltered test set does not fully pass.
The new fixes are applied to the sources and are not part of the stored packages.
Historical reproductions are preserved below; line numbers and commands refer
to the original audit unless a current C1–C4 result is explicitly stated.

## Original audit results before fixes

For core 0.3.0-r4 / LuCI 0.3.0-r8, **10 open application defects are confirmed:
4 high and 6 medium severity** (S1, S2, S3, S4, S6). Builder E05 and
low-priority accessibility S6-A01 are tracked separately. The earlier E02/E03/E04
were resolved before the new freeze and re-verified. After the freeze of the current
application its code did not change. New failing tests are preserved without fixes.

The `rerun registry` (local stand log) preserves:

- R01: nine ShellCheck warnings;
- R02: the first lifecycle check without a timeout, passed after installing the dependency;
- R03: NXDOMAIN for synthetic dnsmasq names before Nadkop was enabled; with a prepared
  upstream, stock DNS 127.0.0.1 passed 35 checks;
- R04: a syntax typo in an auxiliary test wrapper, the application is unaffected;
- R05: bare fake breaks HTTP on the single-hop stand; fake+badseq passed 39 checks
  with a confirmed NFQUEUE counter and direct/proxy isolation.

Previous errors were not deleted: `initial run` (local stand log).
Confirmation boundaries and unverified external conditions are in the
`final report` (local stand log).


B3 addendum: clean installs, upgrades and cold boots on OpenWrt
24.10.8/25.12.6 passed; no new application defects were found. The first failures
of test fixtures and their rerun results are preserved in
[report B3](agent-audit/B3-install-boot.md). The original constraint on IPK verification
on 24.10 is lifted; hardware routers and external VPNs remain unverified.

## Addendum B1 — ShellCheck and the original E05 (closed by C3)

Current E05 status: closed by early rejection of an unsupported project/SDK path
before any SDK/output changes; lead-agent rerun 56/56 PASS. Full support
for paths with spaces is not claimed. [C3](blocker-closure/C3-boundaries-build.md).
Historical data of the original defect is preserved below.

R01 analyzed: plain ShellCheck keeps exit 1 and nine warnings.
The OpenWrt profile passes with seven targeted exclusions; two SC3043s
disappear when BusyBox is selected. Semantics verified in 54 cases, the profile —
in eight checks on copies. The first failures of test fixtures are preserved in
[report B1](agent-audit/B1-shellcheck.md); the original command is not declared successful.

**E05, original medium-severity defect (now closed by C3):** with spaces in the source path the builder
writes a feed that the real SDK 24.10.8 and 25.12.6 parsers split into
multiple sources. The `feeds/nadkop` reference becomes invalid, the package is not visible.
The space-free control succeeds. To reproduce:

```sh
python3 build/agent-audit/B1/feed_space_repro.py
python3 build/agent-audit/B1/feed_space_repro.py --sdk-root build/openwrt-vm/sdk-24.10.8
```

This is a reproduction of the feed error with a `make` stub (exit 73), not a full build.
The lead agent reran both cases; logs are in
`build/agent-audit/parent/B1-review/`. For the current space-free workspace there is
no obstacle. A separate `patch` (local stand artifact)
proposes early rejection for the unsupported path, but not full support
for spaces. At the audit stage the patch was not applied and E05 remained open;
the current C3 implementation and acceptance are stated above. At that time 327 product files and
six packages were verified by SHA256 with no mismatches.

## I01 — provider limit interrupted the first S5 run

The first Xray protocol verification run finished `failed` with the message
`You've hit your usage limit`. The final report was not yet published; the tests,
network logs and summary were preserved. When the notification was received, the orchestrator
was already showing an active second run of the same task on Sol Max. The lead agent did not
initiate it, did not change the model, and did not rerun the tests.

This is an audit-execution failure, not an application error. S5 remains open;
the preserved numbers are not included in the accepted passing checks. Details:
[S5 interim state](agent-audit/S5-status.md).

Addendum to I01: the task was resumed by the user in the same thread and successfully
completed the report. The lead agent accepted the tests and the original logs: S5 is closed for
local Xray 26.3.27. The first provider failure remains history, not a current
obstacle. No Nadkop defects were confirmed in S5. The first REALITY run
did not pass due to the short timeout of the synthetic target; only
S5 tests were changed, the successful rerun is preserved. [Final report](agent-audit/S5-protocol-compatibility.md).

## S3-F01 — closed by C1; original ID-match loss on partial restore

Schema and all stored bindings/pins, including inactive ones, are verified before
reconcile. A partial restore is rejected without publishing; a full one preserves IDs.
[C1 fix and checks](blocker-closure/C1-state.md),
[final acceptance](blocker-closure/acceptance.md). The original defect follows.

Condition: config.json already contains references and pins to stable IDs, while
servers.json is missing, empty/whitespace, null, version-only, null/empty servers,
or contains a subset of the original catalogue. SyncServers permits this state,
creates new IDs and writes the catalogue **before** Compile. For the seven empty/incomplete
forms — 0/3 previous IDs, for a non-empty subset — 1/3.

CLI generate then rejects a single old nodeRef: exit 1, no runtime
created, config not rewritten, yet servers.json already holds different IDs.
The group keeps unresolvable references and the block selector; server substitution or
direct bypass are not confirmed. A complete set preserves all 3/3 IDs.
Schema and stored bindings/pins must be verified before reconciliation.

Paths: `internal/core/servers.go:49`, `:66`, `:164`, `:364`;
`cmd/nadkop/main.go:105`, `internal/core/compile.go:190`.
Tests: TestS3PartialCatalogueMustNotRegenerateBoundIDs,
TestS3CLIGenerateAfterRestore/missing-catalogue.
The `proposed diff` (local stand artifact)
was not applied and not verified as a fixed product.

## S3-F02 — closed by C3; original symlink external-target defect

The applied fix was verified by the lead agent: 13 Go tests with race PASS,
including stock update/restore. [Trust boundary and evidence](blocker-closure/C3-boundaries-build.md).
The pre-fix behavior is described below.

Condition: a synthetic restore contains current with an absolute/relative escape,
a symlink for generation or a separate dat; the external target exists and contains
a valid protobuf database. In all 5/5 variants OpenGeoDatabase and CompileWithGeo
accept it. Reading a separate dat through a link permits mixing the pair.
This is database reading outside the expected directory; external sentinels were not modified,
secrets, remote exploitation and arbitrary tar writes were not tested.

Paths: `internal/core/geodata.go:240` (`EvalSymlinks`), `:247` (`os.Open`).
Test: TestS3GeodataArchiveSymlinkBoundary.
The `proposed diff` (local stand artifact)
was not applied and not verified as a fix.

## S3 — conditional notes and HWID limitation

- S3-F03: deliberately restored config/nodes/servers 0644 are read without
  normalization. A well-formed 0600 backup is not weakened; no actual leak is proven.
- S3-F04: in an isolated user namespace EUID 0 reads 8/8 files with UID/GID
  12345 and mode 0600/0644; the UID stays foreign. The policy of trusting restore metadata
  needs a decision. This is not a proven exploit on OpenWrt.
- S3-R01: a missing/empty/whitespace HWID is reinitialized in 3/3
  cases. DeviceID does not distinguish restore from a stock clean install. This is
  a partial-restore limitation; it needs an explicit preflight/manifest.

S3 finished with **exit 1**, **10 PASS / 6 FAIL / 0 SKIP** at the top level;
the F01 CLI recheck and conditional assertions are part of the six FAILs, so
these are not six separate confirmed defects. Reproduction:
`python3 scripts/test-s3-backup.py --label s3-verified`.
The full synthetic backup passed; real sysupgrade/boot/network were not tested.
The lead agent cross-checked the original tests, code and JSONL: the numbers matched. The application and
all patches were left unapplied; 327 files and six packages kept their SHA256.
[Report S3](agent-audit/S3-backup-restore.md),
`raw logs` (local stand artifact).

## D-S2-01 — closed by C1; original bound-ID loss on CLI/RPC

CLI and RPC use a shared .state.lock before config read; the RPC lock order is
ui → state → catalogue. Both orders of real CLI/RPC calls were verified with the race
detector. [C1](blocker-closure/C1-state.md), [acceptance](blocker-closure/acceptance.md).
The original pre-fix reproduction follows.

Condition: a separate CLI read the old config before catalogue.lock, while an RPC save
stored a new reference to the vanished server. Both calls succeed, but
CLI prune against the stale config deletes an already bound ID. The next load+compile
fails; returning the server creates a new ID and does not restore the reference.
Regular RPC/updater calls are serialized by ui.lock; this defect is not presented as a race of two
stock UI operations. The current runtime in the fixture was not switched.

Paths: `cmd/nadkop/main.go:84`, `internal/core/servers.go:119`,
`internal/router/rpc.go:240`, `:306`. Evidence:
`build/agent-audit/S2/fixtures/accepted/save-vs-stale-generator/`.
The `proposed shared-lock diff` (local stand artifact)
was not applied.

## D-S2-02 — closed by C2; original false stop success

Stop clears exact-owned policies regardless of runtime, reports errors and
preserves retry. [Acceptance](blocker-closure/acceptance.md).
The original pre-fix reproduction is below.

Condition: `current/runtime.json` is corrupted, Nadkop-owned policy route/rule
objects already exist. The `readRuntime` error is ignored in `internal/router/network.go:328`;
stop removes nft/current, does not call ip cleanup and returns 0. In the namespace
model, two IPv4 policy objects persisted. The application origin of the runtime corruption
is not proven; real kernel routes and traffic were not tested.
Evidence: `build/agent-audit/S2/namespace-fixtures/final2/stop-corrupt-current/`.
The `proposed diff` (local stand artifact) was not applied;
its behavior on a kernel without IPv6 still needs verification.

## D-S2-03 — closed by C2; original incomplete network/current rollback

A network recovery journal was applied; the lead-agent independent verification is
40 scenarios and 250/250 assertions. During integration, an init failure
on cleanup of a published journal was also reproduced and fixed. [Evidence](blocker-closure/acceptance.md).
The original defect is below; real kernel/LAN remain untested.

Condition: the network is already applied, current publication via ln/mv fails.
On first start, init does not call rollback, deletes the candidate, leaves nft
and two new policies without current/procd. The next start is blocked by the guard
`Table inet nadkop exists without an owned generation`. With previous and
TPROXY off, pointer/nft were restored but two new policies remained.
Separately, a cleanup error after successful nft leaves the new nft with the old current.

Paths: `openwrt/nadkop/files/nadkop.init:51`, `internal/router/network.go:265`.
Evidence: `namespace-fixtures/final2/first-ln-fail`, `ln-fail`, `mv-fail`,
`cleanup-after-nft-fail` under `build/agent-audit/S2/`. Real code was executed
with external command mocks; kernel nft/ip, LAN blackhole or bypass were not measured.
The `transaction/rollback draft` (local stand artifact) is preserved
unapplied; it is not a ready verified diff.

## F-S2-01 — separate file-consistency boundary on crash

On directory-fsync EIO/SIGKILL, config and nodes may be left from different
saves; a fresh load+compile fails. Partially written JSON was not
observed, the current runtime kept its previous hash. The contract does not promise a shared
crash transaction for config/nodes/catalogue, so this is not counted as a fourth
confirmed contract defect of S2. Power loss/flash were not tested.

S2: 48 syscall scenarios, 12 init/network + 1 real ENOSPC in a 64 KiB tmpfs;
6 top-level race tests and 9 subcases PASS, 2 helper SKIPs in the normal
run were invoked separately. 227/227 verifier assertions check **behavior together
with the defects**. [Full report, commands and limitations](agent-audit/S2-crash-consistency.md).

## S1-F1 — closed by C1; original invalid-config write via addsource

The full future config, options and URL are validated before catalogue/config publication.
Invalid requests are rejected without file changes; a valid retry passes.
[C1](blocker-closure/C1-state.md), [acceptance](blocker-closure/acceptance.md).
The original pre-fix reproduction follows.

Write ACL and a current revision are required. Valid HTTPS JSON is accepted with a
4129-byte URL, an 81-character options.title, or a Title with a newline. Addsource answers
success; subsequent ReadConfig/backend.load reject the config. source_import.go
has no full revalidation before SyncSourceServers/atomicWrite; Title is not
forbidden in options, URL length is not checked, unlike ReadConfig.
Paths: `internal/router/source_import.go:37`, `:47`, `:80`;
`internal/core/config.go:122`, `:134`.

Three JSON evidence files `s1-addsource-long-url.json`, `s1-addsource-long-title.json`,
`s1-addsource-control-title.json` are in `build/agent-audit/S1/`; the positive
URL and the malformed-JSON rejection were verified separately. The already applied generation was not
changed. Privilege escalation or mandatory data loss are not proven; the source
can be fixed via an allowed get/save or locally.

## S1-F2 — closed by C1; original read-limit overflow after save

The final config/nodes serialization with newline is checked before writing;
the 4 MiB ±1 byte boundaries and preservation of previous files were verified with the race detector.
[C1](blocker-closure/C1-state.md), [acceptance](blocker-closure/acceptance.md).
The original pre-fix reproduction follows.

Write ACL, a valid <4 MiB request and a current revision are required.
MarshalIndent+LF expand config 4 096 193 → 4 243 741 bytes; another example
expands nodes 3 842 109 → 4 284 523 bytes. Save answers success, but the next
ReadConfig/Load rejects a file larger than 4 194 304 bytes. The full input requests
of 4 096 408 and 3 842 660 bytes fit within the limit. This is not a proven OOM.
Paths: `internal/router/rpc.go:304`, `:309`, `:555`;
`internal/core/config.go:68`, `internal/core/subscription.go:104`.

`JSON config` (local stand artifact),
`JSON nodes` (local stand artifact).
The `S1 general proposal` (local stand artifact) was not
applied: context dry-run only. Both S1 errors are reproduced with
`python3 scripts/test-s1-security.py`, additionally `--race`. Repro tests
pass when the defect is present; 15/46 PASS does not mean clean acceptance.
[Report S1](agent-audit/S1-security.md).

## S4-D01 — closed by C1; original catalogue overflow beyond the read budget

The writer checks the final catalogue size with newline before publication.
The 4 MiB ±1 byte boundaries, preservation of the previous catalogue/IDs and a successful valid retry were verified;
S4 catalogue-byte-budget was rerun by the lead agent.
[C1](blocker-closure/C1-state.md), [acceptance](blocker-closure/acceptance.md).
The original pre-fix reproduction follows.

An allowed source of 4096 nodes, 3 190 785 bytes and 523-byte ASCII tags
successfully passes load/SyncServers. The writer publishes a 7 818 180-byte
`servers.json`, although the reader is limited to 4 MiB. Subsequent ServerSummaries and a real
repeat SyncServers return `cannot read server catalogue`; the file/IDs
remain on disk. Interruption of live traffic was not measured.
Paths: `internal/core/servers.go:62`, `:232`, `:235`, `:243`.

Reproduction: `python3 scripts/test-s4-performance.py --cases catalogue-byte-budget --output build/agent-audit/S4/repro-NEW`.
Expected exit 1; the output directory name must be new. The defect was rerun
three times. `Raw log` (local stand artifact),
the `proposed guard byte budget` (local stand artifact),
[report S4](agent-audit/S4-performance.md). The patch was not applied, does not restore
an already oversized catalogue, and does not resolve the whole long-name policy.

A separate S4 limitation: on 16 rules with the largest geodata pair the runner
stopped the process at 745.77 MiB RSS; the 64/256/1024 steps were skipped. This
is not declared an additional bug. 132 measurements/250 iterations total; host
estimates are not memory/time guarantees for the target router.

## S6-D01 — closed by C4; original busy/dirty bypass

The applied fix and independent checks:
[C4 acceptance](blocker-closure/acceptance.md). The original reproduction follows.

Task blocks only existing controls. Save/status/geodata rebuild the
DOM before task completion; new fields are available. In the browser reproduction
the field accepts `unsaved-during-apply.s6.test`, the mock config holds the previous
`draft.s6.test`, the UI shows "Saved and applied". Dirty was overwritten
after a new edit. Four more paths with the same cause — the selected server,
late geodata, rename dblclick and status controls; 5 FAIL total.
Paths: `overview.js:125`, `:154`, `:168`, `:677`.
`JSON` (local stand artifact),
the `unapplied proposal` (local stand artifact).

## S6-D02 — closed by C4; original acceptance of a ubus error as success

All 13 RPCs now use reject:true; a failed save does not start the service.
Verified with the original rpc.js of both SDKs and backend mocks.
[C4](blocker-closure/C4-luci.md). S6-A01 was also fixed and verified separately.
The original reproduction follows.

HTTP 200 with JSON-RPC `result:[9]` passes through the original LuCI rpc.js without
`reject:true`: `expect` substitutes {}, checked does not see result.error.
In the five FAILs, validate/save/service/nodes show no error; after a failed
save a restart-RPC is sent and "Saved and applied" appears, although writes=0.
This differs from the regular Nadkop error frame, HTTP 503 and malformed JSON:
their handling passed. Probe already has reject:true and passes the negative
control. The behavior was rerun with the rpc.js of both SDKs. No real service restart
was performed; backend/storage in S6 were mocks.
Paths: `overview.js:14`, `:17`, `:583`; SDK `rpc.js:93`, `:105`.
`JSON` (local stand artifact),
the `unapplied proposal` (local stand artifact).

S6-A01, P3/low, not a separate release blocker: a tablist with role=tab does not move
focus on ArrowRight. Tab/Enter work; Home/End/screen reader are unconfirmed.
S6 total: **84 scenarios, 73 PASS / 11 FAIL**, including 5 D01, 5 D02 and 1 A01.
[Report with exact reproducers and logs](agent-audit/S6-luci-resilience.md).

## B2 — no new application defects found; first runner TERM cleanup failed

Two real hourly firings on OpenWrt 25.12.6 passed: a successful refresh of the
local source, then preservation of the working generation and the real HTTP/SOCKS
route when the source was corrupted. The old "full hour untested"
constraint is lifted only for this scenario. A timed HTTPS/provider outage was not tested.

The first TERM cleanup returned exit 1: the stored Zapret hostlist/ipset copies
were owned by nfqws and failed the root preflight. User files were
restored; the service/network were restored from a private copy. Only the
auxiliary runner was fixed: before preflight the copies temporarily get the root owner back,
after which init assigns nfqws normally. The repeat TERM check returned the expected
143 and restored the VM. The failing log was not deleted:
`first cleanup` (local stand artifact),
`recovery` (local stand artifact),
`rerun` (local stand artifact).

The lead agent verified the report, logs, runner/updater and independently read
the current VM state: enabled/running, Xray/monitor/nfqws active, NFQUEUE 200
attached, updater absent as before the test, no test directories/locks.
Production was not fixed. [Report B2](agent-audit/B2-updater.md).
