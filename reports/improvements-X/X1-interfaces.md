# X1 — LuCI/CLI, accessibility and clarity of existing actions

Analytical report, no implementation. Author: separate X1 stream. Working folder: `<repo>`; no Git. First observation UTC: **2026-11-02T19:59:14Z**. Main snapshot: **2026-11-02T20:06:22.117705+00:00**; local check: **2026-11-02T20:46:11.840Z** (repeat after resumption). Final verification time and currency are given below.

## Subtotal

Five small changes to the existing interface are proposed: preserving focus after reorder/pin; a distinguishable save and apply outcome; accessible card names and state; keyboard access to the reason Zapret is unavailable; CLI help. No new confirmed blockers were found. This is an improvement backlog for discussion, not a decision on implementation or release readiness. All effect and effort estimates are hypotheses, without measurement of user errors or task completion time.

The ordering below reflects benefit and confidence; P2 means medium improvement priority, P3 means low. No item is assigned as a standalone release blocker.

| Order | Stable ID | Priority | Subject | Work estimate with verification |
| --- | --- | --- | --- | --- |
| 1 | X1-I01 | P2, preliminary | Focus after reorder and pin | 6–12 person-hours |
| 2 | X1-I02 | P2 | "Save and apply" outcome on failure | 3–6 person-hours |
| 3 | X1-I03 | P3 | Distinguishable servers and accessible selection state | 3–5 person-hours |
| 4 | X1-I04 | P3 | Zapret unavailability reason via keyboard | 2–4 person-hours |
| 5 | X1-I05 | P3 | Help for existing CLI commands | 2–4 person-hours |

Estimates are approximate, not commitments. The focus work is more expensive than the rest: it requires verifying the real DOM after reorder. Work allocation is decided by the coordinator after critique of stream X.

## Basis and method

Read: `reports/blocker-closure/team.md`, `README.md`, `docs/product-contract.md`, `docs/interface.md`, `reports/blockers.md`, the relevant S1/S4 parts and the audit summary, the full S6 report, the current `overview.js`, `picker.js`, CSS, CLI `main.go`, RPC `rpc.go`, and the rpcd wrapper. Read the S6 JSON files on keyboard, error focus, service failure, and themes. The S6 test sources were studied as a description of the historical check; the runner was not launched.

Currency priority: user instruction → current sources and contract → `team.md` → historical audits/packages. The no-implementation rule for X1 was observed. The old S6 snapshot was not treated as current code; the shared team changed files during the analysis.

All proposal paths and lines refer to **`build/improvements-X/X1/snapshot-current/`**, with the same relative path inside the tree. These are immutable copies of the read bytes; the absolute root is given at the start. For convenience:

- **O** = `openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js`;
- **P** = `openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/picker.js`;
- **C** = `openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/picker.css`;
- **M** = `cmd/nadkop/main.go`.

New local observations were obtained with `node build/improvements-X/X1/inspect-ui.mjs`: the extracted unmodified bodies of functions from the current O, the `picker.card` function, and the tab-creation block were run on simple stub objects. Saving, network RPC, browser DOM, and system commands were not executed. Result: `build/improvements-X/X1/local-observations.json`, source SHA included. Node v26.13.0 was used. This is a logic/attribute check, not browser or screen reader acceptance. The first version of the custom extractor did not recognize the tabulation before `moveRule` and terminated with an assertion before writing results; only `inspect-ui.mjs` was fixed, then the check passed.

Evidence is separated explicitly: **new measurement** — the result of this local check; **historical data** — S6; **hypothesis** — the expected user benefit and not-yet-measured browser symptoms. Static code reading is marked separately.

## Proposals

### X1-I01 — preserve working focus after reorder and pin

**Problem.** The order arrows and pinning trigger recreation of the containers holding the pressed button. There is no explicit focus return in these paths. For sequential keyboard operation this is a probable loss of position. Focus loss in a real browser was not measured in X1, so the P2 priority is preliminary; this is not a claim that the actions are fully inaccessible.

**Path/lines/evidence.** O:166 removes all previous `domReplace` descendants; O:687–688 recreates the rules. O:322–327 (`moveServer`), O:554–561 (`pinServer`), O:812–817 (`moveRule`) call `rules`/`renderPool`, without `.focus()`. In `local-observations.json` the `mutationEvidence` field confirms these calls on synthetic IDs; actual removal of the focused DOM element was not modeled. Unlike these paths, O:670–672 returns focus to the matcher, O:712 returns it to rename. **Historical data:** S6 `S6-keyboard-controls` and the three `S6-focus-*-rule-error` pass only their own scenarios; they do not prove focus after moving servers/rules or pin.

