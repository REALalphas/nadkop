# C4 — applied LuCI fixes

Date: 2026-11-02. Implementer: gpt-6.1-sol, reasoningEffort=high.
Scope: S6-D01, S6-D02; a small local S6-A01 fix separately.
The implementer worked alone, without launching agents. Changes are actually applied in the
shared workspace; no Git. Ready for integration verification by the lead
agent; shared blocker registries are not updated by this report.

## Changed files

- `openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js`.
- `scripts/test-s6-luci-resilience.mjs`.
- `scripts/fixtures/s6-luci-harness.mjs`.
- New test runners `scripts/closure_c4_s6.mjs` and
  `scripts/closure_c4_regression.mjs`.
- This report. All new logs, JSON, snapshots, manifest, and the applied diff are
  only in `build/blocker-closure/C4/`.

`picker.js` and `picker.css` were unchanged. Other people's owned files, backend/core/init/build,
SDK, dependencies, package versions, VM, host networking, and user data
were unchanged. No required integration diffs for other people's owned files.

## Original behavior: personally reproduced

Read team.md, blockers.md, errors.md, product-contract.md, the S6 report, and three
proposed patches from `build/agent-audit/S6/`. Historical files were not rewritten.
Baseline assets were taken from `build/blocker-closure/baseline/`; SHA256 of all five
verified owned files matched its original manifest. See
`source-checks.json` (local stand artifact).

Command from the project root:

```bash
S6_CASE='^S6-(apply-pending-.*|poll-return-during-save-lock|geodata-return-during-save-lock|title-double-click-during-save|rpc-adapter-ubus-control|ubus-.*|keyboard-tab-arrow-pattern)$' \
  node scripts/closure_c4_s6.mjs --baseline \
  > build/blocker-closure/C4/baseline.log 2>&1
```

Actual exit **1**, **13 scenarios: 2 PASS / 11 FAIL**:
five D01, five D02, one A01. The adapter control and probe error passed.
Reproduced: editable editor/deletion of the selected server during apply,
late geodata/status, rename on dblclick, acceptance of `result:[9]` as success, and
restart after a rejected save. This is an own rerun on the saved code,
not just a reference to the old audit. JSON and PNG of each failure are kept in
`baseline/` (local stand artifact).

The baseline run used the original rpc.js of SDK 24.10.8. The historical
second-SDK reproduction was read in S6; own checks of the second SDK
at this stage were run on the fixed implementation, see below.

## S6-D01

`lockControls()` re-locks controls after every `domReplace` and after
point replacement of the name editor with a label/rename button. Original disabled/inert states
are saved once per element and restored when the task completes.
A repeated rerender does not memorize temporary busy as permanent disabled.
Action-compatibility and order-boundary checks keep applying.

Rule/source names, deletion, and moving of the selected server have
busy-guards. Existing assignment and reorder/drop guards are preserved. On
late dragend the picker is recreated with locked controls. The name editor
via dblclick does not open until the task completes.

`editRevision` tracks real draft changes after the save snapshot.
A successful save updates the backend revision even with a newer local edit,
but the "Saved"/"Saved and applied" text is set only for an unchanged
draft. A real late edit stays in the editor as "Unsaved" and
is sent by the next save with the new backend revision. A successful restart refers
to the already saved snapshot. Blur/paste normalization of domains/IP triggers
`changed()` only when values change; otherwise a late blur created a false
dirty. Normalization and preservation of original values were not removed.

The original five strict S6 assertions are now PASS. New checks additionally
verify repeated rerenders, held validate/service, double activation,
dragstart/drop/dragend, rename, control restoration on success/failure,
late status/geodata, and draft/revision preservation by the next request.
For a late draft the test intentionally dispatches input on a disabled field: this models
a deferred editor callback, **not a claim about available user input**.
Regular user input during a task is prevented by disabled.

## S6-D02

All **13** `rpc.declare` declarations set `reject:true`, including get,
validate, status, identity, save, geodata, updategeo, probe, import, addsource,
nodes, check, service. The existing `nobatch` for probe is preserved.

A nonzero ubus status is now rejected before the `expect` substitution of `{}`. The Promise
chain validate → save → restart stops on a validate/save error.
The previous catalogue and revision are preserved; the error is shown to the user,
the unsaved draft is retained. A regular Nadkop error `{error:...}` keeps
being handled via `checked()`.

New tests extract the real declarations from the view, pass them to the original
SDK rpc.js, and via the existing HTTP/JSON-RPC mock verify for each method
codes **1, 4, 9** and positive **0 with payload**: 52 RPC checks per SDK.
The mock does not execute failing methods. Separate UI scenarios verify failed
validate/save for both ubus and Nadkop-error, absence of even a service request, a successful
retry with the old revision, then the next save with the new revision.

## Separately S6-A01

Locally added ArrowLeft/Right with wrap, Home/End, and roving tabindex for the already
existing ARIA tablist; tab design and composition were unchanged. The original
arrow-pattern and the new test of both boundaries PASS.

The previous `S6-keyboard-controls` expected Tab to move from one tab to another.
For a correct roving tablist this previous expectation no longer fits: replaced with
strict checks of Tab exiting the tablist, Shift+Tab returning to the active tab,
and ArrowRight moving to the next one. Checks for Enter, Space, Escape, rename, and
focus restoration are preserved. This is an accessibility-expectation change, not
a weakening of the D01/D02 checks.

## Final commands and results

All commands are run from the workspace root. Node v26.13.0, headless Chromium;
Playwright and Chromium are used from already existing local installs.
No preview tools, dependency installs, or servers were launched.

