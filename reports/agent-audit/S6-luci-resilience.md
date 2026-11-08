S6 — LuCI resilience, core 0.3.0-r4 / LuCI 0.3.0-r8

The audit was performed on 2026-11-02 by a dedicated S6 test agent. Production code was not modified. Only its own tests, this report, and `build/agent-audit/S6/` artifacts were added. No subagents, delegates, VMs, SSH, SDK builds, or shared runners were used. All sources, names, HWIDs, and JSON-RPC session IDs are synthetic.

**Result: 84 unique scenarios, 73 PASS / 11 FAIL.** Five FAILs relate to S6-D01, five to S6-D02, one to S6-A01. These are two confirmed P2 resilience defects and a separate P3 keyboard-accessibility shortcoming. This run must not be treated as green or as confirmation of the whole product's readiness. All 327 protected files matched the manifest before and after.

Final machine-readable index: `S6-summary.json` (local stand artifact). Main run: `final.log` (local stand artifact), `final/results.json` (local stand artifact). It contains all 84 scenarios and finished with exit 1 in 61.368 s. After the Bootstrap adapter was refined, 12 themed scenarios were repeated separately: `theme-final.log` (local stand artifact), 12 PASS, exit 0. In S6-summary, themed results are replaced by this latest check; repeats do not increase the unique-scenario count.

**What was executed.** `scripts/test-s6-luci-resilience.mjs` (local stand artifact) and the standalone `s6-luci-harness.mjs` (local stand artifact) load the unmodified `overview.js`, `picker.js`, `picker.css`, plus the genuine LuCI `rpc.js` from local SDK 24.10.8. Chromium 156.0.8078.79, Node v26.13.0, headless, one sequential scenario, at most two tabs, `--renderer-process-limit=2`, `nice -n 10`, outer `timeout 180s`. No shared full run of other tests was launched.

An S6 Fetch-based adapter with AbortController is installed instead of LuCI `request.post`; `rpc.declare`, JSON-RPC parsing, `expect`, and `reject` are performed by the original LuCI `rpc.js`. All page requests are intercepted by `page.route('**/*')`. HTTP responses, errors, JSON-RPC, and delays are local; no server is started. Held responses are released explicitly; timeouts are measured in real Chromium time. `view.load()` also runs through local get/status/identity, then the real render is invoked. Poll is registered in the adapter and invoked explicitly for a controlled race; the real five-second LuCI scheduler was not tested.

The final selected scenarios executed 505 intercepted JSON-RPC requests, including each view load. Unhandled page exceptions: 0; unhandled rejection: 0; unexpected external page requests: 0. Chromium console messages about artificial HTTP/connection errors are expected and saved in the JSON of individual scenarios.

**Actually verified conditions.**

| Area | Scenarios and measurable result |
| --- | --- |
| RPC validate/save/service/nodes/probe | 25 PASS: each response is held at least 90 ms; success, `error` payload, HTTP 503, `route.abort('connectionreset')`, truncated JSON. A double activation accounts for exactly one RPC before and after the response. |
| Lock release | Editing and saving recover after errors; rule edge arrows and incompatible Zapret stay disabled. `aria-busy` and re-run verified. S6-D01 describes separate DOM-recreation paths. |
| Preservation | The entire draft is compared on all standard failures. Inactive IPs and sets, conditions, pinned/order, nodeRefs, and the probe URL are preserved. For proxy/direct, the hidden selection URL is preserved separately under mode=ping with a save error. |
| Multiple editors | Three PASS with two tabs of one browser context and a shared mock backend: stale save+apply, simultaneously submitted identical revision, source commit in another tab. Only one concurrent save is committed; the losing draft and the entered URL remain; no restart is sent after a stale save. This is UI integration with the revision model, not a re-check of the backend lock. |
| Lost response after commit | One PASS: the mock save is committed, the response is cut; the UI keeps the draft, does not write "Saved", does not send restart. A repeat save is rejected on the old revision; no second write. |
| Save+Apply on service failure | Three PASS: payload error / abort / HTTP 503 after a successful save. The status stays "Saved", the draft is preserved; the retry gets a new revision and only then restarts. |
| Probe concurrent with save | Three PASS: probe finishes first, save finishes first, save fails first. An unfinished probe stays disabled; a finished probe does not lift the overall lock. |
| Probe queue | PASS: the old URL response does not render 777 ms over the new probe; the new probe renders 123 ms. proxy/direct/proxy switching, continuation after abort, "Probe already running" retry. For 16 servers — four sequential RPCs of 4 IDs; one rejected batch yields 4 errors and 12 oks, links stay unchanged. |
| Timeouts | validate/save/nodes/service with a 250 ms adapter deadline; probe after such a timeout does one retry and releases pending. A standalone save+apply with the normal 20 s finished in 20,061 ms: 0 writes, 0 restart RPCs, draft preserved, controls released. This is an HTTP-adapter timeout, not a measurement of rpcd/the real router. |
| Catalogue and selected server | After a nodes error the catalogue is not cleared. When the selected ID successfully disappears, it stays in the rule as missing; no available spare is substituted. No automatic ID replacement was found under these failures. S6-D02 is a separate unrejected ubus status. |
| Sources and JSON | Three source failures preserve the URL and the entire independent draft. Invalid headers/local JSON report an error and clear busy; validate/save are not invoked on invalid nodes JSON. |
| Keyboard and focus | PASS: Tab/Enter between sections, Space on matcher, Escape/Enter when renaming, focus return to rename. Local errors and structured validate/save errors expand the needed rule, switch sections, and focus `aria-invalid` with `aria-describedby` on the alert. |
| Themes and mobile | 12 PASS: Bootstrap, Material, OpenWrt2020 × 1360/390 px × browser light/dark; in each, 5 sections with open details, draft preservation, and visible error focus verified. Positive horizontal overflow — 0 px in 60 checks. Bootstrap genuinely switched via `data-darkmode`: background rgb(255,255,255) → rgb(34,34,34). Material and OpenWrt2020 keep their original palettes; no separate dark version of these themes is claimed. |

