**S4 — Nadkop 0.3.0-r4 / LuCI 0.3.0-r8 Resources and Performance**

Date: 2026-11-02. Working directory: `<repo>`.
The task was performed by a standalone test agent, without subagents, PRs, external messages, VMs, SDK, or application changes.

**132 completed measurements, 250 operation iterations** were obtained in the main pass and the HTTPS supplement. 25 scenarios selected: 22 actually executed, 20 passed the stand's functional checks, one reproduced **confirmed defect S4-D01**, one was stopped by the RSS guard; three higher steps were skipped. These counts are not a release-readiness verdict. No time/memory threshold for the target router is set.

**Source Pinning and Scope.**

`reports/parallel-tasks.md`, `reports/blockers.md`, `docs/product-contract.md` were read. Before tests: 327/327 guarded files matched. After all tests: 327/327 matched, no missing/modified files. Manifest: `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json`, SHA256 `09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`. Full line-by-line verifications: `before` (local stand artifact), `after` (local stand artifact); each individual run also contains its own `frozen-before.json` / `frozen-after.json`. Initial verification start: 2026-11-02T17:12:10.208436+00:00; final verification: 2026-11-02T17:56:00.470012+00:00.

Changes are limited to its own `internal/core/s4_performance_test.go`, `scripts/test-s4-performance.py`, this report, and `build/agent-audit/S4/`. The test and runner were published via rename of a prepared file. Production, recipes, `go.mod`, `go.sum`, README, shared reports/runners, and SDK were not modified. The full `go test ./...` was not run. All endpoints/credentials/UUIDs/HWIDs are synthetic; no real user data was read or printed. No external subscriptions or VM connections.

**Stand and Measurement Method.**

Linux x86_64, AMD Ryzen 7 5700X, Go 1.26.10. Only CPUs 14 and 15 available via affinity of its own child processes, `GOMAXPROCS=2`, build `go test -c -p=2 -vet=off`, `CGO_ENABLED=0`. A dedicated private `GOCACHE` and temporary directories inside S4; no SDK used. Only the manifest-listed production files `internal/core/*.go` and the S4 test are compiled. Other tests are not run and are not even included in this build.

Each scenario is a new process; heavy scenarios run sequentially. `GOMEMLIMIT=576MiB` is a soft Go GC tuning, not a hard RSS limit. An external watchdog checks RSS/HWM of each own child every 10 ms and stops only its own process group at 736 MiB, leaving headroom up to about 768 MiB. The highest observation was 745.77 MiB; the between-polls threshold breach was recorded, the offending process stopped. There is a 120 s timeout, disabled core dumps, and a 256 MiB file limit. Host firewall/routes/sysctl/clock were unchanged. After the guard stop, the runner deleted its own temporary geodatabases. Total size of retained S4 artifacts at report time was about 133 MiB, mostly the private Go cache.

Wall time comes from Go monotonic clocks, CPU = `getrusage` user + system. `TotalAlloc` / `Mallocs` are per-operation runtime deltas, **not peak live heap**. GC runs before each measured phase; fixture preparation, pre-phase GC, and JSON result writing are excluded from phase time. CPU and allocs include small harness overhead; for very short phases CPU may not be comparable to wall. RSS in tables is the maximum of the **whole scenario process**, including preparation and multiple phases; for successful processes `wait4.ru_maxrss` is used together with `/proc` sampling, and the JSON also holds the accumulated Go HWM and sampled phase peak. The stopped phase has no final per-operation CPU/wall/allocs: only process data and a start marker exist. Cold-cache/steady-state p95 and long-term leaks were not measured; normally 3 iterations, heavy geosets — one.

**Reproducible Commands.** From the project root, each `--output` must point to a new directory:

```sh
# Full S4 matrix; exit 1 expected due to S4-D01.
python3 scripts/test-s4-performance.py --output build/agent-audit/S4/reproduce-NEW

# Regular scale and combined maximum only.
python3 scripts/test-s4-performance.py --reuse-binary --cases scale-16,scale-256,scale-1024,scale-2048,scale-4096,maximum --output build/agent-audit/S4/scale-NEW

# Standalone defect reproduction; exit 1 expected.
python3 scripts/test-s4-performance.py --reuse-binary --cases catalogue-byte-budget --output build/agent-audit/S4/defect-NEW

# Real loopback HTTPS and metadata observation without dumping network payloads.
python3 scripts/test-s4-performance.py --reuse-binary --strace-fs-only --cases https-byte-budget --output build/agent-audit/S4/io-NEW

# Tracing of write/eventfd and catalogue writes on synthetic data.
python3 scripts/test-s4-performance.py --reuse-binary --strace --cases scale-1024 --output build/agent-audit/S4/trace-NEW
```

The runner builds an isolated binary and runs only `-test.run=^TestS4Performance$ -test.count=1 -test.parallel=1`. Without `S4_CASE` the test is skipped so that an incidental full run does not start the load. `--reuse-binary` verifies the fingerprint of all production Go files and the S4 test. Full commands and settings of each run are stored in its `environment.json` / `build.json` / `results.json`; successful build logs may be empty. The measurement reference is `verified` (local stand artifact) plus `https-budget` (local stand artifact). Summary `JSON` (local stand artifact), `metrics JSON` (local stand artifact), `CSV` (local stand artifact), `counters` (local stand artifact), and `scale HTML` (local stand artifact). Source-version snapshots of the stand itself are stored in each corresponding run directory.

**Subscriptions, Catalogue, Rules, and Repeated Refresh.**

"Small" = 16 nodes / 8 rules, "typical" = 256 / 64 — these are explicit stand assumptions, not user statistics. The main payloads are unique Trojan + TLS/WS outbound JSONs with `.s4.invalid` domains. All 4096 catalogue records have distinct IDs. Rules contain a domain and an IP; every fourth rule is proxy with `nodeRef`, the rest block. The outbounds do not connect to the Internet.

Time is ms/operation, CPU is user/system sum. Allocations are MiB / count per one full local refresh. Flash metrics are given separately.

| Nodes | Rules | Subscription, bytes | unchanged sync, ms | compile, ms | load+sync+compile wall / CPU, ms | alloc MiB / allocs refresh | Process RSS, MiB |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 16 | 8 | 4,257 | 2.531 | 0.357 | 3.842 / 3.931 | 1.238 / 21,041 | 12.44 |
| 256 | 64 | 68,097 | 37.281 | 4.955 | 55.938 / 59.322 | 20.010 / 331,461 | 13.93 |
| 1024 | 256 | 272,385 | 155.447 | 20.684 | 229.233 / 247.996 | 80.295 / 1,324,567 | 20.49 |
| 2048 | 512 | 544,769 | 318.749 | 41.463 | 456.639 / 490.214 | 161.051 / 2,648,476 | 31.13 |
| 4096 | 1024 | 1,089,537 | 681.238 | 82.703 | 1021.429 / 1095.792 | 324.270 / 5,296,261 | 51.73 |

Combined maximum: **16 sources × 256 nodes = 4096 records**, 1024 rules, 16 groups of 16 members, 64 unique checked refs total. Every four groups use the same four blocks of 16 IDs; each ID actually lands in the health manifest. `ReadConfig` accepts the configuration; compile yields 16 groups and 64 unique refs; the initial group choice is blocking. Configuration 125,809 bytes, subscriptions 1,089,552 bytes total, catalogue 1,551,300 bytes, artifacts 2,346,785 bytes. Generated monitoring credentials are random and change on compile; byte equality of runtime with groups is not expected.

| Operation at combined maximum | wall, ms | CPU, ms | alloc, MiB | allocs |
| --- | ---: | ---: | ---: | ---: |
| unchanged sync | 660.939 | 715.784 | 220.613 | 3,666,596 |
| compile | 82.248 | 88.985 | 24.015 | 325,060 |
| CompileProbe 64 refs from 4096 nodes | 5.200 | 5.916 | 2.503 | 28,570 |
| local load+sync+compile | 947.335 | 1015.749 | 323.590 | 5,299,415 |

