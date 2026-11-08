# X — Summary of Analysis After the Terra Critique

Prepared by coordinator gpt-6.1-sol / codex_hub / high. 2026-11-03 Europe/Moscow, 2026-11-02 UTC. Work X is complete: two analytical reports → subtotal → separate review by gpt-6.1-terra / codex_hub / max → this summary. Production code, positioning, purpose, and product composition did not change in X. The recommendations below are not implemented.

Of the 14 proposals, **one is kept, ten are refined, three are excluded from the current plan**. The coordinator accepts Terra's substantive objections. The main result is a short list of clarity/accessibility changes and bounded investigations; pre-approved caches, streaming serialization, and the recovery journal are excluded from it as ready-made solutions. **There are no new confirmed blockers.** Package readiness and closure of the 11 blockers are determined by the lead team.

## Documents and Contributors

- [X1 — interfaces, UX, and accessibility](X1-interfaces.md), gpt-6.1-sol, high.
- [X2 — speed, resources, reliability, and maintenance](X2-performance-quality.md), gpt-6.1-sol, high.
- [Subtotal before review](subtotal.md), coordinator Sol high.
- [Substantive critique of each of the 14 IDs](terra-critique.md), gpt-6.1-terra, max.
- [Addendum on C1–C4 integration](integration-addendum.md).
- [Assignments, links, models, and effort](assignments.json).

Contributor threads, links untransformed:

- X1: (internal orchestrator)
- X2: (internal orchestrator)
- Terra: (internal orchestrator)

## Baseline the Summary Refers To

The lead team reported completion of C1–C4 and ongoing integration of regressions. Coordinator X read the C1, C2, C3, C4, and acceptance reports and took the user's addendum into account. At first reading, acceptance still contained the pending C1 acceptance. **Before the summary was delivered, the team updated acceptance and README:** all 11 blockers accepted; per its results, the final contractual race run is 159 top-level PASS, 0 FAIL, 11 SKIP with the explicit exclusion of the three prior policy audits. The coordinator read the updated acceptance and the result JSON; it did not run the overall gate itself. The unfiltered run remains non-green, and packages/device on the new code are not verified.

Current integrated manifest: `integrated-source.json` (local stand artifact), `capturedAt=2026-11-02T21:01:59.237164+00:00`, SHA256 **`96376c2debff9cdd48c68f279e6d6ed2ccfb2b57f6ec5b99f17a55dbd1921b48`**. Coordinator verification **2026-11-02T21:04:19.694197+00:00**: **408/408 files matched**, `result and document hashes` (local stand artifact). This is a verification of the bytes read, not a gate run and not an atomic filesystem snapshot. The lead team may continue making changes.

At the control verification at **21:06:47 UTC** relative to this manifest, only `README.md` and `docs/product-contract.md` had changed: the read diff documents the completed acceptance, C1 guards, and the explicit absence of a shared crash transaction. These are documentation changes by the lead team; the code sources remained unchanged. The updated acceptance was read separately after this verification; document hashes at delivery are recorded in `build/improvements-X/coordinator/delivery-verification.json`. The old X1/X2/subtotal review inputs are not rewritten.

Fixes that are **already implemented and not recommended again**:

- C1: state lock before reading config, checks of bindings and serialized budgets, prevention of reference loss/unreadable directory.
- C2: cleanup stop and publication rollback; parent handling of commit cleanup failure after current with continued process registration. Acceptance records 40 scenarios / 250 assertions with command mocks, not a real LAN/procd.
- C3: safe geodatabase-open boundaries and early rejection of unsupported build paths. Full support for spaces in paths did not appear; this is not a remaining X proposal.
- C4: busy/editRevision for the dynamic DOM, rejection of ubus errors, ARIA tabs. Old S6-A01 removed from backlog.

The S3-R01/F03/F04 reported by the lead team are the prior policy audits with the same failures on baseline, outside the 11 blockers; X does not turn them into new defects. The additional diagnostic-stage regression and routes-coverage are integrated by the lead team and reflected in its final gate; X does not duplicate that work.

## What Was Kept and How It Was Changed

For each ID, the source lines and expanded evidence are in X1/X2 and the corresponding Terra item. Below is the coordinator's final wording. **P2/P3 is the order for future consideration of improvements, not severity and not a release condition.** User benefit and speedup remain hypotheses unless a narrow local observation is explicitly stated.