**Concrete change.** After the listed user actions, return focus to the corresponding button of the same server/rule in its new position. If the arrow direction has become unavailable, select the opposite arrow of the same element; if the pin filter removed the card, select the nearest visible card, then the filter. Identify a rule by its object in the current editor, a server by ID; do not introduce a new configuration format. Do not restore focus automatically on a regular poll/probe.

**Effect.** **Hypothesis:** several moves or pins can be performed in sequence without re-finding the position with the Tab key. The number of saved keystrokes and the frequency of the problem are unknown.

**Cost.** 6–12 person-hours, including regressions. **Risk:** stealing focus from the user after an async response, moving to a vanished element, incorrect binding after sorting, impact on scroll. Apply restoration only to the completion of the specific user action.

**Verification criterion.** In the future, in the real DOM: move a rule and a server three times via keyboard; focus stays on the moved element, and the order matches the data. Check both list boundaries, pin with the pinned filter enabled, and regular pin. Between actions, poll/probe does not change `activeElement`. The rename/matcher and error-focus regressions remain passing. No browser check was run here.

### X1-I02 — explicitly show the apply outcome after a successful save

**Problem.** On a successful save with a failed service, "Saved" remains next to the main action. The error is notified separately. This is an accurate write status, but it does not show whether the requested apply completed; a service failure and a lost response to it require different wording.

**Path/lines/evidence.** O:167–186, especially O:181 and O:183; shared `notifyError` — O:118–124. **New measurement:** `local-observations.json/saveEvidence`: success path → "Saved and applied"; service payload error and lost response → "Saved" + one notification, revision already `new`; validate/save failure → "Not saved", revision `old`, service not called. This is isolated UI logic, not a disk write. **Historical data:** S6 report:25 and runner `scripts/test-s6-luci-resilience.mjs:185–195` specifically check "Saved" after three service failures. The final S6 JSON files were captured after a repeated successful apply, so their resulting "Saved and applied" is not evidence of the first phase. README:414–419 explains that the saved config may differ from the running state.

**Concrete change.** In the existing live-status near the buttons, show "Saved; applying…" after save. On an explicit service failure — "Saved; apply failed"; on a lost response/timeout — "Saved; apply result unconfirmed". Keep the reason in the notification and the existing "Save and apply" retry. Do not add RPC, retry automation, rollback, or a claim about which generation is actually running. If the adapter does not allow reliably distinguishing the failure type, use the cautious generic wording "apply unconfirmed".

**Effect.** **Hypothesis:** the user distinguishes settings persistence from confirmed apply, and less often treats the action as fully complete. The error count was not measured.

**Cost.** 3–6 person-hours. **Risk:** calling an unconfirmed apply a guaranteed failure, overwriting a fresh draft, breaking the C4 `editRevision` guard. Change the status only for the same revision and after a confirmed save.

**Verification criterion.** Success, explicit service failure, lost response after possible execution, validate/save failure: status, notification, revision, RPC count, and draft preservation are correct. There is no "not applied" wording where the fact is unknown; no restart after a refused save; retry uses the new revision. Scenarios S6-D01/D02 remain C4 regressions.

### X1-I03 — distinguish cards by subscription and announce assignment

**Problem.** Two servers with the same name from independent subscriptions have the same accessible button name. The already selected server differs by CSS class, but the select button has no accessible state. A single label does not convey the source and the fact of assignment to the selected rule.

**Path/lines/evidence.** P:121–124 sets `aria-label = 'Select ' + name`; the source/protocol are in the child text at P:114–118, which the explicit button label does not include. P:122/P:132 encodes `selected` with classes; P:126–128 already correctly uses `aria-pressed` for pin. O:582–585 passes selection and the subscription name into the card. **New measurement:** `local-observations.json/cardEvidence`: `srv_one`/`srv_two` from Provider One/Two, both Frankfurt, selection true/false → both labels "Select Frankfurt", `aria-pressed` absent, classes differ. The actual returned attributes of `picker.card` on objects were checked, not the computed accessibility tree.

**Concrete change.** Include the displayed subscription name and the "added to the selected rule" state in the existing accessible label. Prefer an explicit state description via `aria-describedby` or the label; do not turn the button into a removal toggle: repeated selection is currently idempotent. Keep the visual provider name and the assignment behavior. For the accessible name, do not use the URL, access ID, or outbound contents; the already visible `sourceTitle` is sufficient. When no rule is selected, the label must not promise assignment to a specific rule.

**Effect.** **Hypothesis:** a screen reader user distinguishes same-named connections and understands the assignment result. Screen reader usage itself and the frequency of name collisions were not checked.

**Cost.** 3–5 person-hours. **Risk:** overly long announcements, stale state after changing the selected rule, disclosure of hidden parameters. Limit the added text to the displayed source name and a brief state.

