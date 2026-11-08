# X2 — Verification and implementation

2026-11-03–04, Europe/Moscow. Basis: [final.md](final.md), [terra-critique.md](terra-critique.md). Previous X1 fixes were re-verified. X2-05/07/09 remain excluded; this is not a blanket implementation of the original X2 proposals.

## What changed

| ID | Decision and implementation |
| --- | --- |
| **X2-01** | After profiling, reuse of expansions was added only within a single compile operation and only for repeated selectors. Remaining-use counters free the entry after the last reference; unique selectors are not cached. Slices are not mutated by the consumer. Validate stores only the selector-vs-Zapret compatibility result, without expanded strings. There is no shared GeoDatabase cache, no streaming JSON, and no Artifacts API change. |
| **X2-02** | Investigated; **indexes not implemented**. In the profile of a limited 256/1024 entries, exact lookup produced 20 ms of total samples out of a 3030 ms overall profile; canonicalization/JSON/allocations matter more. This is not a profile of all admissible collisions and not grounds to change the identity rules. Reconcile and its exact/fallback/claims semantics are preserved. |
| **X2-03** | Cancellation implemented before/after read and parsing, before publish, and while waiting on the catalogue lock. An expired fetch-context error is checked after all loaders. There is no late publication of an observably cancelled operation. All 16 sources still start immediately, order is preserved; the overall 12 s fetch and 24 s RPC are not increased. Sync uses the parent context: a completed fetch is not constrained by its fetch timer that expired during the subsequent reconcile. No pool added: four 5 s waves do not fit into 12 s. Local CPU parsing does not promise instant interruption. |
| **X2-04** | The probe queue is deterministic by round-robin over group participant positions; URL+ID is deduplicated. At most four probes and one API apply run concurrently. Ready groups are applied before other groups' probes finish. Priority may apply the first successful result after receiving all senior ones; the planned junior probes still run. URL/ping wait for all their results; tie-break and all-failed→block are preserved. The function waits for all probes/apply, then the previous 15 s pause begins. A generation change/cancellation cuts off old results; a shared lease on ui.lock protects the generation-check/API call from concurrent service publication. A successful choice is not cached, so the next cycle re-applies it after an independent Xray respawn. |
| **X2-06** | Action process and outcome are shown separately. Monitor and updater write a small private RAM status with the process generation, PID, and Linux starttime, time, and success/error counters; URL, credentials, HWID, and stderr are not included there. LuCI distinguishes "No data", "Running", "Completed", "Error" and expands the explanation. A stale PID/generation or a missing/corrupt status does not yield success. One group's error remains an error of the overall result with the count of failed actions. The updater `updatedAt`/`ok` fields are preserved; the erroneous unconditional wording "previous generation retained" was replaced with a request to check the service state. |
| **X2-08** | **Detection + manual recovery** was chosen, without replay and without full old/new copies. Before save, `.save-state.json` is written with the SHA256 of the two pairs. A full old/new pair is acceptable; a mixed/unknown one blocks further reads and configuration changes. On lock of a consistent pair, the marker is removed. A directory-fsync error after a visible config rename no longer rolls back only nodes to the old version. The marker is included in keep.d; old full backups without it are still supported. [Guarantees and recovery](../../docs/interrupted-save.md). |

Code: [compile](../../internal/core/compile.go), [matchers](../../internal/core/matchers.go), [validation](../../internal/core/validation.go), [loader](../../internal/router/source_load.go), [monitor queue](../../internal/core/monitor_cycle.go), [monitor](../../internal/core/monitor.go), [RAM status](../../internal/core/action_status.go), [diagnostics](../../internal/router/action_diagnostics.go), [save detector](../../internal/core/saved_pair.go).

## Measurements and limitations

Fixture: 8 selectors of 2048 domains and CIDRs each, 8 rules; one repeated selector or eight different ones. Each case was run as a separate process three times with `GOMAXPROCS=2`; TotalAlloc, live heap after GC with the retained result, peak RSS, and Xray size were measured. Separate phases: open, expand, validate, compile. `All measurements` (local stand artifact), `CPU profile` (local stand artifact), [benchmark](../../internal/core/x2_profile_test.go).

