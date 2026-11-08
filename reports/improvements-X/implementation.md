# X1 — Implementing remarks after Terra critique

Implemented concrete fixes **X1-I01–I05** from [final](final.md) taking into account the [critique](terra-critique.md). Research proposals X2 are not declared implemented: their conditions and exclusions from the final document are preserved. Date: 2026-11-03, Europe/Moscow.

## Changes

| ID | Result |
| --- | --- |
| X1-I01 | After keyboard move, focus stays on the moved rule/server button. At the boundary, the available reverse direction is selected. Pin returns focus to the same card; if the card disappeared from the filter — to a neighboring one, if the list is empty — to search. The pinned toggle also preserves focus. Restoration is synchronous, only if the source element was focused; background responses do not trigger it, scroll position is preserved. |
| X1-I02 | A validate error leaves the draft. A save error/lost response shows "Save unconfirmed", keeps the previous revision, and does not trigger restart. A save response without revision also does not count as confirmation. After a confirmed save and a service error, "Saved, apply unconfirmed" is shown. A flag for later edits is stored separately. A subsequent status error does not cancel an already confirmed apply. No automatic retry/rollback. |
| X1-I03 | Accessible names of select and pin buttons include the displayed source name. For server-name/source-title matches, a number from the full list is added: filtering does not change it. An assigned server gets the text "already added to the selected rule"; repeated selection stays idempotent. No toggle semantics for selection were added. URL, outbound, and credentials are not used for the new context. |
| X1-I04 | Next to unavailable Zapret there is a compact native details/summary help. The reason is available via aria-describedby, Enter, and touch; the action stays disabled. The reason updates when compatibility changes, including a late geodata response. When the reason disappears, the focused help stays until focus leaves. Expanded text is limited to the width of the actions block. |
| X1-I05 | Global `-h`/`--help`, `help`, `help COMMAND`, and help for each command, including all network actions and internal rpc. Successful exit 0 before reading files, stdin, lock, and any operations. Ordinary invalid commands, flags, and extra arguments remain errors. Existing flags are preserved, including the accepted but unused `-output` for validate/nodes. Help reflects the real side effects of apply/updater and recovery during network check. |

Production changes: [CLI dispatch](../../cmd/nadkop/main.go), [help](../../cmd/nadkop/help.go), [LuCI view](../../openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js), [cards](../../openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/picker.js), [styles](../../openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/picker.css).

## Verification

| Check | Result / evidence |
| --- | --- |
| New X1 scenarios | **26/26 PASS**: lost response after actual write/action, malformed save, revision, late draft, repeated move and scroll, pin/empty filter, matching names, accessibility tree, reason update, three themes, light/dark, and touch. `Result` (local stand artifact), [runner](../../scripts/test-improvements-x1.mjs). |
| Reproduction on the pristine copy | **3/3 expected FAIL**: focus loss, indistinguishable names, and false draft status after committed save with lost response. `Result` (local stand artifact). |
| Full S6 | **84/84 PASS**, matching hashes of six input files before/after the run. `Result` (local stand artifact), `log` (local stand artifact). |
| Additional C4 regressions | **13/13 PASS**. `Result` (local stand artifact), `log` (local stand artifact). Only the expected status texts were updated; the busy, revision, and draft checks are preserved. Manual blur in the test is performed by a single DOM operation, with no race between count and dispatchEvent. |
| Picker / product UI | `scripts/test-picker.mjs` and `scripts/test-product-ui.mjs` — PASS in Chromium. |
| CLI | `go test ./cmd/nadkop -count=1` — PASS. Additionally 30 help invocations under strace and 6 invalid invocations: **36/36 PASS**. Help performs no config/runtime reads, file writes/creation, network connections, or child command launches. `Result and arguments` (local stand artifact). |
| Go contract with race detector | `go test -race ./... -count=1 -skip '^(TestS3PartialHWIDCannotSilentlyReinitialize\|TestS3RestoredWeakSensitiveModes\|TestS3RestoredForeignOwners)$'` — PASS in all three packages. `Log` (local stand artifact). |
| Unfiltered Go | `go test ./... -count=1` — FAIL only on the three pre-existing policy audits S3-R01/F03/F04 described in the original X final. `Log` (local stand artifact). These tests were not deleted or modified. |
| Static / build | `go vet ./...`, `go build ./cmd/nadkop`, `node --check` of changed JS/MJS — PASS. |

UI launch scenarios:

```sh
node scripts/test-improvements-x1.mjs
C4_OUTPUT=build/blocker-closure/C4/improvements-X-final node scripts/closure_c4_regression.mjs
S6_MANIFEST=build/improvements-X/implementation/s6-manifest.json S6_ARTIFACT_DIR=build/improvements-X/implementation/s6-final node scripts/test-s6-luci-resilience.mjs
NADKOP_PLAYWRIGHT_MODULE=/tmp/nadkop-ui-r18/node_modules/playwright/index.mjs node scripts/test-picker.mjs
NADKOP_PLAYWRIGHT_MODULE=/tmp/nadkop-ui-r18/node_modules/playwright/index.mjs node scripts/test-product-ui.mjs
```

For porting to another machine the runner supports `NADKOP_PLAYWRIGHT_MODULE`, `NADKOP_CHROMIUM`, and the harness supports `S6_LUCI_RPC`/`S6_LUCI_THEME_ROOT`. The path `/tmp/nadkop-ui-r18` is a locally installed dependency of the current machine.

## Shared folder and output limits

Before the changes, `hashes of 364 files` (local stand artifact) and the pristine copy were saved. The main team confirmed no active edits on the taken section. After the work, six existing files changed and three code/test files were added; the other 358 source files matched. `Final manifest` (local stand artifact), `diff files` (local stand artifact). Shared README, acceptance, and blocker registries were not edited. C1–C3 backend, storage/network/geodata, and the results of their previous acceptance are not re-declared by this verification.

UI was verified in a real Chromium DOM and accessibility tree with the original LuCI SDK rpc.js. E(), DeviceSelect, and the HTTP/RPC environment are provided by the existing isolated harness; this is not a full LuCI session on the router. Manual screen reader, physical touch, new APK/IPK, real Xray/procd, power loss, and the device were not verified. Dark emulation for Material/OpenWrt2020 does not mean these themes have a separate dark palette.

X2-01/02/03/04 remain profiling/modeling tasks; X2-06 requires verification of diagnostics by generation; X2-08 requires defining the crash guarantee and the recovery method. In this work such measurements and phase syscall injections were not performed. Save errors now honestly display the indeterminate outcome; there is no new guarantee of overall config/nodes atomicity. X2-05/07/09 remain excluded per the final critique. These items are not part of the implemented fixes list.