Combined-maximum process peak — 49.98 MiB. This is compilation/catalogue, not a running Xray/nfqws. Additionally, a 3,095,218-byte configuration with 1024 rules, 128 domains per rule (**131,072 domain + 1024 IP matchers**) was accepted and compiled: 73.700 ms read, 60.743 ms compile, 74.499 ms compile CPU, 46.692 MiB / 181,446 allocs, Xray JSON 5,610,701 bytes; process peak 59.12 MiB.

JSON profiles, URI links, and base64 URI links were also checked on 4096 nodes. This is a parsing/generation check; there is no real VLESS/Trojan exchange via Xray here.

| Format, 4096 nodes | Input, bytes | parse wall, ms | parse CPU, ms | parse alloc, MiB | full local refresh, ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| json | 1,089,537 | 210.616 | 225.502 | 77.554 | 1021.429 |
| profiles | 1,269,761 | 493.119 | 524.383 | 178.142 | 1267.712 |
| links | 524,288 | 204.159 | 219.974 | 81.689 | 1154.520 |
| base64 | 699,052 | 219.948 | 229.474 | 82.866 | 1157.832 |

**Unchanged Refresh and Writes.**

For each regular step: one initial sync, 3 unchanged syncs, 3 compiles, 3 full local refreshes, a single synthetic credential rotation, and 2 more unchanged syncs (no rotation was performed for URI formats). After rotation the first server's ID is preserved. The initial sync and the real rotation each publish the catalogue once; unchanged operations produce **0 catalogue-content writes, 0 new temp files/renames**, inode/mtime/SHA256 stay the same, and the `/proc/self/io.write_bytes` increment is 0. A repeated refresh over 4096 nodes still consumes about 324 MiB of aggregate allocations and 1.02 s of wall: absence of writes does not mean a cheap re-check.

`syscw/wchar` are kept raw in JSON/CSV: Go 1.26 sometimes writes 8 bytes to the runtime `eventfd`. `Tracing` (local stand artifact) separated these records from files. `IN_CLOSE_WRITE` on an open `O_RDWR` lock likewise does not mean data writes. The initial measurement stand wrongly required zero `syscw` and counted the number of still-closing responder handlers as the number of active probes; the original `main/` logs are kept for transparency. Only the stand's checks were fixed, then the main matrix was repeated in `verified/`. Their initial failures are not product defects.

A separate **real local HTTPS exchange** was performed: a TLS server with a trust transport for this test only, 16 sources of 4,194,304 bytes each = **64 MiB per refresh**, 256 nodes each. The limit is reached via JSON whitespace padding, so this is a boundary transfer volume, not 64 MiB of dense outbound objects. Two loaders; the global HTTP transport is unchanged. 16 initial HTTPS requests and 32 in two full refreshes; process peak 78.45 MiB.

| HTTPS, 16 × 4 MiB | wall, ms | CPU, ms | alloc, MiB | allocs |
| --- | ---: | ---: | ---: | ---: |
| loading 16 sources | 283.889 | 512.462 | 359.487 | 1,314,644 |
| unchanged HTTPS load+sync+compile | 1013.495 | 1274.464 | 599.149 | 5,304,093 |

In unchanged HTTPS refreshes the catalogue does not change and recorded kernel bytes are 0, but **32 `fchmod(hwid, 0600)` / `IN_ATTRIB` over two refreshes** remain. These are real metadata operations on the synthetic HWID with 0600 already set (`internal/core/hwid.go:105`, called from `applySourceHeaders:203`), observation **S4-O02**, not proof of a physical write/erase-flash count. In the same interval, tracing showed 0 fsync/fdatasync and 0 renames. Evidence: `io-evidence.json` (local stand artifact), `FS trace` (local stand artifact). The first creation of the synthetic HWID writes the file once; this is identity setup, not a redundant write of the unchanged catalogue. In production `backend.load`, up to 16 sources load concurrently (`internal/router/rpc.go:398`); the stand's deliberate two-loader limit does not measure the peak of that full concurrency.

**64 checked refs / 16 groups: genuinely local probes.**