**S6-D01 — P2, medium: new DOM controls bypass busy; unsaved changes get saved status.**

Root cause in `overview.js:125` (local stand artifact): the task remembers and locks only existing `button/input/textarea/select`. `domReplace:154` (local stand artifact) does not apply the lock to new elements. After save, the catalogue recreates rules and the picker before the service RPC completes (`overview.js:168` (local stand artifact)); late geodata/status also update the DOM. A rule name is a `span` with dblclick and no busy check (`overview.js:677` (local stand artifact)).

Primary reproducer `S6-apply-pending-editor-lock`:

1. In the rule, keep the domain `draft.s6.test` and two stable nodeRefs; press "Save and apply".
2. validate/save complete successfully, save returns servers; hold the service response.
3. The recreated "Domains" field has `disabled=false`. Enter `unsaved-during-apply.s6.test` with a genuine Playwright `fill`.
4. Release service. The editor shows the new domain, the mock-saved config still has `draft.s6.test`, status is "Saved and applied", no notices.

Expected: editing is unavailable until the global task completes; if a new draft is admitted anyway, it must stay "Unsaved". In fact, the new edit is not part of the submitted clean config and its dirty status is overwritten. The user may leave the page relying on the wrong status. Real storage on the router was unchanged in this test.

Four more confirmed paths of the same cause:

- `S6-apply-pending-selected-server-lock`: the new "Remove server from rule" button is active. An explicit user click removes the first ID in the editor; the mock save still contains both IDs, status "Saved and applied". This is a permitted UI mutation during busy, not a spontaneous selection of another server.
- `S6-geodata-return-during-save-lock`: after the late geodata, the new Geosite URL is active. Entering `https://unsaved-edit.s6.test/geosite.dat` stays only in the editor; the submitted config contains no geodata, status "Saved".
- `S6-title-double-click-during-save`: hold validate and perform a genuine `dblclick` on the title. The new enabled input accepts `S6 unsaved name during save`; after Enter and save completion, the mock config has name `S6 route`, the editor has the new one, status "Saved".
- `S6-poll-return-during-save-lock`: the already started status returns during save and creates enabled service buttons. A direct DOM click does not invoke service thanks to the busy guard. Here a disabled violation is proven, while the side operation did not occur.

All five scenarios are FAIL and have `state-snapshot JSON` (local stand artifact), plus PNGs in `build/agent-audit/S6/final/` with the same IDs. The JSON contains before/after, RPC parameters, and mock-backend config, so the conclusion is not based on a single screenshot.

Proposal without application: `proposed-S6-D01-dynamic-lock.patch` (local stand artifact). Re-lock new controls after domReplace, preserve the original disabled only once, and forbid editName while busy. The patch was not executed and is not declared a verified fix.

**S6-D02 — P2, medium: nonzero ubus status is treated as a successful empty object.**

Exact condition: HTTP 200 and a well-formed JSON-RPC frame `{"jsonrpc":"2.0","id":N,"result":[9]}` without payload, i.e. a ubus-level error rather than a normal Nadkop response `{"error":"..."}`. Normal Nadkop errors, HTTP 503, aborts, and truncated JSON are handled correctly in the matrix.

In `overview.js:14` (local stand artifact), `reject` is enabled only for probe; the other methods have `expect: {'': {}}`. The original `LuCI rpc.js:93` (local stand artifact) throws a nonzero-status error only on `req.raise`, then `rpc.js:105` (local stand artifact) replaces the number 9 with an empty object. `checked:17` (local stand artifact) sees no result.error and treats the operation as successful. The genuine rpc.js is used here; the mock returns the standard error frame and does not execute the rejected method.

