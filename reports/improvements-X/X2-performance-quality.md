# X2 — Nadkop speed, resources, reliability, and maintenance

Interim analytical summary of stream X, 2026-11-02 UTC. The executor worked independently, without delegation. **Implementation of proposals was not permitted and was not performed. No new confirmed blockers were established.**

The scope is the existing OpenWrt selective-routing manager: core, router, lifecycle, and their maintenance. The product purpose, positioning, supported features, formats, and stated numeric limits are not revisited. These are proposals for after the current blockers are closed, not an additional list of release conditions.

## Evidence and moving baseline

Initial read: 2026-11-02 19:59:12 UTC. The first snapshot of 82 files was completed at 19:59:41 UTC; after adding the read runners and historical JSON files it contains 95 files. The updated snapshot of 98 files was taken at **20:05:46 UTC**. After the final re-read of moving files, an additional `snapshot-end/` was saved. Snapshots are sequential per file, not an atomic filesystem snapshot: each entry has its own `read_utc`.

- Initial snapshot and SHA256: `snapshot-manifest.json` (local stand artifact), bytes in `build/improvements-X/X2/snapshot/`.
- Basis for the line references of the current analysis: `snapshot-final-manifest.json` (local stand artifact), bytes in `build/improvements-X/X2/snapshot-final/`.
- Final combined manifest: `snapshot-current-manifest.json` (local stand artifact); `snapshot_path` points to the exact saved bytes, `snapshot-end/` takes precedence over `snapshot-final/`.
- Final recheck and stale paths: `snapshot-recheck.json` (local stand artifact); its result is given at the end of the report.
- Snapshots and measurements belong to X2 only. The product, project tests/runners, configurations, SDK, VM, dependencies, other reports, and registries were not modified. Old reports were read as history; the absence of Git was accounted for via SHA256.

`reports/blocker-closure/team.md`, README, `docs/product-contract.md`, `docs/interface.md`, `reports/blockers.md`, S1–S5, B1, and related sources were read. S1 constrains permissible optimizations by preserving origin/TLS/ACL/secrecy; S2 — by failure semantics and durability boundaries; S3 — by restoration of IDs, database pairs, and private files; S5 — by transport compatibility and dependence on the Xray version. The historical 50 HTTP/44 UDP in S5 refer to Xray 26.3.27 and the former code, not to the current fixes. B1 closes a specific ShellCheck profile, not arbitrary subsequent revisions of init.

During the work, `cmd/nadkop/main.go`, `internal/core/config.go`, `servers.go`, `geodata.go`, `internal/router/rpc.go`, `source_import.go`, `network.go`, init, the builder, and the shared registries actually changed. Intermediate diffs are saved in `build/improvements-X/X2/diffs/`; they illustrate the transition from the first snapshot to the second and are not a complete journal of all revisions. New C1/C3 tests are included in the updated snapshot. Old line numbers from audits were not used as addresses of the new code.

## C1–C4 work duplicates: not a separate improvement backlog

Per `reports/blocker-closure/team.md:15`, all 11 blockers are already assigned:

| Already-being-fixed IDs | Owner | X2 observation from the snapshot, without declaring closure |
| --- | --- | --- |
| D-S2-01, S3-F01, S1-F1, S1-F2, S4-D01 | C1 | Shared configuration locks appeared in `config.go:353`, `main.go:85`, `rpc.go:138`; in `servers.go:178` saved refs are checked before reconcile, in `:265` — catalog bytes; `rpc.go:310` uses `encodeSavedJSON` before writing. The old S4-D01 reproduction does not prove a defect in this revision. |
| D-S2-02, D-S2-03 | C2 | A journal was introduced in `network.go:353`; recover/rollback/commit are visible, and in init `:52` and `:58` the failure path changed. The old conclusion "the first retry blocks" is outdated as a description of the current code; C2 verification still belongs to the lead team. |
| S3-F02, E05 | C3 | `geodata.go:240` opens root/generation/dat with symlink-following restrictions; the builder changed. Neither the old escaping fixtures nor the former feed error were re-checked here. |
| S6-D01, S6-D02 | C4 | Outside the X2 scope; not converted into performance proposals or new blockers. |