A real `MonitorCycle` was invoked with a real `Probe` through its own loopback SOCKS5 with password auth and a synthetic HTTP 204 responder; the latter answers after 2 ms and performs no DNS lookups/outgoing dials. A local stub checking for absence of `block` is used for the selected-outbound apply callback. Xray API, VPN operation, NAT/firewall, and background 15-second cycles were not checked. Only active `Probe` callbacks are counted: at most 4, on two CPUs.

| Group URLs | HTTP requests per cycle | Concurrent Probes | apply callbacks | wall, ms | CPU, ms | alloc MiB / allocs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| shared URL | 64 | 4 | 16 | 43.944 | 14.450 | 1.729 / 11,750 |
| distinct group URLs | 256 | 4 | 16 | 171.536 | 53.490 | 6.522 / 44,811 |

256 requests with distinct URLs reflect deduplication by URL+ID pair in `MonitorCycle`, not a violation of the 64-unique-server limit. All 16 groups received an available choice in both cycles. Process peak 14.02 MiB.

**Large Geodatabases Available and Expansion Scale.**

The existing local public pair was used without re-downloading; `inputs and SHA256` (local stand artifact):

| File | Bytes | SHA256 |
| --- | ---: | --- |
| `build/geodata/geosite.dat` | 11 313 407 | `de0ed52484ee4e793b63e3f9d9de9677e90b289483d96d94fa7f2d7503bdced0` |
| `build/geodata/geoip.dat` | 16 549 675 | `fbf747a085be25eb61ecac8c27140b4ab0bf8181af6fdb104caa325bd93cf117` |

`OpenGeoDatabase`: 119.684 ms wall, 129.349 ms CPU, 67.763 MiB / 539,890 allocs; whole-scenario peak 89.20 MiB. The catalogue contains **2285 selectors** (additionally recorded in `filesystem-trace/geo-real.log`); catalogue fetch about 10.11 ms. Expanded: `geosite:github` (65 domains), `geosite:youtube` (178), `mixed:telegram` (21 domains + 12 prefixes), `geoip:ru` (25,157 prefixes), largest geosite **`category-ads-all` (195,040 records)** and largest non-inverted GeoIP **`us` (261,288 prefixes)**. Set counters are given before dedup within the rule.

For the worst-case scale, each block rule repeats this largest pair. Duplicate/overlapping rules are contract-permitted, but such a profile is not "typical". `ReadConfig` accepts every actually run configuration. The time/allocations below refer to compile, the RSS peak to the whole process; at measurement time the input DB of the first discovery of the largest sets was freed before reopening inside compile.

| Rules | Config, bytes | Total records before unique | compile wall / CPU, ms | alloc MiB / allocs | Process RSS, MiB | Xray JSON, bytes | Result |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 289 | 456,328 | 393.068 / 444.591 | 276.084 / 1,456,036 | 120.44 | 16,251,826 | completed |
| 4 | 580 | 1,825,312 | 1255.381 / 1462.429 | 900.968 / 4,203,784 | 405.68 | 65,005,369 | completed |
| 16 | 1750 | 7,301,248 | not completed | not fully measured | 745.77 | not published | RSS-guard SIGKILL after 3.767 s of whole process |
| 64 / 256 / 1024 | — | — | not run | — | — | — | stopped at a lower step |

At 16 rules the marker holds 7,301,248 source records with a configuration of only 1750 bytes. Last RSS observation 763,672 KiB, kernel HWM 763,516 KiB; the watchdog killed its process with SIGKILL at the 753,664 KiB threshold. Whole-process CPU before the stop 4.754 s, whole-process wall 3.767 s. No completed artifact and no final compile metric. This is **resource limitation S4-R01**, not automatically a functional bug: there is no target-device budget, the process was stopped by the test agent, not by proven kernel OOM/application error. Without the stand limit, this phase was not explored.

