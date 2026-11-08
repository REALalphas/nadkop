# Zapret auto-tuner

Implementation of UI-01 from `reports/ui-backlog-2026-11-03.md`: backend 0.3.0-r6,
LuCI 0.3.0-r11. Classic nfqws from Nadkop's pinned package.

Task state is polled every two seconds in the background. Polling neither blocks nor
resets focus in the "Probe sites" and "Probe protocols" fields, and results
redraw only when the task changes, so open sections and scroll
position survive. The cancel and state-refresh buttons stay available
during background polling.

In LuCI open **Settings → Zapret**, pick the real WAN device,
enter 1–4 domains, and pick HTTPS or HTTP + HTTPS. URLs, ports, IPs, query
parameters, and local names are not accepted. IDNs go in punycode. Tuning
uses the draft WAN but never saves it and never reads subscriptions.
A current revision of the saved configuration is required before starting.

A run takes up to six minutes. Each HTTP request gets four seconds,
each domain resolution four seconds. The IPv4 address is pinned once
for the whole task. Non-public, local, multicast, and FakeDNS addresses are forbidden,
including mixed DNS answers with public and non-public addresses.

First, strategy-less requests run. If every destination is reachable,
the verdict is "reachable without a strategy", with no result to apply. Then
multisplit, multidisorder, and their fake/badseq variants are probed. Selection needs
two successful requests per site/protocol, confirmed passage
through Nadkop's own nfqws queue, and failed strategy-less control requests
between attempts and after them. Destinations reachable before the run must stay
reachable too. Certificate and HTTP errors in the baseline comparison yield an indeterminate
result; on their own they do not prove DPI blocking.

Requests are GET `/`, no proxy from the environment, no redirect following, no cookies
or credentials. Success means HTTP 200–399 plus reading the start of the response. HTTPS
validates the certificate chain and site name against the regular system CAs, TLS 1.2+.
HTTP proves nothing about page authenticity, and a 3xx response does not prove the
final address is reachable. Page bodies, Location headers, and raw external-process
errors are never stored.

No success percentages are modelled: progress shows the count of actually
finished probes and the upper bound of their number. A result holds domain,
protocol, stage, attempt, outcome, HTTP status, timing, and queue confirmation.
After a page reload LuCI picks up the latest task from the router. The
"Refresh tuner state" button stays available on lost connectivity. A run can be cancelled even
after losing the launch response, before the client has the task ID.

A successful run with confirmed cleanup shows a carry-over button for the
proven strategy. It asks to confirm replacing the manual arguments
**in the draft**. Until then the manual field is kept. The backend revision and WAN check
rejects a result after saved settings or WAN changed;
a result also expires after one hour. A draft edit during the carry-over RPC
must not be clobbered by the response. After carry-over, run the regular
**Save and apply** action. It saves the draft's other pending changes too,
runs the normal preflight, and applies the service generation.

## Isolation and recovery

Tuning never calls service start/stop/restart, never changes `current`, Xray,
the user nfqws, DNS/UCI, or config files. A separate nfqws
runs on queue 201 with UID/GID 2147483647. Its arguments come only from the
built-in set and first pass `--dry-run`; a manual strategy never executes
disguised as a candidate. Before launch, WAN existence, disabled
flow offloading, and a free queue are verified. Queue ownership is checked via the nfqws
netlink port ID before and after every probe; processed-packet and loss counters,
process liveness, and temp-table ownership all count.

The `inet nadkop_tune_<random ID>` table holds only postrouting for the
exact socket mark 0x28000000, the given WAN, the pinned destination addresses,
and TCP 80/443. Control sockets use 0x28000001. Both marks include the
production bypass 0x20000000, so the live queue 200 never processes them.
Normal connections never get these marks. The table is created atomically with
`create` and a unique owner comment. A foreign same-named table is never
deleted. Every temporary process and rule carries its own ID.

Worker and guard are detached from rpcd and the terminal. Guard starts before temporary
rules are installed and validates PID together with process start time. Guard
readiness is tied to its live PID, and finalization is serialized with worker. Cancel
interrupts the network context; guard also sends TERM, then KILL on hang
or deadline. nfqws additionally gets parent-death SIGKILL.
Worker watches guard liveness, stops if it is lost, cleans up itself, and starts
a replacement guard. Guard cleans up after a SIGKILLed worker even if the
page is closed. An nft error never skips the independent nfqws stop.

Guard publishes the finished state only after the temporary
table and process are removed/verified absent. `recovery_failed` blocks the next run and
strategy selection. A repeat cancel or state request triggers orphan-task recovery.
Killing both guard processes at once forcefully needs such a request; a reboot clears runtime and nftables.
Never delete the journal by hand while the temporary table or nfqws exists.

The journal lives in `/var/run/nadkop/zapret-tune/`: directories 0700, JSON 0600,
in RAM, the latest result plus up to three previous ones. User domains are visible
in the authorized RPC but never written to syslog or localStorage. PIDs, internal
addresses, and service fields are never returned to LuCI. The `zaprettune` method is
available only to the app's write ACL.

## Verification

```sh
go test ./internal/router ./cmd/nadkop
go test -race ./internal/router -run TestTune -count=1
go vet ./...
node scripts/test-zapret-tuner-ui.mjs
```

The Chromium test uses the existing LuCI RPC harness, the real `rpc.js`, and
synthetic backend answers: progress, cancel, relaunch, recovery
after page reload, indeterminate verdicts, RPC drops, explicit draft
replacement, and separate applying. Bootstrap, Material, and OpenWrt2020 are covered,
light/dark themes, 390 and 1360 px. A separate scenario proves that
background polling neither disables the domains field nor steals its focus. Set
`NADKOP_PLAYWRIGHT_MODULE` and
`NADKOP_CHROMIUM` if dependencies live elsewhere.

On a **dedicated VM**, with the binary and LuCI from the current build:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/nadkop-tuner-http scripts/fixtures/tuner-http.go
scp -O -i ~/.ssh/nadkop_vm_key build/nadkop-tuner-http scripts/test-zapret-tuner-openwrt.sh root@<vm-mgmt-ip>:/tmp/
ssh -i ~/.ssh/nadkop_vm_key root@<vm-mgmt-ip> \
  'NADKOP_TUNER_VM_TEST=1 sh /tmp/test-zapret-tuner-openwrt.sh'
```

The script creates two separate network namespaces, a local HTTP/TLS server, and
an isolated network fault for the control mark only. It checks real
nfqws/NFQUEUE, cancel, SIGKILL of worker/guard/nfqws, and deadline; it never stops the
working service and compares config, HWID, UCI, generation, PID, and the production
table before/after. The synthetic public IP exists only inside the namespace.
This test proves isolation and failure handling, not real-DPI bypass effectiveness.

Limits: four built-in strategies, IPv4 HTTP/HTTPS from the router, one IPv4
per domain, no QUIC/HTTP3, IPv6, custom-argument brute force, or LAN probes.
Networks change; even interleaved probes prove no causality for any
provider. HTTP needs an extra user-side resource check. The three known restore studies
still fail in a full `go test ./...`;
they must neither be hidden nor declared fixed by this change.
