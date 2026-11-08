# S5 — Real Local Xray Connections

Date: 2026-11-02. Application under check: core **0.3.0-r4**, LuCI **0.3.0-r8**.
Executor: a dedicated S5 check, without subagents, VMs, or application changes.

**No confirmed Nadkop defects were found in the checked scope.** Two
final targeted runs finished with exit 0: 81 scenarios, 50 successful
HTTP exchanges and 44 UDP replies. Parser acceptance, compilation, Xray preflight, and
the real exchange are separated below. These counts do not mean all combinations
or all Xray versions were checked.

All **327/327** guarded files matched the frozen manifest before and after the work;
**0** changes between verifications. Production, README, recipes, go.mod/go.sum, SDK,
shared reports, and shared runners were unchanged. All test data is synthetic.

## Matrix Basis and Boundaries

`reports/parallel-tasks.md`, `reports/blockers.md`, and
`docs/product-contract.md` were read. Connection formats were cross-checked against
`README.md:74`, `README.md:84`, `README.md:92`, and the engine-version caveat in
`README.md:101`.

- VLESS/Trojan URI: RAW/TCP, WS, gRPC, XHTTP; TLS or REALITY. In engine 26.3.27,
  REALITY supports RAW, gRPC, XHTTP; **REALITY+WS is checked as a rejection**.
- Shadowsocks: SIP002 plain/base64 userinfo and legacy base64 of the whole authority.
  SIP003 plugins and arbitrary URI transport parameters are not claimed.
- Plain VLESS RAW was checked via JSON outbound, because a URI without TLS/REALITY
  is rejected by the importer under the current rules.
- Vision was checked in the allowed RAW+TLS/REALITY. WS+Vision is a separate
  incompatible example, not a row of the positive Cartesian product.
- TLS settings were checked against a dedicated CA and certificate. Setting
  `SSL_CERT_FILE` affects only child Xray processes. `allowInsecure` was not added;
  the global Go HTTP transport and system trust stores were unchanged.

## Engine and Stand

The already prepared local **Xray 26.3.27 OpenWrt, go1.26.9 linux/amd64** was used:

```text
Source: build/openwrt-vm/sdk/staging_dir/target-x86_64_musl/root-x86/usr/bin/xray
SHA256:   4f92ba5e12339213108495ce92478cf3460e450a03495e8cd1459b7f7317ca48
Size:     46272952 bytes
```

Its private copy `build/agent-audit/S5/xray` runs via private
copies of the musl loader and libgcc from the same SDK. The SDK was only read. Network access for
downloading the engine, rebuilds, and system installation were not needed.
`engine-provenance.json` and `engine-preparation.json` contain versions, paths, and
SHA256 of all copies. The hash of the available APK is recorded for provenance; this is **not**
a claim of verifying the official release signature or binary identity against the
unpacked APK.

HTTP-origin, UDP-echo, TLS-target REALITY, and all Xray listeners are bound to
`127.0.0.1` on free ephemeral ports. Destination addresses and proxy endpoints
are loopback too. At most three Xray instances run simultaneously per scenario.
Tests are sequential, without `t.Parallel`; host firewall/routes/sysctl/clock
were untouched. No SSH and no B2/B3 VM calls were made.

Real positive-exchange path:

```text
synthetic subscription → ParseSubscription → SyncServers → Compile
→ xray run -test for client and server
→ client SOCKS5 → Xray transport/security → Xray server → local HTTP/UDP
```

`Compile` sets default=block and a proxy rule by stable nodeRef for
127.0.0.1/32. The generated outbounds/routing/SOCKS listener are used
without substitution; only the loglevel is raised in the test copy of the runtime
JSON for observability. The original Compile result is stored as `*.compiled.json`.

The HTTP check compares status 200 and the full body with the scenario's unique name
and a payload of about 3.8 KiB. The UDP check uses SOCKS5 UDP ASSOCIATE
and compares the returned datagram payload and destination port. Origin counters
check for absence of direct bypass on upstream failure. This is application-level UDP
echo, not a QUIC or transparent-TPROXY test.

Child-process limits: GOMAXPROCS=2, GOMEMLIMIT=128MiB for Xray,
256MiB for the test Go process, nice +10, RLIMIT_NOFILE=512,
RLIMIT_CPU=420 s, RLIMIT_FSIZE=32 MiB, core dumps disabled. Go runs
with `-p 1`. GOMEMLIMIT is a soft Go limit, not a hard RSS guarantee.

## Measurable Results

| Final run | Scenarios | HTTP origin | UDP echo | Runner time | Exit |
| --- | ---: | ---: | ---: | ---: | ---: |
| `matrix-final-20261102T172149Z` | 68 | 44 | 38 | 82.505 s | 0 |
| `edges-20261102T172351Z` | 13 | 6 | 6 | 8.581 s | 0 |
| Total | **81** | **50** | **44** | **91.086 s** | **0** |