**Verification criterion.** In the future accessibility tree, the two Frankfurts have different names with sources; the state matches the selected rule after assignment and rule change. Pin, search, and single-subscription filter work; repeated selection does not remove the server. No URLs/credentials in the new attributes. Manual screen reader verification is required to confirm the effect; it was not performed here.

### X1-I04 — provide keyboard access to the reason an action is unavailable

**Problem.** For a not-yet-selected Zapret, the unavailability reason is in the `title` of a disabled button, while the visible explanation is hidden. Such a button is not a regular Tab stop. A keyboard user gets no standard path to the reason, although the needed text is already computed.

**Path/lines/evidence.** O:452–456 computes the missing WAN/strategy. O:477–482 sets disabled/title; `aria-describedby` is assigned only if the unavailable action is already selected. O:485 hides the notice for another selected action; O:797 creates the disabled button. **New measurement:** `local-observations.json/disabledReason` for `action:'direct'` without WAN/strategy: `disabled:true`, non-empty title, `describedby:null`, `visibleNote:false`. This is a check of attributes and text; specific screen reader behavior was not measured. `docs/interface.md:195–196` deliberately removes the permanent explanation for another selected action — restoring it permanently is not proposed.

**Concrete change.** Next to the unavailable Zapret, add a compact focusable "?" with the already existing native LuCI tooltip and the name "Why Zapret is unavailable". Pass the current `actionReason` into it; leave the action button unavailable. Use the existing `help` helper at O:31–34, not a separate panel or palette.

**Effect.** **Hypothesis:** the reason and the needed next action can be learned via keyboard without enabling the wrong action; the text does not permanently occupy space in the rule.

**Cost.** 2–4 person-hours. **Risk:** an extra Tab stop in every rule, a stale reason after WAN/database changes, the tooltip itself being inaccessible. Show "?" only when a reason exists; update it together with compatibility.

**Verification criterion.** On a new rule without Zapret settings, help is reachable via Tab; on focus it opens the current reason, the action remains disabled. After selecting WAN/strategy, the help disappears and Zapret is available. Check the incompatible-set scenario, the absence of permanent text, and the Bootstrap/Material/OpenWrt2020 themes. Touch behavior is unknown here; it cannot be considered confirmed by this proposal.

### X1-I05 — make CLI help sufficient for the first action

**Problem.** The list of command names does not explain the difference between a local check and actions that fetch sources or change state. A global `--help` falls into `unknown command`; help for individual flag sets returns an error in `main`. The successful validate message already honestly states that subscriptions were not fetched.

**Path/lines/evidence.** **Static reading:** M:24–27 — command list only; M:72–79 — unknown command and flag parser error propagation; M:17–20 — any returned error leads to exit 1. M:75–77 registers identical config/output flags for validate/nodes/generate, although output is used only in generate. M:100–102 — `configuration valid (subscriptions not fetched)`; README:272–295 contains a useful validate/generate distinction that the CLI user has to reach separately. A fresh CLI build and run were not performed: ready binaries may lag behind the current C1 changes.

**Concrete change.** Add a global `-h`/`--help` and a successful exit from the existing subcommand `-h`/`--help`. Briefly describe the already existing commands: validate — local structure without fetching; nodes — source fetching and catalog sync; generate — file generation into a new directory; apply/network — runtime/network changes; updater/monitor/rpc — service commands. For output, explicitly state applicability to generate only. Preserve the existing commands, flags, their compatibility and meaning; limit the change to help and help handling.

**Effect.** **Hypothesis:** fewer mistaken expectations of validate and accidental runs of state-changing commands. User research and CLI statistics are absent.

**Cost.** 2–4 person-hours. **Risk:** regression of error exit codes, false description of side effects, help diverging from C1 after fixes. Change the exit code only for explicitly requested help; leave argument errors as errors; reconcile the exact catalogue description with the final C1.

**Verification criterion.** After implementation, global and subcommand help returns exit 0 without reading user configs and without writing state/directory/network. An unknown command and an invalid flag return a non-zero code. The commands from help match the existing dispatch; `validate` still does not fetch subscriptions. Check help for apply/updater/network/monitor/rpc separately, without invoking their actions.

## Duplicates and the already moving base — not a separate backlog