The presence of changes does not prove a fix. X2 does not run their acceptance and does not change the statuses of the main registry. The proposals below preserve the new locks, refs checks, and byte budgets.

## Limited new measurements

Isolated run: 2026-11-02 **20:02:57–20:03:30 UTC**, including stdlib build. Only the unmodified copy of `internal/core/monitor.go` and a custom test with the `maxSubscription` stub constant were compiled; the remaining product files, project tests, and engines were not built/run. SHA256 match between the copy and the snapshot: `89fb8b333fcfbf42c89efb6964bb8a0407a8290f3090a375720439df96bc07ac`.

The real `MonitorCycle` function, synthetic probe/apply callbacks, no sockets and no network requests. `GOMAXPROCS=1`, `GOMEMLIMIT=192MiB` — a soft GC limit, not a hard RSS limit; build with `-p 1`, external timeout 60 s, test timeout 3 s. The test portion finished in **0.045 s**, exit 0. The private Go cache was deleted after the measurement. Log: `result.log` (local stand artifact), source fixture: `monitor_test.go` (local stand artifact), command/UTC/hashes: `monitor-measurement.json` (local stand artifact).

- Two cycles, 16 groups × 16 members, 64 unique IDs: a shared URL produced **128 probe callbacks**, per-group URLs — **512 callbacks**. In both cases **32 apply callbacks** were executed despite the unchanged selection. This confirms scheduling and callback counts, not the number of real HTTP/CLI/Xray operations.
- Two independent groups: after the fast probe finished, the apply callback was not invoked for the full **40 ms** while the slow probe was held by its own channel. After release, both groups were applied. This is an intentional fixture delay, not a measured user delay.
- The already-measured S4 745.77 MiB, 27,863,082 bytes written, and refresh times were **not reproduced**. Below they always carry the "historical data" label.

## Proposals

Priorities: **P1** — investigate/design first after the current fixes, **P2** — next bounded optimization, **P3** — only on confirmed need. This is improvement priority, not severity of a new defect. Cost is preliminary single-developer engineering days with targeted verification; implementation and hardware acceptance are not included.

### X2-01 — reduce repeated geodata materialization

**Problem.** A small configuration can produce a large transient volume when large selectors are repeated. Limiting the rule count does not describe compile memory. Router safety cannot be promised only because config is under 4 MiB.

**Evidence.** Snapshot `internal/core/compile.go:166`, `internal/core/matchers.go:47`, `:58`, `internal/core/geodata.go:318`: each rule invocation expands the selector and re-builds/deduplicates strings. `compile.go:231` then serializes the whole Xray JSON in memory. `validation.go:43` also expands the same selector again. History: `reports/agent-audit/S4-performance.md:118` — four large repeated rules produced 65,005,369 bytes of JSON and 405.68 MiB whole-process RSS; on 16 rules the watchdog stopped the process at 745.77 MiB. These are **historical data**, not the current OOM and not a standalone blocker. The stop was external; there is no completed compile metric for 16 rules.

**In-product change.** First, build an expansion cache shared by one compile/validate operation, keyed by selector and the selected database generation, with immutable slices and safe merging in the specific rule. Then evaluate writing Xray JSON directly into a staging writer instead of mandatorily holding the full `[]byte` and its copies. Within the same study, compile an accounting of expanded matchers and output bytes for diagnostics. Preserve rule order, domain/IP/set OR, disabled fields, and Zapret prohibitions; do not merge rules automatically. Do not introduce an arbitrary new limit on supported sets/rules. The cache itself does not reduce the mandatory final JSON volume and Xray RAM.

**Effect.** **Hypothesis:** less repeated protobuf parsing, fewer allocations, and a lower generator peak. The size of the gain is unknown; caching may hold more live data in some configurations. The historical S4 only indicates where to look.

