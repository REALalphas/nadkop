S1 — RPC and subscription-loading security audit, core 0.3.0-r4 / LuCI 0.3.0-r8.

Verification is complete on the pinned implementation. Two persistence defects are confirmed (S1-F1 and S1-F2, P2/medium); both require access to Nadkop write methods. No bypass of the declared read ACL, no identifier transfer to another HTTPS origin, and no reflection of saved URL/token/auth/HWID into status were found in the tested scenarios. This is not confirmation of the actual authorization of a running rpcd: no VM or rpcd was used in this audit.

Read: `reports/parallel-tasks.md`, `reports/blockers.md`, `docs/product-contract.md`, all files from the S1 assignment, plus the related catalogue, compile, probe transport, and diagnostics implementations. No production files, SDKs, recipes, README, shared runners/reports, or third-party tests were modified. No SSH, VM, firewall/routes/sysctl/clock changes, subagents, or external network requests. Data and credentials in fixtures are synthetic.

**Reproducible run and results.**

```sh
python3 scripts/test-s1-security.py
python3 scripts/test-s1-security.py --race
patch --dry-run --batch --forward -p1 --input=build/agent-audit/S1/s1-proposed-fixes.patch
```

The runner runs only:

```sh
go test -p 1 -parallel 1 -count 1 -timeout 60s -json -run '^TestS1' ./internal/core ./internal/router
go test -p 1 -parallel 1 -count 1 -timeout 60s -json -race -run '^TestS1' ./internal/core ./internal/router
```

Runner environment: `GOMAXPROCS=2`, `GOMEMLIMIT=384MiB`, `GOFLAGS=-mod=readonly`, `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`; Go 1.26.10 linux/amd64. Packages run sequentially, no `t.Parallel`. Own temp files live in `build/agent-audit/S1/tmp`; HTTP/TLS/SOCKS listen only on free loopback ports. One child test process is used per each of the five TLS scenarios; the fixture certificate is trusted only via that process's SSL_CERT_FILE. Global HTTP transports, trust store, and real identity files are unchanged. GOMEMLIMIT is a soft Go heap limit, not an RSS measurement and not a hard limit on the whole process tree.

Both latest runs: exit 0; 15 top-level tests PASS; 46 events PASS including nested scenarios; 0 FAIL. One SKIP — `TestS1RouterTLSChild` when run directly without the fixture environment; this helper was indeed executed five times by the parent test. The normal run took 4.035 s, race — 20.860 s; core/router packages 0.214/1.261 s and 2.272/15.165 s respectively. The race detector reported no data races. These are observed-behavior tests: `TestS1Repro*` intentionally pass when the defect is reproduced; a green result does not mean no defects were found.

Logs: `build/agent-audit/S1/go-test-normal.jsonl`, `go-test-normal.stderr.log`, `go-test-race.jsonl`, `go-test-race.stderr.log`; summaries `summary-normal.json`, `summary-race.json` in the same directory. `initial-*` preserve the first, narrower successful run before SOCKS and nodes-repro were added, and are not the final metric. The full project-wide test run was not executed.

**What was actually verified.**