| Repeated selector | TotalAlloc before | TotalAlloc after | Live heap before / after | Peak RSS before / after |
| --- | ---: | ---: | ---: | ---: |
| validate | 4 763 515 B | 2 095 216 B | 253 859 / 253 901 B | 15 775 / 15 356 KiB |
| compile | 14 792 867 B | 12 123 795 B | 2 480 227 / 2 476 536 B | 19 607 / 17 781 KiB |

For different selectors TotalAlloc is nearly identical: validate 4 763 547 / 4 763 509 B, compile 14 791 344 / 14 791 517 B. The identical **1 116 853 B** output has identical SHA256 before/after in both cases: `verification` (local stand artifact). Host time under parallel tasks was very noisy; no speedup on the router is claimed. A post-operation measurement does not prove peak live memory for every admissible configuration; process RSS of this limited fixture was accounted separately for that. Large/heavily overlapping real databases require their own measurement.

The synthetic monitor fixture contains eight 1 ms probes and eight 1 ms applies: `before` (local stand artifact), `after` (local stand artifact). It checks phase overlap, not real downtime. A shorter cycle with the same 15 s pause may increase cycle frequency; unchanged load is not promised. One slow API apply still delays the following ready groups; mass Xray CLI parallelism was not added.

The save marker is under 400 B and contains no full secrets. The cost is one extra write and two fsyncs before the pair plus removal/fsync after. This is not a general crash transaction, automatic recovery, or a physical power-loss test. A backup concurrent with save does not become atomic; restoring an old full copy while a newer marker remains requires actions from the recovery document.

## Checks

- `go test -race ./...` with three previous S3 policy audits in explicit `-skip` — PASS. `Log` (local stand artifact).
- Unfiltered `go test ./...` — only three known failing S3-R01/F03/F04 checks. `Log` (local stand artifact). These tests were not removed. The backup fixture was extended with the new optional marker, and mixed-content detection after tar restore was added.
- `Targeted X2 race tests` (local stand artifact): four slots, fair first job set, shared URL+ID, slow apply, cancellation, early priority, full cycle, lease/lock cancellation, identity/privacy status, 16 loaders, source order preservation, and publish refusal after cancel.
- `Phased save failures` (local stand artifact) — **12/12 PASS**. SIGKILL and fsync/rename EIO were injected via strace into own files; the runtime sentinel was preserved. Runner: `python3 scripts/test-x2-save-faults.py`.
- X1 — **26/26 PASS**: `summary` (local stand artifact). S6 on the final UI: `summary` (local stand artifact). Separate diagnostics in three themes at width 1360/390 — **6/6 PASS**: `summary` (local stand artifact).
- `go vet ./...`, native build, and `validate -config examples/config.json` — PASS. CLI validate does not fetch subscriptions.

The first full run revealed two fixed integration issues: the backup test assumed a single keep.d line, and the fetch context was mistakenly used for the next reconcile phase. An intermediate race run with a parallel SDK build also hit an overall timeout of 180 s that was too short; the final run uses `-timeout=10m` without changing production deadlines. These intermediate results are not presented as successful.

## Packages and shared folder

Both x86_64 formats were rebuilt from sources: `APK bundle` (local stand artifact), `IPK bundle` (local stand artifact), `SHA256` (local stand artifact). Three packages and build metadata in each. Actual marker/status strings in the extracted binaries, the new LuCI renderer, keep.d, and help via the musl-loader SDK were checked: `result` (local stand artifact).

`--prepared` initially left old `.built` files and copied previous packages; after that, `clean` of only Nadkop/LuCI and a genuine rebuild were performed. Logs: `APK` (local stand artifact), `IPK` (local stand artifact). Package versions belong to the shared branch: backend 0.3.0-r6, LuCI 0.3.0-r10. A matching number with a previous package does not mean matching bytes; use the attached checksums.

A parallel task was adding the Zapret tuner; its current changes are preserved and included in these bundles. This is not a separate acceptance of the new tuner feature. SDKs were used sequentially and agreed with the executor; others' ready bundles were not overwritten. The dedicated VM and its configuration were not changed by this work. There was no full device/VM run, physical touch, screen reader, LAN/procd, or power-loss testing of X2.

`Source copy` (local stand artifact), `final shared-folder manifest` (local stand artifact). The recorded hashes allow distinguishing this check from subsequent changes by the parallel task.