**Cost.** Cache and targeted verification 2–4 days; streaming serialization separately 3–6 days, only after measuring the first step. **Risk/regressions:** first-match change, shared-slice mutation via append, incorrect database invalidation, large live cache, CLI vs RPC generation mismatch.

**Verification criterion.** On a small synthetic database with repeated selectors — same matchers/order/errors, each unique expansion no more than once per operation; a generation change does not return stale data. Compare TotalAlloc and the process peak on one bounded completed stage before/after, without repeating the stopped S4 stage. Separately confirm that runtime semantics and output byte count are preserved. **Priority P1.**

### X2-02 — index catalog matching without changing identity rules

**Problem.** An unchanged refresh does not write the catalog but performs a nested search of already-known servers and full connection-graph processing.

**Evidence.** The re-read snapshot `snapshot-end/internal/core/servers.go:349` iterates `groups × c.Servers` for exact fingerprint; `:360` repeats the full search for endpoint/tag fallback. With identical order and N unique entries, the exact pass makes N(N+1)/2 entry visits; for 4096 that is **8,390,656** — a loop calculation, not CPU profiling. `:319` and `:323` build full/endpoint identities. **Historical data:** S4 `:61` — combined-maximum sync 660.939 ms, 220.613 MiB TotalAlloc; this must not be presented as metrics of the new C1 revision.

**In-product change.** Build, for the duration of reconcile, `source+fingerprint` and `source+protocol+endpoint` indexes, then a tag/alias index for the unmatched. Index values are candidate lists; preserve `used`, multiple claims, and the "do not guess on ambiguity" rule. Do not change the hash format, IDs, or on-disk schema. Only after profiling consider reusing the normalized outbound between full/endpoint fingerprints.

**Effect.** **Hypothesis:** reducing the search portion from quadratic to near-linear; the JSON/canonicalization contribution may remain dominant. The percentage speedup is unknown. Zero writes of the unchanged catalog were already in S4; this is not a new effect of the proposal.

**Cost.** 2–4 days. **Risk/regressions:** silent substitution of an old ID on endpoint/tag collision, different handling of aliases and headings, subscription-order violation, bypass of the new C1 checks. Indexes increase live memory.

**Verification criterion.** On bounded 16/256-node sets with reorder, rename, rotation, chains, duplicates, and ambiguity, obtain the same IDs/availability/order and errors; a stationary catalog gets no temp/rename. Compare the visit counter and TotalAlloc, not only host wall time. Run the targeted C1 refs-completeness and byte-budget scenarios after implementation. **Priority P2.**

### X2-03 — bound concurrent source loading and parsing

**Problem.** RPC load starts up to 16 loaders concurrently; limiting each response does not bound the joint peak of raw JSON, parsed objects, and temporary buffers.

**Evidence.** Snapshot `internal/router/rpc.go:408` sets a shared fetch timeout of 12 s, `:413` starts a goroutine per source and holds `results`; `internal/core/subscription.go:104` limits an individual input. The sum 16 × 4 MiB = 64 MiB is the arithmetic bound of accepted raw payloads, **not RSS and not the aggregate quota of the current loader**. S4 `:83` measured HTTPS with two loaders, padding, and 78.45 MiB RSS; that experiment **does not measure** the current full concurrency and dense JSON.

**In-product change.** Move loading into a small pool bounding the number of concurrently read/parsed sources. On the first fatal failure, cancel the rest; collect results in the original order, publish the catalog only after all succeed. Keep the 16 supported sources, 4 MiB per source, HWID/headers, and the existing IDs. Choose the worker count by measurement. First resolve the conflict with the shared deadline: with four workers and four five-second waves, a valid set may not fit into 12 s. One cannot simply add a semaphore and call it an improvement.

**Effect.** **Hypothesis:** a lower transient-memory peak under large concurrent responses; the full size of retained parsed nodes remains. Longer refresh duration and a lower success probability for slow sources are possible. There is no new RSS measurement here.