```bash
# All 84 original S6 scenarios, including successful/negative RPC,
# timeouts, source revision, probe concurrency, focus, themes, and 390px.
S6_ARTIFACT_DIR=build/blocker-closure/C4/final-s6 \
  node scripts/closure_c4_s6.mjs > build/blocker-closure/C4/final-s6.log 2>&1

# New 13 C4 scenarios with additional boundaries.
C4_OUTPUT=build/blocker-closure/C4/final-regression \
  node scripts/closure_c4_regression.mjs \
  > build/blocker-closure/C4/final-regression.log 2>&1

# Original RPC/ubus mocks with the second SDK: 12 scenarios.
S6_LUCI_RPC=build/openwrt-vm/sdk/feeds/luci/modules/luci-base/htdocs/luci-static/resources/rpc.js \
  S6_CASE='^S6-(rpc-adapter-ubus-control|ubus-.*|rpc-(validate|save|service|nodes|probe)-success)$' \
  S6_ARTIFACT_DIR=build/blocker-closure/C4/final-s6-sdk-second \
  node scripts/closure_c4_s6.mjs > build/blocker-closure/C4/final-s6-sdk-second.log 2>&1

# All new boundaries with the second SDK.
S6_LUCI_RPC=build/openwrt-vm/sdk/feeds/luci/modules/luci-base/htdocs/luci-static/resources/rpc.js \
  C4_OUTPUT=build/blocker-closure/C4/final-regression-sdk-second \
  node scripts/closure_c4_regression.mjs \
  > build/blocker-closure/C4/final-regression-sdk-second.log 2>&1

node --check openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js
node --check scripts/test-s6-luci-resilience.mjs
node --check scripts/fixtures/s6-luci-harness.mjs
node --check scripts/closure_c4_s6.mjs
node --check scripts/closure_c4_regression.mjs
```

| Final run | Result | Exit | Evidence |
| --- | --- | --- | --- |
| Full S6, SDK 24.10.8 | 84 PASS / 0 FAIL | 0 | `results` (local stand artifact) |
| New C4 regression, SDK 24.10.8 | 13 PASS / 0 FAIL | 0 | `results` (local stand artifact) |
| Original RPC/ubus, second SDK | 12 PASS / 0 FAIL | 0 | `results` (local stand artifact) |
| New C4 regression, second SDK | 13 PASS / 0 FAIL | 0 | `results` (local stand artifact) |
| Five syntax checks | PASS | 0 | commands above |

These are 97 distinct scenarios; the second 25 are a rerun with a different rpc.js, not new unique
scenarios. No full overall product regression is claimed by this.

Original SDK rpc.js files unchanged:

- 24.10.8: `ebdb47b45dcb6de4d3f209f034dcc408c36cb429b717424fe84e22274cf3dea4`.
- Second: `7d1c23fac98fc1c8a86d1c75106eb39bd045eaeb1e3f1d10085abf2b87a9955f`.

SHA256 of the final overview.js:
`2787804aa56f5238394492ab51b411479bae02345d0ec6e527ec78cce767cbca`.
`Actually applied diff` (local stand artifact).

## Source checks and intermediate failures

The old S6 runner by default still requires the historical frozen-source
manifest. An explicit `S6_MANIFEST` was added for the permitted fix stage;
the closure runner creates a scoped manifest of six files (three assets, S6 runner,
harness, used SDK rpc.js), preserving strict before/after assertions.
This lets other C-agents edit their areas without a false shared freeze.
`S6_SOURCE_ROOT` only selects saved assets for baseline; RPC mocks,
frame handling, D01/D02 scenarios, and original SDK parsing were not rewritten.

In both final S6 runs scoped hashes before/after are equal, mismatches **0/0**.
New runners also record the hash of the read view/rpc. The old 327 files
are not declared wholly unchanged: other blocker edits are permitted in parallel.

Intermediate logs are kept, not replaced:

- `fixed-s6.log`: 83/84; previous Tab assertion and SHA mismatch because I
  changed normalize while my own run was still executing. This
  run is **not accepted** as the final check.
- `regression.log`: 8/10; found a false dirty from a no-op blur after snapshot.
  Fixed normalize, not the test expectation of the saved state.
- `fixed-s6-v2.log`: 83/84, hashes stable; the previous Tab assertion remained.
- `regression-v2.log`, `regression-sdk-second.log`, `fixed-s6-sdk-second.log`:
  intermediate successful checks before adding the last three C4 scenarios
  and the final keyboard expectation update.

## Limitations and remaining work

S6-D01/S6-D02 are fixed and verified in the LuCI area; S6-A01 is also locally fixed.
No open C4 errors remain in the verified scenarios. The lead agent performs the final change of
the shared blocker status and the overall integration regression.

The checks execute the real view/picker and the unchanged SDK rpc.js in Chromium,
but backend, storage, service, and HTTP/ubus are synthetic mocks. Real rpcd,
service restart, Xray, nft/ip, LAN/VPN, VM deployment, and SDK build were not run.
Proven "no restart" is absence of a service request and mock action; this is not
a procd measurement on a VM. Real backend revision serialization was not
reverified here. The model shows the correct arguments of subsequent requests.

Standalone DeviceSelect is the original S6 HTML adapter; the real LuCI dropdown,
inert, and screen reader were not verified. Preservation/restoration of the original inert
is implemented but is not claimed as a separate VM check. Not verified: Firefox,
WebKit, touch, screen reader, multi-hour load, and behavior on any
semantically incomplete response with ubus code 0. The D02 fix guarantees strict
rejection of nonzero ubus codes; no new scheme for verifying all success payloads was added.