- ACL: `openwrt/luci-app-nadkop/root/usr/share/rpcd/acl.d/luci-app-nadkop.json` was mapped against all 13 `signature` methods. Only `status` is in read; the other 12, including get/import/nodes/check/identity/probe/geodata, require write per this JSON; no wildcard. `get` and `identity` on the synthetic backend positively return saved secrets/IDs, which explains the need for the write ACL.
- RPC envelope: 11 negative scenarios — non-object, empty input, multiple JSON values, unknown top-level/nested field, wrong types, truncated JSON. Exactly 4,194,304 bytes accepted; 4,194,305 rejected, the reader consumes at most limit+1. Unknown method and shell metacharacters in service action are rejected; an invalid action never reaches lock/command. `ubus_rpc_session` is accepted as a string, a wrong type is rejected. Direct launch of the executable plugin does not verify the session itself — authorization is delegated to rpcd, which by itself is not an ACL bypass. Duplicate keys are accepted with the last value; no separate boundary between different parsers yielding an exploit was shown.
- Secrets: synthetic password, URL token, Authorization, HWID, and raw output of a failing command do not appear in status or the verified diagnostics/errors. Canary-bearing config/nodes/xray/health secrets and extra service output fields were fed into status; the result contains only stock information. Source and Xray diagnostics were verified with a local file and a stub commandRunner. Invalid on-disk JSON is not reflected by the get method. No real Xray/nft/nfqws was launched in this check.
- HTTPS: two successful exchanges with the trusted local TLS server (direct and same-origin redirect) preserved the synthetic Authorization/X-Api-Key/X-HWID. Redirects to another port, another hostname, a scheme-relative foreign origin, HTTP downgrade, and a loop were rejected; the foreign TLS endpoint and HTTP endpoint received exactly 0 requests. Untrusted CA and wrong hostname were rejected separately. Trust is configured on individual transports, without InsecureSkipVerify.
- Subscription response: a raw body of exactly 4 MiB accepted, +1 byte rejected. A 4,290-byte gzip decompressing to 4,194,305 bytes rejected after decompression. HTTP 403, HWID-limit header, malformed settings, and links/default-JSON mismatch return errors without canaries. Explicit format=links accepted; an in-body HWID requirement with HWID disabled detected. A request to the local hanging handler was cancelled in about 70.48 ms. This is a context cancellation check, not a wait for the stock 30 s timeout.
- Headers: 12 maps with case conflicts, CRLF/NUL/DEL, reserved Host/Content-Length/Transfer-Encoding, bad name, value length >8192 / name >256, or count >64 were rejected by the loader and ReadConfig. Manual case-insensitive X-HWID/device OS/User-Agent keep priority.
- Files: positive import of an explicit absolute regular .json, and 9 negative path/type cases — relative, `..`, repeated slash, NUL/newline, non-.json, trailing symlink, FIFO, directory. FIFO rejected without blocking. A file >4 MiB rejected. An allowed absolute path through a parent-directory symlink works: this is a boundary characteristic, not a claimed vulnerability. API policy lets the administrator explicitly pick a local .json rather than restricting them to one directory.
- HWID: a dedicated temporary identity was created; a repeat call preserved the ID and mode 0600. Symlinks of the ID itself and .lock rejected; a corrupted ID returns a generic error without the value and is not regenerated. Real user HWIDs were neither read nor printed.
- Parsing: five positive representations — JSON profile, BOM+JSON, base64 JSON, URI, and URL-safe base64 URI. 16 malformed JSON/base64/URI/tag/dependency scenarios rejected without synthetic credentials; among them duplicate query, bad ports/percent escapes, unknown dependency, duplicate/slash/newline tag, and JSON nesting excess. The test is not fuzzing and not a check of all URI extensions.
- Graph: plain and nested XHTTP dialerProxy correctly receive the source namespace; UnprefixNodes strips it when saving local. Provider inbounds/DNS do not replace local listeners/DNS in the generated JSON. The a→b→a cycle is allowed by the parser but rejected by SyncServers with `cyclic server connection chain`, so it is not published in the catalogue. The syntactic parser is not separately claimed as protection against all cycles.
- Probe: three positive modes with four refs; 12 bad refs/mode/URL variants rejected. Four local SOCKS5 negotiation/auth exchanges and three HTTP requests through this fixture were actually executed: success, HTTP 302 without follow, SOCKS auth refusal, and malformed HTTP response. SOCKS password/token/body canaries are not reflected in ProbeResult; the redirect target got 0 requests. The fixture itself completes the HTTP exchange; no external address is dialed. This is a check of the core.Probe HTTP/SOCKS client, not a full VPN/Xray client/server exchange and not a router.probe run with Xray.
- Revisions and file permissions: stale save/addsource and unsafe file/URL imports do not change config/nodes/revision. Positive local addsource, idempotent retry, and save work; written config/nodes have mode 0600.

**S1-F1 — P2 / medium: addsource publishes a source that fails its own ReadConfig.**

Conditions: access to the `addsource` write method, current revision, the local trusted HTTPS fixture returns valid Xray JSON. Three independent inputs reproduced: a 4,129-byte URL; `options.title` of 81 characters; `options.title` with a newline. Each request fits within the RPC limit; addsource returns success, the new config.json is saved, after which ReadConfig and backend.load reject it. The positive control of a normal URL is added and reloaded successfully; malformed remote JSON is rejected without changing config.

Cause: `internal/router/source_import.go:37` does not forbid Title in Options although it states that only headers/format/HWID are allowed; `:47` does not check the 4096 URL limit; `:80`–`:89` sync the source and write config without full re-validation. Meanwhile `internal/core/config.go:122` forbids a long/control title, `:134` — a long URL. On subsequent read, `internal/router/rpc.go:386`–`:388` return `Invalid saved configuration`.

Race-run measurements: long-url input=4453, saved=4569; long-title input=450, saved=574; control-title input=381, saved=505 bytes. All addsourceOK=true, savedConfigReadable=false, backendLoadOK=false. JSON evidence: `build/agent-audit/S1/s1-addsource-long-url.json`, `s1-addsource-long-title.json`, `s1-addsource-control-title.json`. Controls: `s1-addsource-valid.json`, `s1-addsource-malformed.json`.

Impact: a configuration is persisted that blocks further nodes/check/start/restart/generation until the source is fixed/removed. addsource with such a saved config stops working too. This is a reproducible integrity/availability defect under allowed write input, not privilege escalation or proven read-only user access. The already applied generation was untouched in this test; termination of already working connections is not claimed. The source can be corrected via the allowed get/save or locally; mandatory data loss or mandatory SSH is not proven.