**Cost.** 2–4 days with reconciliation of the existing RPC/Apply deadlines. **Risk/regressions:** starvation of the last subscription, false timeouts, order violation, partial publication after cancellation. Preserve TLS, same-origin redirect, and secret-free errors per S1.

**Verification criterion.** Via an injectable loader without network, verify the active-loader ceiling, cancellation, and the original order of the 16 results; an error/timeout of any source preserves the previous catalog and runtime. Compare the memory peak on two small dense fixtures; separately verify slow sources with a deadline model. Until such data, the recommendation is conditional. **Priority P2.**

### X2-04 — apply a ready health group independently of other groups' probes

**Problem.** The shared cycle barrier delays all groups because of the slowest job; `priority` also waits for junior candidates unneeded for an already-determined selection.

**Evidence.** `internal/core/monitor.go:104` deduplicates by URL+ID, `:111` bounds concurrency to four, `:128` waits for the whole set and only `:130` starts apply. **New measurement:** the fixture above confirmed no apply of the fast group for the full 40 ms of the other group's controlled delay. On 16×16 with distinct URLs, 256 jobs per cycle were confirmed. 64 unique IDs do not mean 64 requests. As a reference, the calculation 256/4 × 5 s = 320 s of full timeout waves only; this is an **analytical scenario**, not current latency, a guaranteed upper bound, or a new network measurement.

**In-product change.** Keep the shared dedup and the four-Probe ceiling, but track readiness per group. Apply a latency group after all its candidates complete; a priority group — once the first successful result is obtained and all candidates senior to it have completed. Long jobs of other groups must not block this apply. Do not increase concurrency or the background probe frequency.

**Effect.** **Hypothesis:** faster first working selection and failover of an independent group, especially after startup with an initial block. No reduction of real downtime was measured. The "15 s after cycle" period, HTTP-based selection, and blocking on full failure are preserved.

**Cost.** 3–5 days. **Risk/regressions:** accidental priority/tie-break violation, data race, cancellation of a shared probe, a temporarily stale result of a new generation. Careful binding of events to the manifest generation is required.

**Verification criterion.** Deterministic channel fixtures: a ready group is applied before the release of another group's slow probe; priority does not select a junior while a senior is unfinished; ping/URL waits for its own results; identical URL+ID pairs are not duplicated, active Probes ≤ 4. All unavailable → block; cancellation/generation change does not apply a stale tag. **Priority P1.**

### X2-05 — do not repeat a successfully confirmed selection unnecessarily

**Problem.** Monitor invokes the Xray CLI on every group of every cycle, even when the result is the same. That is a periodic fork/exec and API request on a stable network.

**Evidence.** `internal/core/monitor.go:135` invokes the callback unconditionally; `:164` turns it into `/usr/bin/xray api bo` with a 4 s timeout. **New measurement:** two identical cycles for 16 groups produced 32 callbacks. They were stubs: the count/cost of real processes was not measured here. With vanishingly small cycle time, the formula 16 × 3600/15 = 3840 calls/hour is a limit-frequency calculation, not an actual device counter.

**In-product change.** Store only successfully confirmed `(generation, group, selectedTag)` triples and skip repeated apply; do not cache errors. Mandatorily invalidate the cache after an Xray/monitor restart, a manifest change, and on API loss. Provide for a rare forced reconciliation: Xray may restart via procd while monitor is alive and lose the override. Without this condition the optimization is dangerous.

**Effect.** **Hypothesis:** fewer spawn/API costs of a stable monitor. Probe and its freshness are preserved. CPU/RAM savings are unknown, so with complex reliable invalidation the implementation may not pay off.

**Cost.** 2–4 days. **Risk/regressions:** cache reports success after override loss, a group gets stuck on block after respawn, a failed apply stops being retried.

**Verification criterion.** The first application happens once, the second identical cycle invokes no callback; a selection change invokes exactly one; a failed apply is retried; simulated respawn/API reset/generation change forces the selection back. The real Xray API requires a separate targeted integration check after implementation. **Priority P3, after benefit verification.**

