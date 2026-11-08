# UI-01: Zapret auto-tuning — implementation and verification

Verification completed: 2026-11-04, Europe/Moscow. The date in the file name matches the story statement.

A separate background tuner was implemented in LuCI and backend: `zaprettune` with
`start/status/cancel/select` actions, worker/guard, four built-in strategies,
real HTTP/HTTPS probes with progress and recovery after cancellation/failures.
Backend 0.3.0-r6, LuCI 0.3.0-r10. The working directory is shared with X2; others'
current fixes are preserved, `loadWith`, saved-pair and runtime diagnostics were not rolled back.
SDK packages include the shared source snapshot, including X2 changes.

The result is not applied automatically. Two successful passes over all
sites/protocols are required, lossless counters of the tuner's own NFQUEUE 201, a live nfqws,
queue and table owner verification, stable alternating control probes.
Initially unreachable pairs must stay unreachable in all controls,
initially reachable ones — reachable. All directly reachable sites yield no found
strategy. HTTP/certificate errors and a changing control give an inconclusive
outcome. On any recovery error, selection and the next run are blocked.
A repeat successful cleanup permits a new tuning run but does not revive the previous winner.

In LuCI: **Settings → Zapret → Strategy auto-tuning**. The verified strategy is
carried into the draft only after confirmation; then **Save and apply** is pressed
separately. Manual arguments are not replaced during tuning.
The revision, WAN, result age and draft changes during RPC are verified.
Response loss, empty/corrupt JSON or an unknown state do not count as
completion confirmation. Cancellation is possible against the last active task even
after losing the start response. The `restored=true` claim is published only
after resource verification.

## Changed files

- `internal/router/zapret_tune.go` — contract, storage, validation, start,
  state/cancel/select, bounded history.
- `internal/router/zapret_tune_runtime.go` — worker/guard, recovery,
  process identity, nft ownership, DNS pinning, HTTP/TLS, queues.
- `internal/router/zapret_tune_search.go` — algorithm and real probe execution.
- `internal/router/zapret_tune_test.go` — validation, comparison, cancellation,
  errors, incomplete-recovery lockout, repeat cleanup, selection.
- `internal/router/rpc.go` — only signature/request/switch extensions for the API;
  parallel X2 changes left in place.
- `cmd/nadkop/main.go`, `cmd/nadkop/main_test.go` — internal
  `zapret-worker`/`zapret-guard` commands and argument boundaries.
- `openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js`
  — new tuning block and RPC adapter; the `runtimeChecks` renderer belongs to X2.
- `openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/picker.css`
  — adapting the new block to screen width and themes.
- `openwrt/luci-app-nadkop/root/usr/share/rpcd/acl.d/luci-app-nadkop.json`
  — only write ACL for tuning.
- `openwrt/{nadkop,luci-app-nadkop}/Makefile` — releases 6 and 10.
- `scripts/test-zapret-tuner-ui.mjs`, `scripts/test-zapret-tuner-openwrt.sh`,
  `scripts/fixtures/tuner-http.go` — new checks and an isolated server.
- `scripts/fixtures/s6-luci-harness.mjs`, `scripts/test-{product-ui,picker,subscriptions}.mjs`
  — new status RPC support in test adapters.
- `README.md`, `docs/zapret-tuner.md`, this report — usage and limitations.

The service init was unchanged: tuning is isolated from the procd generation and does not require
an Xray/user-nfqws restart. Tuning writes no `config.json`, `nodes.json`, catalogue,
HWID, saved-pair marker, UCI/DNS or `current`.

## Checks