Sets are re-expanded for every rule (`internal/core/compile.go:166`, `internal/core/matchers.go:51`); this explains the volume and allocation growth. Neither caching nor rule merging was applied. Even 4 rules produced ~62 MiB of JSON, and 1 produced ~15.5 MiB. Before claiming support for "1024 rules with arbitrary sets", an explicit budget on expanded matcher/output-byte/memory counts and rejection testing within that budget is needed, or optimization with separate testing. The rule count alone does not define such a budget.

Installing the real pair into its own temp dir: 95.350 ms; reinstalling the **identical pair**: 89.564 ms CPU/wall about 90 ms, **2 writes, 27,863,082 payload bytes**, `current` changes, two generations kept. This is **S4-O03**, actual `InstallGeoFiles` behavior (`internal/core/geodata.go:419`, `:429`, `:439`), not a confirmed bug: the contract does not promise to skip an identical manual update. Network `UpdateGeoData` was not invoked here; these numbers refer to local file installation.

**Boundary and Oversized Data.**

Exact limits and off-by-one exceedances were checked. Rejection in all applicable cases was obtained without changing the preceding catalogue/current.

| Check | JSON/text volume, bytes | Result | wall, ms |
| --- | ---: | --- | ---: |
| `rules_1024` | 112,648 | accepted | 2.840 |
| `rules_1025` | 112,793 | rejected | 1.814 |
| `sources_16` | 2,636 | accepted | 0.045 |
| `sources_17` | 2,790 | rejected | 0.041 |
| `group_members_16` | 873 | accepted | 0.041 |
| `group_members_17` | 912 | rejected | 0.031 |
| `groups_16` | 3,190 | accepted | 0.081 |
| `groups_17` | 3,378 | rejected | 0.093 |
| `checked_refs_64` | 3,233 | accepted | 0.085 |
| `checked_refs_65` | 3,272 | rejected | 0.083 |
| `picker_refs_4096` | 159,949 | accepted | 4.302 |
| `picker_refs_4097` | 159,988 | rejected | 1.481 |
| `matchers_16384` | 316,793 | accepted | 9.553 |
| `matchers_16385` | 316,813 | rejected | 4.022 |
| `rule_sets_64` | 1,249 | accepted | 0.054 |
| `rule_sets_65` | 1,265 | rejected | 0.034 |
| `config_bytes_4194304` | 4,194,304 | accepted | 28.628 |
| `config_bytes_4194305` | 4,194,305 | rejected | 1.318 |
| `subscription_bytes_4194304` | 4,194,304 | accepted | 4.449 |
| `subscription_bytes_4194305` | 4,194,305 | rejected | 4.322 |
| `nested_json_10000` | 20,001 | rejected | 0.642 |
| `nested_json_10001` | 20,003 | rejected | 0.157 |

`nested_json_*` were rejected as an unsuitable subscription form; this is a bounded-time malformed-parser-input check, not proof of safe network transfer. Positive tests of 64 geosets check the `ReadConfig` syntactic limit, without claiming that all such sets exist.

The 4096-record catalogue was accepted; adding a 4097th was rejected with `server catalogue limit reached; bound references are retained`, inode/mtime/hash unchanged, 0 records written. A 20,000-record geodatabase was accepted, 20,001 rejected with `too many geo entries`; 4 malformed protobufs (out-of-buffer length, truncated nested field, zero key, empty record) were rejected.

Exactly 64 MiB of geosite + a 16-byte GeoIP were accepted by `InstallGeoFiles` and `OpenGeoDatabase`. This is valid protobuf padding with an unknown field, **not a dense 64-MiB real database**. Opening took 74.671 ms wall / 84.985 ms CPU, 157.697 MiB of aggregate allocations. 64 MiB + 1 byte was rejected by both the installer and the reader; install did not change `current`. The whole-scenario peak of 310.04 MiB includes creation/copying of large fixtures; this number must not be presented as a standalone `OpenGeoDatabase` peak. The synthetic oversized file was placed only in its own process temp directory.

**S4-D01 — an accepted subscription publishes a catalogue that then cannot be read; high / P1.**

Conditions: one valid Xray JSON source, **4096 nodes, 3,190,785 bytes (< 4 MiB)**, unique 523-byte ASCII tags (`"s4" × 256 + "-node-00000"`). The tags contain no forbidden characters, and there is no tag-length limit in `ParseSubscription` (`internal/core/subscription.go:164`). The fixture is fully synthetic.