The `TestS1ReproAddSourcePublishesInvalidConfig` test exercises this path on a temporary backend with the same production functions. Child processes use the original nil transport, so this is a real HTTPS fetch with CA/hostname verification. No call to a running ubus/rpcd on OpenWrt was made.

**S1-F2 — P2 / medium: save checks input size but not serialized config/nodes size.**

Conditions: the save write method, correct revision, valid JSON request <4 MiB. ReadConfig checks the compact input; then `internal/router/rpc.go:304`–`:309` perform MarshalIndent and atomicWrite without checking the resulting size. `atomicWrite` additionally appends a newline (`internal/router/rpc.go:555`). Subsequent ReadConfig is limited to 4 MiB (`internal/core/config.go:68`–`:70`), the local subscription loader — to the same limit (`internal/core/subscription.go:104`–`:109`).

Two independent reproductions:

- Config: one block rule, 16 384 unique valid 247-character domains; compact config=4 096 193, full request=4 096 408, written config=4 243 741 bytes. saveOK=true; repeat ReadConfig: `configuration exceeds 4 MiB or cannot be read`; backend.load: `Invalid saved configuration`. Evidence `build/agent-audit/S1/s1-save-serialization.json`.
- Nodes: one trojan outbound with 8192 synthetic endpoints; compact nodes=3 842 109, request=3 842 659 in the normal run / 3 842 660 in race, written nodes.json=4 284 523 bytes. The 1-byte request difference is due to the temp path length; the write result is identical. Config itself stays valid, but backend.load returns `Source S1Local: subscription exceeds 4 MiB`. Evidence `build/agent-audit/S1/s1-save-nodes-serialization.json`. Xray preflight of these 8192 endpoints was not run and their real operability is not claimed.

Impact: a save within the input limits creates local files that the next apply/refresh cannot read. The oversized file remains after a successful response. This is a write-size-check error, not proven RAM/disk shortage and not a privilege bypass. Behavior on real flash/VM and preservation of active connections were not measured.

The `TestS1ReproSaveSerializedConfigExceedsReadLimit` and `TestS1ReproSaveSerializedNodesExceedReadLimit` tests use a temp backend; the full serialized request size is checked separately, but public Run/save with fixed /etc paths is not invoked. Envelope Run was verified by a separate test. The project limits of 1024 rules / 16 sources and 4 MiB were not exceeded on input in these reproductions.

**Proposed change, not applied.**

`build/agent-audit/S1/s1-proposed-fixes.patch` contains only a unified diff for source_import.go and rpc.go. Proposal: do not accept title as an addsource option, check URL length before fetching; after source discovery validate the fully serialized config before SyncSourceServers/write; before any save write check the sizes of both MarshalIndent results accounting for the newline. Serialization errors are also proposed to be handled explicitly. The patch is a proposal, not a verified fix. It was not applied to the sources, no separate checkout/overlay was created, and tests were not run on a fixed implementation. `patch --dry-run` confirmed the context of both files; log `s1-patch-dry-run.log`. Overall config/nodes/catalogue atomicity is not claimed solved by this patch.

**Unverified items and residual risks.**

Actual login to LuCI/ubus with a read-only/unauthorized session and inheritance of third-party ACLs require an isolated rpcd/OpenWrt stand. A statically correct ACL JSON does not replace this test. A direct executable plugin trusts the launching process, so an administrator invoking the binary is not considered a demonstration of session bypass.

Not tested: MITM outside fixtures, DNS rebinding, proxies from the user environment, external provider behavior, all HTTP headers/compression variants, prolonged resource exhaustion, real flash/media, fuzzing, and engine-specific options. Provider outbound options are intentionally preserved; final compatibility depends on Xray preflight. Loader timeout blocking was verified here via context cancellation, not by a 12/24/30-second wait.

For filesystem, exactly the stated types/paths were verified. Not performed: attacks with concurrent replacement of parent directories/hardlinks, FIFO in hwid/catalogue/lock itself, and lock exhaustion. Some of these paths use blocking open/flock; without a demonstrated privilege boundary this is an observation/area for future testing, not a confirmed vulnerability. get reads fixed configuration files as admin, and explicit local import allows absolute .json outside /etc/nadkop; these capabilities are not presented as a path traversal exploit. Other people's VMs and processes were untouched.

**Immutability control.**

Before the first test, all 327 entries of `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json` were reconciled: 0 mismatches. Before/after both final runs — 327/327 matches, 0 mismatches. The final standalone snapshot `build/agent-audit/S1/sha256-after.json` is also compared against the initial `sha256-before.json`. SHA256 of the source manifest itself: `09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.

Only the following were created: `internal/core/s1_security_test.go`, `internal/router/s1_security_test.go`, `scripts/test-s1-security.py`, this report `reports/agent-audit/S1-security.md`, and the contents of `build/agent-audit/S1/`. Test files were published via atomic rename. SHA256 index of own results: `build/agent-audit/S1/s1-artifacts-sha256.json`.