### X2-06 — surface background-operation state in the existing diagnostics

**Problem.** A live monitor process by itself does not confirm successful application of the selection; automatic-update failures are also hard to distinguish from a healthy schedule.

**Evidence.** `internal/core/monitor.go:161` discards the `MonitorCycle` error; `internal/router/runtime_diagnostics.go:57` writes "Monitor running" based on running. `updater.go:46` already produces `update-status.json`, its write error in `:52` is ignored; `rpc.go:548`–`:557` returns runtimeChecks but does not read the updater file. This is a **current-code analysis**: the live-monitor-with-failing-Xray-API scenario and its traffic impact were not executed.

**In-product change.** In `/var/run/nadkop`, keep the compact state of the last completed monitor cycle/apply, generation, last-success time, and generalized failure cause; read it and the existing updater status in the current diagnostics. Do not write every probe to flash. Log errors rate-limited, without URL, HWID, payload, or credentials; do not expose arbitrary CLI output via status. Distinguish process running, cycle still in progress, last apply successful, state stale.

**Effect.** **Hypothesis:** less manual log collection and faster explanation of a group block or failed refresh. There is no new diagnostics-time measurement. The failover behavior itself is not fixed by this proposal.

**Cost.** 1–3 days. **Risk/regressions:** false "stale" on a long X2-04 cycle, generation race, unnoticed writes to persistent storage, secret disclosure via read ACL. The actual long cycle must be taken into account; 15 s must not be treated as the check period of all servers.

**Verification criterion.** Injecting an API failure with a live process yields degraded in diagnostics; success restores the status; an old generation is not displayed as current; a missing/corrupt status does not yield a false success. Canary secrets are absent from the response, persistent files are unchanged. **Priority P2.**

### X2-07 — skip publishing an identical geodata pair

**Problem.** A repeated manual update of the same pair creates a new generation and rewrites both databases even though the data is unchanged.

**Evidence.** The current `internal/core/geodata.go:438` creates staging without comparison, `:448` writes both payloads, and `:458` replaces current. `geodata_update.go:66` parses the downloaded pair, `:88` invokes installation, which in `geodata.go:422` validates it again. **Historical data S4-O03**, `S4-performance.md:127`: the same real pair rewrote 27,863,082 payload bytes. Those are logical-write bytes on the stand, not flash erase cycles. The current safe C3 read implementation has changed; the result was not repeated.

**In-product change.** After validating the candidate and the used selectors, under the update lock compare both files against the safely opened current pair. On exact equality, do not create a generation and do not change current. Equality can be checked streaming, without a second full RAM copy; do not trust mtime alone. Do not bring back the unsafe EvalSymlinks from the former S3-F02. Reusing the parsed candidate between validation/install is a separate small step only after the no-op check, and with the public InstallGeoFiles validation preserved.

**Effect.** **Hypothesis:** logical content writes and pointer churn of an identical pair are eliminated. The download itself and the mandatory active-selector check remain; the benefit depends on the frequency of manual repeats. Flash wear is not estimated.

**Cost.** 1–2 days. **Risk/regressions:** comparing the wrong generation, stale validation, a damaged current pair wrongly accepted as suitable, unsafe symlink, changed meaning of updatedAt. A missing/unreadable current must lead to a safe normal install or a clear error, not to a false no-op.

**Verification criterion.** A small valid synthetic pair twice: the second call preserves the current inode/hash/target and creates no payload files; changing one byte of valid content publishes a full new pair. A pair that drops a used set, and an escaping symlink, are rejected as before; the previous pair stays readable. **Priority P2.**

### X2-08 — recovery of a consistent config/nodes pair after an interrupted save

**Problem.** Locks prevent participant races but do not make several renames a single crash transaction; a directory-fsync error may already mean visibility of the new file. Distinguishable "not published" and "published, durability unconfirmed" states are needed.