Expected: the error is shown, no success status is set; after a failed save no service is called; the previous nodes catalogue remains. Actual results:

| Scenario | Mock operations and actual UI |
| --- | --- |
| `S6-ubus-error-validate` | After the rejected validate, save is sent, 1 write, "Saved", 0 notices. |
| `S6-ubus-error-save` | 0 writes; the model disk has `saved.s6.test`, the editor has `draft.s6.test`, but "Saved", 0 notices. The editor revision is also assigned the missing value. |
| `S6-ubus-save-apply-no-false-success` | 0 writes; one service(restart) RPC sent; "Saved and applied", 0 notices. No real restart was performed on the VM. |
| `S6-ubus-error-service` | No error reported; service not executed by the model; the UI simply requests status. |
| `S6-ubus-error-nodes` | 0 cards instead of the previous 3; both selected references stay missing. No other ID is substituted automatically. The cleanup cause is `result.nodes || []` in `overview.js:583` (local stand artifact). |
| `S6-ubus-error-probe` | PASS: the enabled reject rejects code 9; both badges error, pending/disabled released. |

The five negative scenarios stayed FAIL. Control `S6-rpc-adapter-ubus-control` PASS: the same rpc.js and the same ubus response yield `{}` with default reject and a Promise rejection with `reject:true`. The check was repeated with the second local SDK's rpc.js: `rpc-sdk-primary.log` (local stand artifact), 2 PASS / 5 FAIL with the same results. This confirms the view/rpc-contract behavior but asserts nothing about the frequency of such an error or its occurrence on any VM.

Primary evidence: `save` (local stand artifact), `save+apply` (local stand artifact), `adapter control` (local stand artifact). Proposal without application: `proposed-S6-D02-ubus-reject.patch` (local stand artifact), `reject:true` for all declared RPCs. This is a proposal for this ubus path, not a verified guard against any semantically incomplete success frame.

**S6-A01 — P3, low: ARIA tablist does not implement arrow-key navigation.**

`overview.js:890` (local stand artifact) creates tablist/tab and click; there is no keydown handler. In `S6-keyboard-tab-arrow-pattern`, focus is set on "Rules" and a genuine ArrowRight is sent. Per the ARIA tabs keyboard pattern the expectation is focus on "Subscriptions"; in fact focus stays on "Rules". This is a reproducible accessibility shortcoming, not proof that keyboard operation is fully impossible: Tab/Enter PASS. The Home/End checks in this scenario were not reached due to the first assertion; screen reader operation was not tested. The product contract does not separately promise an ARIA keymap, so A01 is not declared a standalone release blocker.

Evidence: `JSON` (local stand artifact). Separate proposal without application: `proposed-S6-A01-tab-keyboard.patch` (local stand artifact), ArrowLeft/Right/Home/End and roving tabindex.

**Reproduction.** Working directory — the Nadkop root. Dependencies were neither installed nor changed. Playwright comes from `/tmp/nadkop-ui-r18/node_modules/playwright/index.mjs`, Chromium from `/run/current-system/sw/bin/chromium`. The expected exit 1 on reproducers means a preserved strict assertion on the product defect.

```bash
# All 84 unique scenarios, S6 only, no shared tests.
S6_ARTIFACT_DIR=build/agent-audit/S6/reproduce-all \
  nice -n 10 timeout 180s node scripts/test-s6-luci-resilience.mjs

# S6-D01: five independent paths of one race.
S6_CASE='^S6-(apply-pending-.*|poll-return-during-save-lock|geodata-return-during-save-lock|title-double-click-during-save)$' \
  S6_ARTIFACT_DIR=build/agent-audit/S6/reproduce-dynamic-lock \
  node scripts/test-s6-luci-resilience.mjs

# S6-D02: ubus codes and the genuine rpc.js control.
S6_CASE='^S6-(rpc-adapter-ubus-control|ubus-.*)$' \
  S6_ARTIFACT_DIR=build/agent-audit/S6/reproduce-ubus \
  node scripts/test-s6-luci-resilience.mjs

# Independent repeat with the second local SDK's rpc.js.
S6_LUCI_RPC=build/openwrt-vm/sdk/feeds/luci/modules/luci-base/htdocs/luci-static/resources/rpc.js \
  S6_CASE='^S6-(rpc-adapter-ubus-control|ubus-.*)$' \
  S6_ARTIFACT_DIR=build/agent-audit/S6/reproduce-second-rpc \
  node scripts/test-s6-luci-resilience.mjs

# Themes/geometry/focus only, latest 12 PASS.
S6_CASE='^S6-theme-' S6_ARTIFACT_DIR=build/agent-audit/S6/reproduce-themes \
  node scripts/test-s6-luci-resilience.mjs

node --check scripts/test-s6-luci-resilience.mjs
node --check scripts/fixtures/s6-luci-harness.mjs
```