| ID / decision | Recommended change or investigation | Condition for moving to implementation, risk, and verification |
| --- | --- | --- |
| **X1-I01 — modify, P2 after reproduction** | Preserve focus after a direct user move/pin if the current DOM actually loses position. | Container replacement statically confirmed, but no browser was launched. First activeElement in stock LuCI for move rule/server and the pin filter. Then boundaries, empty/disabled lists, scroll, rename; poll/probe and late responses must not steal already-moved focus. Benefit: sequential keyboard work. |
| **X1-I02 — modify, P2** | In the existing status, distinguish a confirmed save, application, and an unconfirmed save/service outcome. | Object stubs confirm the current "Saved" status after a service error; they do not prove files are unchanged on save error. Do not claim "not written/not applied" if the action may have executed. Reconcile the real error classes and revision on the final C1/C4; preserve draft/editRevision, do not auto-retry/rollback/restart after an unconfirmed save. |
| **X1-I03 — modify, P3** | Distinguishable accessible card names and an up-to-date "already added to the selected rule" description, without toggle semantics. | Same-named card attributes confirmed locally; screen reader effect not verified. One sourceTitle is not enough: source names may also match. Use a short non-secret context/number only on residual match. Verify the accessibility tree, screen reader, rule change, filter, and idempotent reselection. |
| **X1-I04 — modify, P3** | Conditional compact note about the reason Zapret is unavailable, with a programmatic link between the text and the focusable element. | Do not rely on title/data-tooltip alone. First stock LuCI with keyboard and accessibility tree; then reason change on focus, themes, and touch. Do not return a permanent paragraph, do not include a disabled action, and do not reset focus when the note disappears. |
| **X1-I05 — keep, P3** | Global/subcommand help for existing CLI commands with exit 0, exact side effects, and compatible flags. | Reconcile the final dispatch, including network rollback/commit, rpc, and commands without FlagSet. Verify help for all commands with config missing and writes forbidden; unknown/invalid args remain errors. Do not run operations for the sake of help. Benefit: a clear distinction between validate, fetch/sync, and runtime/network actions. |
| **X2-01 — modify, P2 research** | Profile the validate/compile phases and the lifetime of DB, expanded selectors, dedup, the JSON tree, and Artifacts.Xray. | The protobuf of the selected record is re-parsed, not the whole database. The old S4 does not measure current RSS. Compare the same small completed input with repeated and different selectors; TotalAlloc, live memory, peak RSS, time, and result size separately. Cache only on proven benefit without live-memory growth; streaming is a separate decision, json.Encoder alone is not evidence. Preserve first-match/OR, errors, and Zapret. |
| **X2-02 — modify, P3 before profiling** | Verify the search share in the final reconcile; consider temporary indexes only on a material bottleneck. | Nested passes confirmed by reading; visit arithmetic is not a CPU profile and not a promise of linearity under collisions. Verify aliases, rotation, claims, ambiguity, headings, chains, order, and C1 guards. No new IDs/schema/guessing. An index may increase memory. |
| **X2-03 — modify, P3 modeling** | Measure loader transient/live memory and the deadlines/cancellation model before choosing concurrency. | Support for 16 sources and 4 MiB per source stands. 16×4 MiB is not RSS. Account for the overall fetch 12 s, RPC 24 s, and local/CPU parsing, which is not required to abort instantly. Deterministically verify queue, order, cancellation, and absence of late publication. A pool is not approved: new timeouts on healthy sources are possible. |
| **X2-04 — modify, P2 research** | Verify the fairness of the existing queue and independent application of a ready health group. | 40 ms is the fixture-set barrier, not real downtime. The group must first receive probe slots; a slow apply also delays the others. Verify four occupied slots, shared URL+ID, priority/tie-break, all-failed→block, generation/process, and long apply. Keep ≤4 probes, all prescribed checks, and the 15 s pause after a full cycle. Apply/probe overlap may raise the actual cycle rate: measure the load, do not promise it is unchanged. |
| **X2-06 — modify, P2 on confirmed diagnostics gap** | In the existing diagnostics, distinguish running, the known action result of the current generation, and absence of data. | Use the existing updater status and minimal monitor RAM state; no new monitoring system. Verify how the real UI renders info/error: one backend state is not enough. Long cycle, partial group error, new generation, missing/corrupt status — not false success. No flash writes, no URL/credentials/raw stderr via the read ACL. |
| **X2-08 — modify, P2 guarantee definition** | Clarify the desired config/nodes guarantee on interruption and the minimal detection/recovery path; choose the architecture after that. | Terra reproduced the known F-S2-01 on an isolated intermediate copy, not power loss. First the phased set on the final C1 and a decision whether detection/manual recovery is sufficient. An automatic journal is not approved. Old/new recovery requires proven replay/locks/backup/secret lifecycle and write cost; old+new full config/nodes may occupy up to 16 MiB before overhead. |

Preliminary estimates after the critique: I01 — 1–3 engineering days on a confirmed DOM defect; I02 — a 1–2 day guideline before evaluating the final API; I03 — 1–2 days together with accessibility; I05 — 0.5–2 days for all help paths. I04 needs estimating after verifying the stock tooltip. This is not a promised deadline. The X2 figures (2–4 days for cache/index/loader, 3–5 for scheduler, 5–8 for journal) are not accepted as a justified full price: proof of benefit, lifecycle, backup, and regressions are not accounted for. X2-06 can fit in 1–3 days only for a genuinely small status; UI/generations are estimated separately.