**Evidence.** Even after the C1 precheck, in the snapshot `internal/router/rpc.go:321` nodes is published first, `:324` config; on a config error only nodes is rolled back. `atomicWrite:592` performs rename before parent fsync `:600`. **Historical data F-S2-01**, `S2-crash-consistency.md:195`: SIGKILL between publications and directory-fsync EIO led to semantic inconsistency of valid JSON. X2 did not repeat fault injection on the new C1 revision. S2 does not directly declare this a separate contract defect of the overall transaction.

**In-product change.** Design a small recoverable save journal with old/new bytes and a publication phase under the shared configuration lock; run recovery before the next CLI/RPC read. First prepare and synchronize everything required, then bring both publications to a consistent state. Preserve the existing config/nodes/servers paths, backup/sysupgrade compatibility, and temp-copy privacy; the new internal journal must not become a mandatory new user restore artifact. Keep catalog cleanup separate from settings commit, preserving bound refs. The exact protocol requires a separate design; there is no ready algorithm/patch here.

**Effect.** **Hypothesis:** after a restart the operation recovers to a consistent old or new pair; less manual repair over SSH. No protection against whole-media damage or any power loss is promised. Potentially more persistent writes on a changed save.

**Cost.** 5–8 days together with the recovery model and targeted fault tests. **Risk/regressions:** erroneous replay, disclosure of old credentials in the journal, write-amplification growth, package hooks/backup incompatibility, wrong lock order. Do not build automatically on top of unfinished C1 changes.

**Verification criterion.** In a custom small fixture, interruption at each phase and EIO before/after rename yield a consistent old/new pair on the next read; refs remain, a partial error does not start a new runtime. Verify absence of secret data in errors, private modes, and reading of an old backup without the journal. Describe directory-fsync uncertainty separately; hardware durability remains unknown. **Priority P1 for design, implementation after C1.**

### X2-09 — separate historical-defect reproduction from regression acceptance

**Problem.** Historical tests may pass while the defect is present, and frozen runners deliberately refuse to run changed code. Such PASS/FAIL is inconvenient as a standing check of the fixed product.

**Evidence.** Snapshot `scripts/test-s1-security.py:16`, `:52`–`:55` is pinned to the former frozen manifest. The second snapshot `internal/router/s1_security_test.go:425`, `:490`, `:534` still contained historical Repro assertions. **On the final re-read they were already replaced by C1 with file-preserving refusal checks**; that observation about the old assertions is outdated, and fixing it is not part of the X2 backlog. The surviving Repro prefix in names and runners with a frozen manifest still require clear classification. The change was confirmed by reading the diff, not by running new tests. `scripts/test-s2-verify.py:2` directly says "success means reproductions agree, not acceptance", `:103` further verifies preservation of the defective behavior. `build/agent-audit/B1/profile.py:24` stores permissions by exact numbers/lines, B1 `:44` requires their review after changes. C1/C3 already added separate closure tests — a step in the right direction; duplicating them again is not proposed.

**In-product change.** After the main acceptance, collect the team's already-updated short regression tests into a reproducible local gate: separate categories historical reproduction, current regression, environment-required. On each run, record the actual source SHA256 values, the Go/Xray version where it participates, scope, and a unique output directory. Do not rewrite the old audit evidence. For B1, move the verified profile from build artifacts into the maintained test layer, keeping the pinpoint exceptions and negative cases; a line shift requires review of the exception. Do not add SDK/VM/hourly runs to the short gate and do not replace external acceptance with it.

**Effect.** **Hypothesis:** fewer false readiness conclusions and less re-analysis cost after baseline changes. The future gate duration is unknown; old runner times are not the target budget.

**Cost.** 1–3 days after C1–C4 completion. **Risk/regressions:** accidentally accepting a reproduction-PASS as a regression-PASS, loosening ShellCheck with global exceptions, wiping old logs, hiding missing external verification.