The main measurement was actually run with `nice -n 10 /run/current-system/sw/bin/time -v -o build/agent-audit/S6/final.resources.txt timeout 180s node scripts/test-s6-luci-resilience.mjs`, `S6_ARTIFACT_DIR=build/agent-audit/S6/final`, stdout/stderr — `build/agent-audit/S6/final.log`. GNU time: 1:01.43 wall, 23.93 s user + 4.54 s system, maximum RSS 287 680 KiB (≈281 MiB), swap 0. This is a GNU time reading for the command tree, not continuous measurement of total memory of all processes and not a router resource estimate.

**Logs and snapshots.** One JSON per scenario with assertion/stack, RPC parameters and responses, draft, notices, disabled/busy, focus, badge, mock backend, and revision. One PNG per FAIL. In `final/` there are 11 failure PNGs and 12 themed PNGs; the current 12 themed PNGs are also in `theme-final/`. The S6 folder after the checks is ≈21 MiB, including historical runs. `environment.json` (local stand artifact) contains versions and the rpc.js SHA256. `final.resources.txt` (local stand artifact) is the resource measurement.

The historical `matrix-01` is not the final result: five service-fails there were an S6 locator bug that looked for hidden Rules via getByRole after navigating to Diagnostics. Only the S6 helper was fixed; Rules is returned before the disabled check; all five scenarios are PASS in final. For geodata, waiting was added specifically for the new catalogue, to verify the late recreated controls rather than the original stub fields. For themes, the adapter was extended with the real Bootstrap `data-darkmode` from header.ut; the new 12 PASS are in theme-final. No production assertion was deleted or turned into an expected success.

**Confirmation limits.**

- These are browser view checks with local mock HTTP/JSON-RPC. No real rpcd, ubus daemon, Xray, VPN, subscriptions, LAN/TPROXY/DNS, service/procd, or backend disk I/O was executed. Partial commits and revision serialization in these scenarios are a model with documented state, not a re-check of the real Go backend.
- The shared `device-widgets.mjs` fixture and runners were read but not changed. The in-house DeviceSelect is a plain HTML select; the genuine LuCI dropdown, its keyboard, inert, and form behavior on the VM are not confirmed here. LuCI request batching/session-expiry UI, HTTP login/ACL, and the full LuCI shell were also not run.
- Chromium and sizes 1360×950 / 390×950 were tested; no Firefox/WebKit, physical phone, touch/on-screen keyboard, screen reader, color contrast, or manual visual acceptance. 390 px is a viewport emulation. Light/dark preferences and the Bootstrap palette were verified separately; the Material/OpenWrt2020 palette was not reworked.
- Probe is a mock of results and errors. Network node-latency measurements and real core multithreading were not tested. Busy-retry was verified on a single transient failure; exhausting all eight "Probe already running" retries was not run.
- 24-hour/multi-hour resource leaks and thousands of repetitions were not tested. Loss of a saved-but-unsaved draft on page reload is not declared a new bug: persistent draft storage is not promised by the contract.
- B2/B3 VMs, user login, and real HWIDs/credentials were not opened. Host firewall/routes/sysctl/clock, other agents' processes and disks were unchanged. No PRs or messages to other people were created.

**SHA256 before/after.** 327/327 files from `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json` verified, mismatches 0 before and 0 after; all SHA256 dictionaries fully equal. S6 start: 2026-11-02T17:11:39.680241+00:00; final reconciliation: 2026-11-02T17:51:54.334992+00:00. Evidence: `frozen-before.json` (local stand artifact), `frozen-after.json` (local stand artifact), plus separate before/after inside each run.

Manifest SHA256: `09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.

| File | SHA256, identical before/after |
| --- | --- |
| overview.js | `a4dbad7fc24f47580aadd8d4058da6c519afae2db7c3e60a8d7c09d213081b86` |
| picker.js | `b30b605ad778e343d2ba4f85e24d2953bd7769eb0910bfbdb42bb8db26a5dc88` |
| picker.css | `e36bc93b4b325222e77b45c3cc1bc9596e6ead389669267a6e10fd497b6de52f` |
| go.mod | `f942b6571b0d9fbf59b3fd065895dad55d292331f27baa7cb3577a4a07ce039d` |

SDKs were not modified. The read `rpc.js` of SDK 24.10.8: `ebdb47b45dcb6de4d3f209f034dcc408c36cb429b717424fe84e22274cf3dea4`; of the second SDK: `7d1c23fac98fc1c8a86d1c75106eb39bd045eaeb1e3f1d10085abf2b87a9955f`. Patch files are saved separately and were not applied to protected code.