Among the 81 rows: 44 positive scenarios with both exchanges, 13 expected
parser rejects, 8 expected Xray preflight rejects, 1 catalogue rejection of a cyclic chain,
1 rejection of an old ambiguous reference, and 13 negative runtime
scenarios. The parameter-preservation check is a separate row without exchange.
Additional checks inside rows are described below.

**134 individual `xray run -test` runs** were executed: 126 `Configuration OK`, 8
deliberate rejections. The count includes server and probe configurations,
so it does not equal the scenario count. In the final runs, **131 Xray runtime processes** were started and awaited
to completion. Together with diagnostic runs —
246/246; remaining S5 processes at final check — 0.

### Positive Compatibility

`P/C/T/H/U = pass` means: parser accepted, catalogue/Compile successful, Xray
preflight successful, correct HTTP and UDP replies received. The real exchange
was performed in **every row** of this table.

| Protocol and source | Security / transport | Actually checked variants | Result |
| --- | --- | --- | --- |
| VLESS URI | TLS / RAW | raw; tcp alias | P/C/T/H/U pass |
| VLESS URI | TLS / WS | path and Host | P/C/T/H/U pass |
| VLESS URI | TLS / gRPC | serviceName; h2 | P/C/T/H/U pass |
| VLESS URI | TLS / XHTTP | auto, stream-one, stream-up, packet-up; splithttp alias | P/C/T/H/U pass |
| VLESS URI | REALITY / RAW | regular flow; xtls-rprx-vision; default Chrome fp | P/C/T/H/U pass |
| VLESS URI | REALITY / gRPC | serviceName | P/C/T/H/U pass |
| VLESS URI | REALITY / XHTTP | auto, stream-one | P/C/T/H/U pass |
| Trojan URI | implicit TLS / RAW | password with escaped `:@+%/` | P/C/T/H/U pass |
| Trojan URI | TLS / WS, gRPC, XHTTP | WS path/Host; gRPC serviceName; XHTTP auto | P/C/T/H/U pass |
| Trojan URI | REALITY / RAW, gRPC, XHTTP | local TLS-target; synthetic X25519 key and shortId | P/C/T/H/U pass |
| VLESS JSON | none / RAW | unencrypted JSON transport | P/C/T/H/U pass |
| Shadowsocks URI | native TCP/UDP | aes-128-gcm plain userinfo; aes-256-gcm std base64; chacha20-ietf-poly1305 URL-safe base64 | P/C/T/H/U pass |
| Shadowsocks URI | native TCP/UDP | legacy authority base64: standard and URL-safe | P/C/T/H/U pass |
| Shadowsocks 2022 URI | native TCP/UDP | 2022-blake3-aes-128-gcm; aes-256-gcm; chacha20-poly1305 with correctly sized keys | P/C/T/H/U pass |
| TLS parameters | RAW and Trojan WS | Chrome/default fingerprint; exact SHA256 pin; wrong+correct pin list | P/C/T/H/U pass |
| XHTTP extra | TLS / packet-up | host/path/mode from extra only, no query value | P/C/T/H/U pass |
| VLESS Vision | TLS / RAW | Chrome fingerprint | P/C/T/H/U pass |

Pin-positive cases run the **client without trust in the test CA**
(`SSL_CERT_FILE=untrusted.pem`). Success with the exact pin and rejection with a wrong pin
confirm pin behavior, not incidental success via a trusted CA.

### Preservation of Important Fields

Checked: URI query precedence over XHTTP extra, extraction of host/path/mode from
extra with an empty query, deep equality of extra with `noGRPCHeader`, `xPaddingBytes`,
`scMaxEachPostBytes`, `xmux`, and nested `downloadSettings`. Preserved:
`tlsSettings.alpn`, `cipherSuites`, `pinnedPeerCertSha256`.

After Parse → UnprefixNodes → JSON → Parse, the same outbound is produced.
After Compile, the whole outbound is compared with the original Node. In additional
chain checks, `sendThrough`, `targetStrategy`, `mux`, and both semantics of
`proxySettings.transportLayer` are preserved. `mux.enabled=false` checks field preservation;
enabled-mux operation is not claimed by this result.

A mixed VLESS/Trojan/SS list in standard/raw-standard/URL-safe/raw-URL-safe
base64 with line breaks yields three valid Nodes in all four variants.
This is a separate **parser** list check. SIP002/legacy SS network exchanges
were run independently and are reflected above.

### Chains and Probes

Four real chains VLESS → Shadowsocks hop → origin carried HTTP and UDP:

1. `streamSettings.sockopt.dialerProxy`.
2. `proxySettings.tag`, `transportLayer=true`.
3. XHTTP stream-up with an upper dialerProxy and nested
   `extra.downloadSettings.sockopt.dialerProxy`.
4. `proxySettings.tag`, `transportLayer=false`, VLESS JSON RAW over SS.

Profile import remaps dependency tags, ignores the provider's inbounds/routing,
and keeps the needed helper outbounds. The Unprefix round trip
does not corrupt references. For the first three chains, **three real
authenticated HTTP checks of production `CompileProbe` + `Probe`** were performed:
Available=true. For the fourth chain, only CompileProbe preflight was checked.

After the SS-hop stop, the remaining reachable entry Xray gives no direct bypass:
HTTP failed in all four chains, origin delta=0. For the fourth chain,
UDP additionally failed, echo delta=0. For the first three, UDP during hop failure
was not run separately.

A missing dependency is rejected by the parser, a self-cycle by the catalogue before
runtime, simultaneous conflicting proxySettings/dialerProxy by Xray preflight.
In nested-download, upload and download use the same hop; the test does not
separate independent failures of different download hops.

### Credential Rotation

For VLESS UUID, Trojan password, and SS password, three full cycles were run:
old credentials work → server replaced → old client gets no HTTP/UDP
and does not reach origin → subscription with new credentials and a new name passes
catalogue/Compile with the previous nodeRef → new client gets HTTP/UDP.

The ID is stable with unchanged source, endpoint, protocol, network/security, and
unambiguous matching. Old bound IDs are not substituted when two different
connections share one endpoint but both names and credentials changed
simultaneously: the two old IDs stay unavailable, the new ones get new IDs;
compile with a single old nodeRef is rejected.

### Rejections, Incompatible Variants, and Their Stage

| Condition | Stage and fact | Network result |
| --- | --- | --- |
| Unknown transport/URI option, security=none in URI, allowInsecure, duplicate query | parser reject | Runtime was not started |
| XHTTP extra=null/array/numeric path; extra on WS | parser reject | Runtime was not started |
| REALITY without pbk, Trojan flow, SS plugin | parser reject | Runtime was not started |
| XHTTP unknown mode; stream-one+downloadSettings | parser pass, Compile pass, Xray preflight reject | Runtime was not started |
| REALITY+WS, wrong pbk; short pin; SS unknown method/short SS2022 key | parser pass, Compile pass, Xray preflight reject | Runtime was not started |
| VLESS/Trojan/SS upstream missing | parser/Compile/preflight pass | HTTP and UDP failed; origin/echo delta=0 |
| TLS untrusted CA, wrong name, wrong pin | parser/Compile/preflight pass | HTTP failed; origin delta=0; server log: `remote error: tls: bad certificate` |
| Mismatched ALPN | parser/Compile/preflight pass | HTTP failed; origin delta=0; server log: `client requested unsupported application protocols` |
| WS wrong path, gRPC wrong serviceName, XHTTP wrong path | parser/Compile/preflight pass | HTTP failed; origin delta=0 |
| REALITY wrong shortId or foreign public key of correct length | parser/Compile/preflight pass | HTTP failed; origin delta=0; client log: `received real certificate` |
| Vision+WS | parser/Compile/preflight pass | HTTP/UDP failed, origin/echo delta=0; runtime: `XTLS only supports TLS and REALITY directly for now` |

Runtime rejections were measured in a bounded window: HTTP normally 1.8 s, UDP 0.8 s;
on credential change HTTP 1.5 s, UDP 0.7 s. This confirms absence of successful
delivery in that window, not the maximum Xray retry-exhaustion time.
The TLS/ALPN and Vision causes are confirmed by runtime logs, not by a single timeout.

## Defects and Warnings

Confirmed application defects: **0**. Severity/conditions/impact/patch for an
application error are not assigned, since S5 reproduced no such error.
No proposed production patch was created or applied.

Below are observations with exact boundaries, **not registered bugs**:

- `internal/core/subscription.go:250` and `:300` accept WS+REALITY at the
  format level; Xray 26.3.27 rejects it at preflight. This is expected handoff of
  engine-specific validation to the installed engine.
- `internal/core/subscription.go:315` preserves the Vision flow regardless of
  transport. Vision+WS passes `xray run -test` but not runtime. Impact:
  parser/preflight are no guarantee of connectivity for an incompatible link.
  Level: an Xray input-parameter limitation, not a Nadkop defect.
- gRPC, WS Host in headers, and REALITY on an ephemeral port produce engine warnings.
  This is not a connection error; the corresponding positive HTTP/UDP exchange
  was confirmed separately.
- `internal/core/xray_json.go:267` and `:276` remap dialerProxy and
  proxySettings.tag; `internal/core/probe_config.go:17` preserves the closure;
  `internal/core/servers.go:354` allows only unambiguous matching;
  `internal/core/compile.go:190` forbids a vanished single nodeRef.
  Real results are consistent with these paths.