| Check | Outcome and evidence |
| --- | --- |
| `go test -race ./internal/router -run TestTune -count=1` | PASS; `build/zapret-tuner/tuner-race.log` |
| `go test ./cmd/nadkop -count=1` | PASS; `build/zapret-tuner/cli-tests.log` |
| `go test ./internal/router -count=1` | PASS; `build/zapret-tuner/router-tests-final.log` |
| `go vet ./...` | PASS; `build/zapret-tuner/go-vet.log` |
| `go build -o build/nadkop ./cmd/nadkop`, `./build/nadkop validate -config examples/config.json` | PASS |
| `node scripts/test-zapret-tuner-ui.mjs` | 29/29 PASS; `build/zapret-tuner/ui/results.json` |
| Chromium themes | Bootstrap, Material, OpenWrt2020 × light/dark × 390/1360 px; screenshots in `build/zapret-tuner/ui/` |
| `node scripts/test-improvements-x1.mjs` | 26/26 PASS; `build/zapret-tuner/x1-regression.log` |
| Product UI / picker / subscriptions | PASS; corresponding `.log` files in `build/zapret-tuner/` |
| OpenWrt 25.12.6, x86_64, Linux 6.12.112 | 9/9 isolated network scenarios; `build/zapret-tuner/vm-final.log` |
| Real rpcd/ACL | PASS: registration, status, anonymous deny, no secret reflection in errors; `build/zapret-tuner/vm-rpc.log` |
| SDK 25.12 APK and SDK 24.10 IPK | Both `--prepared` builds PASS, SHA256 and byte equality of final JS/CSS/ACL verified |

On the VM only the binary for running internal worker/guard was temporarily swapped,
then restored. For the rpcd check the ACL was temporarily swapped with restoration
and a restart of rpcd only. The working Nadkop service was not stopped.
Network experiments are isolated in two namespaces with a local HTTP/TLS server:

1. Access without a strategy → `unneeded`, no winner.
2. Non-public DNS → rejection before temporary rules.
3. Invalid TLS certificate → `inconclusive`.
4. Real nfqws/NFQUEUE and A/B/A/B/A alternation → confirmation and separate select.
5. Cancellation during a pending request → removal of process/queue/table.
6. SIGKILL worker → guard recovers without status polling.
7. SIGKILL guard → worker stops probes and starts independent cleanup.
8. SIGKILL nfqws → success is not confirmed.
9. Frozen worker + expired deadline → guard finishes and cleans up.

Before/after, SHA256 of config/nodes/catalogue/HWID/UCI, absence/contents of the
saved-pair marker, autostart, `current`, production process PIDs and the
`inet nadkop` table were compared. State matched. The script returns a nonzero code on mismatch.

There were two independent Terra medium audits; the found races, incomplete control-probe
verification and masking of a cleanup error in the script were fixed and covered by
regression. Repeat audits do not claim an unconditional security guarantee.

Build:

```sh
nix-shell -p ncurses unzip wget file pkg-config --run \
  './scripts/build-openwrt.sh build/openwrt-vm/sdk build/packages/zapret-tuner-apk --prepared'
nix-shell -p ncurses unzip wget file pkg-config --run \
  './scripts/build-openwrt.sh build/openwrt-vm/sdk-24.10.8 build/packages/zapret-tuner-ipk --prepared'
```

Packages: `build/packages/zapret-tuner-{apk,ipk}/`, three packages each, `sha256sums`,
`build-info.txt`. The APK binary ran on the VM with real network checks;
the IPK binary ran separately on 25.12. Full APK install with hooks and opkg install
on OpenWrt 24.10 were not performed. The SDK warned about the missing optional
`csstidy`; CSS is packaged and verified to match.

## Limitations

- Only four built-in profiles and IPv4 HTTP/HTTPS from the router; no QUIC/HTTP3,
  IPv6, LAN probes, arbitrary paths or enumeration of user strategies.
- A synthetic network error in the VM confirms the mechanism works and is isolated,
  but not real-provider DPI bypass effectiveness.
- HTTP 200–399 does not prove page authenticity; redirects are not followed.
- After a simultaneous SIGKILL of worker and guard, cleanup requires the next
  status/cancel; outside interference with nft/queues yields rejection, not success.
- Full `go test ./...` is not green: three previous S3-policy studies. During
  parallel X2 work, an old backup fixture and a large-JSON deadline
  were also observed; the final X2 full run confirms only the three previous failures, and
  the final race contract excluding them is PASS. These areas were not fixed
  within the tuner scope.
- There is no git repository in the working folder; no commit was created. Hashes of affected
  files: `build/zapret-tuner/source-sha256.json`.