**Verification criterion.** A short run on the current snapshot accepts correct behavior; an intentionally restored old defect in an isolated copy breaks the corresponding regression gate. Historical reproduction keeps a standalone status and does not determine the current product's readiness. A change of the B1-checked line breaks the gate until a deliberate review. **Priority P2.**

## Subtotal

First substantive selection: X2-01 (geodata sets), X2-04 (independent group readiness), X2-08 (recovery-save design). They address generation cost, working-route wait, and settings preservation. The X2-02 catalog first needs a small profile of the new C1 revision; the X2-03 loader mandatorily requires a deadline trade-off check. X2-06/07/09 are bounded improvements to diagnostics, database rewrites, and maintenance. X2-05 is low priority until benefit and reliable invalidation are demonstrated.

**9 proposals** in total, of which 3 P1, 5 P2, 1 P3. These estimates do not add up to a promised timeline: code areas overlap, and review plus hardware verification are not included. The 11 blockers being fixed are listed separately and are not part of this count. **No new blockers were reported to the coordinator.** The global monitor barrier and repeated callbacks were confirmed locally, but the contract sets no smaller failover deadline and promises no absence of repeated API calls.

Historical S4-O02 (32 HWID fchmods over two refreshes) is left as a low-presumed-value observation, without a separate proposal: `hwid.go:105` concurrently restores 0600; savings of physical flash writes are unproven. S3-F03/F04 on restored mode/UID and S3-R01 on missing HWID are likewise not raised to new unconditional blockers. Permission repair must not disappear due to micro-optimization.

## Limitations and unknowns

No network requests, browser work, VM/SDK/package builds, overall project build/test, load/hour-long runs, real Xray/nfqws, kernel nft/ip, or fault injection into the product were performed. The local measurement covers only the monitor scheduler with callbacks. The report is an analysis of code and existing artifacts, not a release confirmation.

Target CPU/RAM/flash and the joint-consumption budget of OS + generator + Xray + nfqws are unknown; there are no user statistics on sources and selectors, current cold/steady p95 values, the peak of normal loading of all 16 sources, index/cache benefits, or real Xray CLI costs. The S4 Ryzen x86_64 host does not replace OpenWrt ARM/MIPS or physical flash. TotalAlloc must not be equated with live heap/RSS. Preservation of an atomic pointer does not prove durability after power loss.

Improvements must preserve first-match and OR, absence of direct fallback, group blocking on full failure, stable refs and no ID guessing, subscription order, disabled conditions, the local service scope, ACL, TLS/same-origin, and Zapret prohibitions. New product features/formats/purpose are not proposed. README comments about the former absence of blockers and the freeze are considered in the context of the new `team.md`, not as a current record of a verified baseline.

## Final snapshot check

The final recheck was performed at **2026-11-02 20:45:59 UTC**: 98 files, **97 matched**, one changed — `internal/router/network.go`. Its references in the duplicates table belong to the saved revision, are **stale as addresses of the current workspace**, and do not serve as a conclusion about C2. Additional copy of the last-read revision and SHA256: `snapshot-terminal-manifest.json` (local stand artifact), diff — `build/improvements-X/X2/network-terminal.diff`. None of the nine proposals relies on execution verification of this network code.

Consolidated analytical snapshot: `build/improvements-X/X2/snapshot-current-manifest.json`, SHA256 **`0bef361fcfcc6eb9476502c4cca81016f782b5a2b5ffe5f5673bc579acdb65f7`**. It preserves the exact evidence sources, including the already-flagged stale C2 revision; the terminal snapshot supplements the history rather than rewriting the analysis basis. The last observed `servers.go` change concerns remote-source availability before reconcile; the X2-02 nested search is preserved. Changed S1/S4/closure tests were re-read as diffs, not run; S1 assertions have already become regression ones, so their conversion is excluded from X2-09.

SHA256 binds the conclusions to the bytes read but does not guarantee immutability after the stated UTC. The lead team continues its work. The report does not substitute for its acceptance and does not declare blockers closed.