Actual sequence: `loadContext` accepts 4096 nodes; `SyncServers` returns success and atomically publishes a **7,818,180-byte** `servers.json`; the next `ServerSummaries` and the **actually executed repeat `SyncServers`** both return `cannot read server catalogue`. The final targeted run contains both errors in one structured result. The same file and IDs remain on disk; no data deletion was observed.

Cause: the reader is limited to 4 MiB (`internal/core/servers.go:62`), while the writer checks only the record count (`:232`), serializes without a size check (`:235`), and publishes all bytes (`:243`). Display name/tag/aliases repeat the tags (`:381`, `:400`, `:404`), so an accepted sub-4-MiB source expands into a catalogue unreadable by its own reader.

Impact: after successful publication, the summary and sync APIs stop working for this file. Product picker/import/refresh/service paths call these APIs (`internal/router/rpc.go:174`, `:410`; `internal/router/source_import.go:81`); error propagation follows from these calls. Live traffic of the running generation, LuCI on a VM, and real-device recovery were not checked by this test; their failure is not claimed.

Reproduced three times: `main` (local stand artifact), `verified` (local stand artifact), `repeat read+sync` (local stand artifact). Machine-readable description: `defects.json` (local stand artifact).

An **unapplied** `patch` (local stand artifact) was proposed: check `len(after)+1 <= maxSubscription` before temp creation and publication. This is minimal protection of the previous catalogue; it does not resolve the product policy of supporting arbitrary long names in 4096 records and does not recover an already oversized file. A full fix requires aligning the reader/writer/format byte budget with the contract while keeping stable refs. The patch was not applied and is not presented as a validated fix.

**Limitations and Required Budgets.**

Host timing must not be used as a guarantee for a cheap router. Not measured: OpenWrt MIPS/ARM, hardware flash/UBIFS/JFFS2, Xray/nfqws CPU/RAM, real WAN/VPN/DNS/TPROXY, the updater timer, VM boot, and browser-side LuCI work. They are not replaced by parser/preflight. The HTTPS/SOCKS/HTTP exchanges in this report are real but strictly local, with synthetic responders; this is not VPN compatibility.

The 64/256/1024-rule steps with the largest set pair were not run after the RSS stop at 16. Two dense real 64-MiB geodatabases were unavailable; the protobuf-padding test does not replace them. 16 × 4 MiB HTTPS was checked with two loaders and whitespace padding; the peak of regular concurrent loading of all 16, dense 64-MiB JSON, repeated alias rotation, and a long soak were not studied. fsync frequency/duration metrics depend on the host disk and neighboring loads.

Readiness approval requires: a memory budget covering generator + Xray + nfqws + OS simultaneously, acceptable compile/refresh wall times on the target architecture, a limit on expanded geo-matchers/output JSON, and a geodatabase/HWID write policy. No arbitrary convenient product pass/fail threshold was introduced here. Confirmed S4-D01 requires resolution regardless of budget choice; S4-R01/O02/O03 remain separately labeled facts/limitations.

**Artifacts.**

All logs are in `build/agent-audit/S4/`; measurements are stored atomically in JSON and CSV. `verified/` is the main corrected measurement matrix; `https-budget/` is HTTPS timing without tracer; `io-trace/` and `filesystem-trace/` are syscall evidence (their timing is not used as a performance reference); `defect-repeat/` is the defect repeat; `smoke/`, `main/`, `refined-smoke/` are the history of validating the stand itself. Main runs contain `.log`, `.samples.json`, `build.json` (and `build.log` on a fresh build), `environment.json`, `results.json`, `metrics.*`, `summary.json`, SHA256 verifications, and test-source snapshots; the earliest smoke artifacts are kept in their original form. For the guard stop, the operation start is stored in `.log`/`last_started`; no completed metric exists. The chart is shown via HTML render; server-side PNG preview is unavailable due to missing browser libraries on the host, and no system packages were installed for it.