The first diagnostic run `matrix-initial-20261102T171850Z` finished with
exit 1: eight REALITY rows got no HTTP/UDP. The cause lay in the
**S5 stand**: the TLS target closed on ReadHeaderTimeout=2 s, while Xray
26.3.27 resolves post-handshake records in about 5 s. Only the
S5 tests were changed: target timeout=20 s and a 5.3 s detector wait before the client. The targeted
`reality-diagnosis-20261102T172105Z` then gave HTTP/UDP pass, and the final matrix
confirmed all eight REALITY rows. The initial error is not presented as an application
defect. Logs of both states are kept.

## What Was Not Confirmed

- Xray 25.1.30 from OpenWrt 24.10.8 and Xray 26.10.21 used by the shared VM
  were not run here. Compatibility with them cannot be inferred from 26.3.27.
- Clean install, OpenWrt/procd/nft, TPROXY, LAN/DNS/IPv6, physical router,
  external Internet, and real VPN providers were outside S5.
- No full Cartesian product, long sessions, load,
  multiplexed-stream stress, QUIC, packet loss/MTU, congestion, or throughput.
- SS over arbitrary Xray WS/gRPC/XHTTP/TLS, SIP003 plugins, VMess,
  HTTP/SOCKS outbounds as primary connections, and new VLESS ciphers
  were not confirmed here.
- URI `cipherSuites` is proven as a preserved parameter and passed preflight;
  selection of a specific cipher suite in a TLS 1.2 handshake was not measured.
- Enabled mux, separate distinct upstreams for XHTTP upload/download,
  interchangeability of all fingerprints, and endpoint/chain rotation were not checked.
- The mixed base64-list and deep-equality checks by themselves are not
  a network exchange; network results are listed separately.

## Reproduction and Artifacts

New files belong to S5 and were published atomically. All Go test entrypoints
carry the `TestS5` prefix, helpers — `s5`. The full shared test run was not launched.
Tests are skipped by default without an explicit `S5_XRAY`, so the
integration stand is not started unexpectedly in someone else's run.

```sh
python3 scripts/test-s5-prepare-xray.py
python3 scripts/test-s5-protocol-compatibility.py --label matrix --run TestS5Compatibility
python3 scripts/test-s5-protocol-compatibility.py --label edges --run TestS5ProtocolEdges
```

Prepare only reads the pinned local SDK inputs, verifies their SHA256, and
creates copies for S5. It stops on a different checksum. The runner prints
its private run path and launches respectively:

```text
go test -p 1 ./internal/core -run ^TestS5Compatibility$ -count=1 -timeout 330s -json
go test -p 1 ./internal/core -run ^TestS5ProtocolEdges$ -count=1 -timeout 330s -json
```

Final compile check after mechanically assigning the S5 prefixes to
helpers: `GOMAXPROCS=2 go test -p 1 ./internal/core -list '^TestS5'` — exit 0.
Behavior was unchanged after this renaming; no repeat full
run was performed.

Test and runner files:

- `internal/core/s5_protocol_compatibility_test.go`
- `internal/core/s5_protocol_edges_test.go`
- `scripts/test-s5-protocol-compatibility.py`
- `scripts/test-s5-prepare-xray.py`

Private root folder **`build/agent-audit/S5/`, mode 0700**:

- `summary.json` — machine summary, commands, S5 test hashes.
- `engine-provenance.json`, `engine-preparation.json`, `engine-version.log` —
  engine and provenance.
- `matrix-final-20261102T172149Z/` and `edges-20261102T172351Z/` — final
  `runner.json`, `results.json`, `go-test.jsonl`, `process-cleanup.json`;
  per scenario: subscription, catalogue, compiled/client/server JSON,
  `*.preflight.log`, `*.runtime.log`.
- `matrix-initial-20261102T171850Z/` and
  `reality-diagnosis-20261102T172105Z/` — the initial stand error and
  the REALITY control success after target tuning.
- Certificates, private test keys, `reality-private.json`, and configs
  live only inside private run folders, mode 0600. Private
  keys are not included in the report.

Total S5 artifact size is about 53 MiB; disk/resource stress tests
were not run. All its runtime processes finished; exit/wait and
absence of remaining executable S5 processes were verified.

## SHA256 of the Guarded Source State

Manifest: `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json`.

```text
Manifest SHA256:
09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2
```

Full per-path verification against the expected SHA256 is stored separately:
`build/agent-audit/S5/frozen-before.json` and `frozen-after.json`.
Before tests: 327/327 matches, 0 mismatches. After tests: 327/327, 0 mismatches.
Between before/after: 0 changed guarded files. Hashes of new S5 tests and
scripts are in `summary.json`.