## What Is Excluded Now

| ID | Why excluded | When the question may be reopened |
| --- | --- | --- |
| **X2-05 — cache of the confirmed Xray selection** | Callbacks were measured, not real CLI/API costs. Xray and monitor have separate respawn; skipping apply may hide override loss after an Xray restart and leave block/wrong selection. Rare reconciliation gives no immediate recovery. | Material measured costs plus a proven epoch/identity mechanism and immediate reapply on respawn/API reset, with retry and supported Xray versions. |
| **X2-07 — no-op publication of identical geodata** | Logical write bytes of the old test are known; repeat frequency and flash effect are unknown. Safe comparison under lock adds I/O and C3 complexity, and the 1–2 day price is unjustified. | A confirmed frequent scenario and total I/O comparison on the new C3, including corrupt/missing current and active selectors. |
| **X2-09 — new standalone regression gate** | The lead team is already integrating checks, S4, and ShellCheck. A second gate now creates a duplicate and a competing understanding of acceptance. Historical repros are not rewritten. | Only a specific uncovered scenario or a non-reproducible command after the lead integration completes. Inventorying its result is allowed without a new project. |

## Clarification of the Known F-S2-01 and Save Status

Terra locally reproduced two cases on its own copy: SIGKILL between the nodes/config publications and a synthetic directory-fsync EIO after rename of config. Syntactically valid but inconsistent files and a standalone compile failure were obtained; real runtime/procd application was not run. [Exact basis and results](terra-critique.md) and `build/improvements-X/Terra/save-check/` preserve commands/hashes. The test PASS denoted fixture execution and does not mean successful behavior acceptance.

In the read integrated revision, `internal/router/rpc.go:326` publishes nodes, `:329` config; a config error leads to nodes rollback, `:335` performs the subsequent directory cleanup, and `atomicWrite:597` does rename before `:605` directory Sync. The RPC SHA256 **`e48997b52e86b7f8cf956067b0674c333e3dc29000f1806a71ccd2708df60404`** matches the Terra re-read revision and the integrated manifest. This strengthens the relevance of the uncertain-response question, but **does not mean a new fault injection run on the final integration** and does not raise the old boundary to a new blocker. C1 does not directly claim a shared multi-file crash transaction; the C2 network journal does not replace it.

In X1-I02, two different facts must be taken into account: save may be confirmed while apply is unknown; and the save response itself may be erroneous/lost after the write. A safe outcome message does not equal an automatic retry and must not lose the user's draft. The specific reconciliation/recovery mechanism requires a separate decision after the guarantee is defined.

## Recommended Sequence Without Implementation

1. **Use the completed lead integration as a separate basis.** Its manifest, commands, limitations, and classification of the old policy audits are already preserved by the lead team. X launches no new agents, proposes no separate gate, and does not repeat its checks; the current integrated-source is given above.
2. **Clarify the save outcome and the recovery boundary.** Consider X1-I02 together with the narrow phased X2-08 checks on the final C1; select the required guarantee and the minimum solution before assigning a journal or deadlines. This is post-closure research, not an additional condition for closing the 11 blockers.
3. **Pick small interface improvements.** I05 after dispatch inventory; I01/I03/I04 — after confirmations from real LuCI/accessibility. Preserve the theme style and existing action scenarios. Browser checks here are only criteria for future work: the user did not order them in X, and they were not performed.
4. **Investigate memory and monitor separately.** X2-01 — phased profile; X2-04 — queue, readiness, and lifecycle. Do not tie them into a shared refactoring. Clarify X2-06 through the existing diagnostics view.
5. **Defer search/load optimizations until data.** X2-02 and X2-03 are P3; implementation only on a measured problem and with ID/deadlines preserved. Do not include X2-05/07/09 in the current implementation.

## Limitations of the Final Conclusion

The investigation does not set target CPU/RAM/flash and acceptable p95, does not measure user error/failure rates, and does not confirm a physical router, VPN, real power loss, touch, or screen reader. New X1 data is object stub attributes/logic; X2 — monitor callbacks; Terra — a targeted syscall fixture in its own copy. Historical S4/S6 refer to their old snapshots. Overall build/test, external network, VM/SDK, browser, and sustained load were not run in X. The lead team's checks are cited with their source and are not attributed to X.

The analyst, review, and integration manifests tie conclusions to bytes and time but do not freeze the shared directory. Further code changes require re-verifying the relevant basis before implementation. Product limits, first-match/OR, stable refs, no direct fallback, timeouts, and compatibility remain conditions of all kept proposals.