- **S6-D01**: the initial O SHA `a4dbad7f…` still contained the old locking. In the second snapshot C4 added `lockControls`, re-locking after `domReplace`, guards, and `editRevision`; then separately changed normalization on blur. The historical five S6 FAILs are not declared current FAILs. Full acceptance of the fix belongs to C4/the coordinator; X1 did not repeat it.
- **S6-D02**: initially only probe had reject. In the current O:14, `reject:true` applies to all methods. The old ubus reproducer does not automatically carry over to the new code. X1-I02 considers the clarity of a **correctly rejected service after a successful save**, which differs in condition and effect from the false success of a failed save in D02.
- **S6-A01**: the old ArrowRight/roving tabindex deficiency was fixed in C4 code at O:908–935. **New measurement:** the tab block on object stubs passed ArrowRight, End, Home, ArrowLeft with matching focus/selection and a single Tab stop. This is not a repeat of the historical browser test. **Outdated conclusion:** A01 must not be included as a separate open X1 improvement.
- **D-S2-01/S3-F01/S1-F1/F2/S4-D01**: C1 scope; M:84–90 already has configuration locking before reading. The proposal to fix catalogue/state must not be repeated under the guise of CLI-UX. X1-I05 changes only the explanation of existing actions and help.
- README:28 of the initial/current snapshot contains the old "no blockers found"; the current registry already reports a separate fixes stage. This is coordinator work on shared documents, not a new X1 improvement and not grounds to deny blockers. Old freeze statements in audits/contract describe the previous stage.

## What needs no separate proposal on the available data

CSS scopes the main styling to `.nk-app`, uses sizes/layout and theme classes, and contains reduced-motion for probe/refresh animation (C:97–99,120–121). S6 `theme-final` historically shows no positive horizontal overflow at 390 px in the selected themes; its `final` does not replace the refined `theme-final`. There is no new data for changing layout/palette, enlarging all buttons, or list virtualization. No such changes are proposed in X1. This is not a conclusion about sufficient contrast, touch targets, or performance on large catalogs.

A subscription is added immediately after paste/Enter/blur per the existing contract and README; X1 does not propose additional confirmation or a change to this scenario. UI files use stable IDs, not credentials, for drag. The standard DeviceSelect, its real keyboard, and the LuCI shell were not checked by this analysis.

## Limitations and unknowns

1. No browser, preview, server, VM, SDK, package build, clock or load checks, network requests, or real subscriptions/credentials were run. Product code, project scripts/tests, dependencies, and configs were not changed. New files exist only in `build/improvements-X/X1/` plus this single report; others' reports and shared registries were not edited by X1.
2. Real-DOM focus after pin/move, accessibility tree, screen reader, touch, contrast, 200% zoom, Firefox/WebKit, and physical router were not measured. Object stubs cannot be treated as confirmation of full accessibility or real apply. X1-I01 has lower confidence pending real-DOM reproduction.
3. S6 is historical mock-backend data, not current C4 acceptance. The final status in JSON after retry does not prove the intermediate state; the runner was read for the phases. Ubus/timeout frequency, user-rule complexity, and the CLI-user share are unknown.
4. Snapshot is a sequential read of files, not an atomic copy of the whole running program. Each file has its own recorded UTC and SHA256; the final verification detects byte changes after reading, but does not prove the absence of a transient write that restored the previous bytes.
5. Priorities, hour estimates, and benefit are preliminary; critique of stream X and the implementation decision belong to the coordinator. X1 did not launch further delegation.

## Snapshot manifest and final verification

Three recorded states: `build/improvements-X/X1/snapshot-manifest.json` (initial), `snapshot-mid-manifest.json` (after the first C4/C1 edits), `snapshot-current-manifest.json` (basis of the proposals). Each contains the relative path, exact SHA256, size, read UTC, and path of its own copy. The historical S6 and the read documents are included in these 32 entries. Changed initial conclusions are explicitly withdrawn above.

Key SHA256 of the main snapshot:

| Source | SHA256 |
| --- | --- |
| O | `2787804aa56f5238394492ab51b411479bae02345d0ec6e527ec78cce767cbca` |
| P | `b30b605ad778e343d2ba4f85e24d2953bd7769eb0910bfbdb42bb8db26a5dc88` |
| C | `e36bc93b4b325222e77b45c3cc1bc9596e6ead389669267a6e10fd497b6de52f` |
| M | `a807eafccc148a6e1e6d7acba8ea43ec9708dbe4878b444d926f1d85eee738d8` |
| `internal/router/rpc.go` | `39dce816b0af2487e3ce5258bfdaf2dd4bdde034a4344c4ae203efb6768dca24` |

Final verification UTC: **2026-11-02T20:46:11.913007+00:00**. **32** source files and the SHA256 of all own copies were checked. **Divergences from the main snapshot: 0.** This is consistency of what was read with the verification moment, not completion of C1–C4 work.

SHA256 of `snapshot-current-manifest.json`: `d29b30e3bb0c4a0415ebf439a3c9f5ccbfe03131b9dd84456f8483ec988355ed`. Machine-readable verification: `build/improvements-X/X1/final-verification.json`.

**New blockers for the coordinator: none.** X1-I01–I05 are proposals; S6-D01/D02 and the already changed A01 are not added again.

