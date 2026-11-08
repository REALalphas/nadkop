# S2 — Fault Injection and Data Preservation Under Crashes

Checked core **0.3.0-r4** / LuCI **0.3.0-r8**, Linux amd64, 2026-11-02.
Found **3 reproduced violations of explicit contract requirements**:
loss of a bound ID under concurrent CLI generate/save; successful stop with
unremoved policies on corrupted runtime; incomplete network rollback after
an apply/publish-current error. No fixes were applied.

A config/nodes inconsistency under certain fsync/SIGKILL faults was confirmed
separately. It is not declared a fourth contract defect: the contract does not
promise a general crash transaction for config/nodes/catalogue. Single-file
atomicity, multi-file consistency, and rollback of the actually applied network
are distinct observed properties.

**Isolation and limitations.** Work without Git, without subagents/delegates and PRs; B2/B3 VMs
were not used. Host firewall/routes/sysctl/clock, production,
recipes, go.mod/go.sum, SDK, README, and shared reports/runners were not modified. Real subscriptions/HWID
were not read. Narrow package router tests use a temp backend; `router.Run` with
a fixed `/etc/nadkop` on the host was not invoked. The only real ENOSPC was
in a user/mount/net/pid namespace on a dedicated **65536-byte** tmpfs. Filling
the shared disk was not performed. SIGKILL was addressed only to its own test children
via strace or to its own shell PID inside the namespace.

## What Was Done and How to Reproduce

Tools: Go 1.26.10, strace 7.3, bubblewrap 0.12.0. Go build/test limited to
`GOMAXPROCS=2`, `-p 1`; fault children — `GOMAXPROCS=1`,
`GODEBUG=asyncpreemptoff=1`. Subprocess helpers additionally use LockOSThread.
No changes to the global HTTP transport and no external HTTPS requests.

The **unmodified init file and real Go network code** ran inside the namespace.
External ip/nft/uci/ubus/Xray and procd were replaced with observable synthetic
stubs. This checks orchestration and commands/state, not nft/kernel integration,
not procd operation, and not a full network exchange. The Xray mock only parses JSON;
it must not be treated as a check of the real Xray preflight.

Only unique tests and S2 runners were added:

- `internal/router/s2_crash_test.go`: TestS2Child and TestS2* for atomicWrite,
  ui.lock, incomplete runtime, prepare failure, rollback policy mocks.
- `internal/core/s2_crash_test.go`: TestS2CoreChild, DeviceID concurrency/corrupt,
  permission for catalogue/geodata/DeviceID, and an incomplete geodatabase pair.
- `scripts/test-s2-crash.py`, `scripts/test-s2-namespace.py`,
  `scripts/test-s2-verify.py`. Go tests were published via temp file + rename.

Commands from the workspace root (run-id must be new; the runner
intentionally does not overwrite existing fixtures):

```sh
GOMAXPROCS=2 go test -p 1 -c -o build/agent-audit/S2/router.test ./internal/router
GOMAXPROCS=2 go test -p 1 -c -o build/agent-audit/S2/core.test ./internal/core
GOMAXPROCS=2 go build -p 1 -o build/agent-audit/S2/nadkop ./cmd/nadkop
GOMAXPROCS=2 go test -race -p 1 -count=1 -run '^TestS2' -v ./internal/router ./internal/core
python3 scripts/test-s2-crash.py --suite all --run-id accepted
python3 scripts/test-s2-namespace.py --run-id final2
python3 scripts/test-s2-verify.py
```

When reproducing, choose new run-ids. The verifier by default reads the pinned
artifacts `accepted`/`final2`. It checks **that observations match, including
defects**; it does not issue a product conformance certificate.

Measurable results:

- **48 syscall scenarios**: save 16 (including baseline and the real CLI race),
  atomicWrite 4, catalogue refresh 5, geodatabases 8, DeviceID 7, CLI generation 8.
  Of these, **9** are SIGKILL stops of its own Go children.
- **12** init/network scenarios + **1** real ENOSPC; after each lifecycle
  failure, the pre-recovery state and a separate fresh-start attempt were recorded.
  11/12 recovery attempts passed in the mock environment; the first current publish with
  an ln error left a state that blocks restart.
- Narrow `-race` run: **6** top-level tests + **9** subcases passed;
  **2** subprocess-only helpers are skipped in a normal run and were invoked separately.
  The Go race detector reported no issues. The full suite was not run.
- **227/227** evidence-verifier assertions executed.
  This includes checks of reproduced failures, not 227 passed
  application acceptance criteria.
- S2 artifacts occupy about **48 MiB**; the shared disk was not filled.

Key evidence: `syscall JSON` (local stand artifact),
`syscall log` (local stand artifact),
`namespace JSON` (local stand artifact),
`namespace log` (local stand artifact),
`Go race log` (local stand artifact),
`assertions` (local stand artifact),
`verification log` (local stand artifact).
Early namespace/syscall fixtures are kept for auditing stand debugging, but
acceptance conclusions are based on `accepted` and `final2`. In particular, only
the S2 mock shebang/nft-removal model was fixed, not the product.

## D-S2-01 — A bound nodeRef is lost under save + CLI generate

**Severity: high. Condition:** an independent CLI `nadkop generate -config FIXTURE`
has already read the old config and source, while an RPC save before acquiring catalogue.lock
picked a different, subsequently vanished server. Regular RPC save/nodes/service share
a common ui.lock and are mutually exclusive; the defect is not claimed to be a race of the normal
automatic updater with save. CLI generate/nodes do not hold this UI lock.

Contract `docs/product-contract.md:114–117`: "Needed vanished servers are retained."
Root cause: `cmd/nadkop/main.go:84–105` reads config before SyncServers;
`internal/core/servers.go:119–145,209` prune uses the passed old snapshot,
although catalogue read/write itself is serialized by a separate lock. Save accepts
the ID against the previous catalogue in `internal/router/rpc.go:240–265`, then publishes config
and prunes in `:306–315`.

Reproduction — fixture `build/agent-audit/S2/fixtures/accepted/save-vs-stale-generator/`:

1. Synthetic A/B are in the catalogue; config C0 uses A.
   The local source of the next refresh already contains only A.
2. The real CLI generate is started. strace delays the first flock by 2 seconds,
   after reading C0/source, before the catalogue lock.
3. Meanwhile temp-backend RPC save of C1 with nodeRef B returns **success**.
4. The CLI continues with C0, deletes vanished B as supposedly unbound, and also
   returns **success**. C1 remains on disk with a catalogue-unknown ID.
5. A new subprocess `load + CompileWithGeo` is rejected. Returning B to the source
   assigns it a new ID and does **not** restore the previous reference.

Vanished ID: `srv_22222222222222222222222222222222`.
New ID on return in the accepted run: `srv_8ae4bbbbf0c59a910a91ba3009dfc6b1`.
JSON/ReadConfig/nodes/catalogue are valid; current and the old runtime file are unchanged.
Next compile response: `selected server is missing from the subscription; its reference is retained`.
The last part of the message does not mean the catalogue record was actually preserved.

**Impact:** the saved rule loses its stable mapping; restoring the
source does not restore enforcement; the user must reselect the server.
The running runtime did not switch in this fixture.

Evidence: `generator.strace`, `save.strace`, `evidence.json`,
`restart.stdout`, `restart-source-returned.stdout` in the named fixture.
A separate, **unapplied**
`shared-locking diff` (local stand artifact) was proposed.

## D-S2-02 — stop reports success while leaving its policy route/rule behind

**Severity: medium. Condition:** current/xray exists, TPROXY was previously enabled,
but `current/runtime.json` is corrupted. This is specifically handling of violated persisted
state, not proof that the application spontaneously corrupts the file.

Contract `docs/product-contract.md:103–105`: stop removes its policy routes,
then drops current. `internal/router/network.go:328` ignores the
readRuntime error; an empty plan disables the cleanup branch `:329–337`, after which current
is deleted in `:339–340` and the command returns 0.

Fixture `namespace-fixtures/final2/stop-corrupt-current/` executes a real
`nadkop network stop` inside the namespace. Before the call the model contains its own
IPv4 route `local default dev lo table 100` and rule priority 10000,
fwmark `0x10000/0x10000`, lookup 100; runtime.json equals `{`.
After: the nft model is deleted, current is absent, **both policy objects remain**;
exit **0**. The log shows only 3 nft commands, no ip cleanup calls.
A new start with a valid candidate passed in the mock environment.

**Impact:** stop does not perform the declared cleanup, reports an incomplete stop
as success, and loses the pointer to the plan. Real kernel routes/traffic were not changed.
Runtime/current state and hashes — in `evidence.json` before recovery;
calls — `commands.jsonl`, stdout — `run.stdout`.

A separate, **unapplied**
`stop-cleanup diff` (local stand artifact) was proposed.
It requires a future IPv6-less kernel check; the diff was not validated by application.

## D-S2-03 — an error after nft apply does not guarantee network rollback

**Severity: high on first start; medium for policies left over after
rollback to disabled TPROXY.** Verified with the real init/network code and external
stub commands. An unperformed rollback and inconsistent state are observed;
no real blackhole/rule bypass on the LAN was measured.

Contract `docs/product-contract.md:96–97` promises rollback of newly added policies
on error, `:107–108` — non-application of the new runtime on update error.
`openwrt/nadkop/files/nadkop.init:51–57` first applies the network, then publishes
current; on apply error it immediately deletes the candidate, on pointer error it rolls back
only when previous is non-empty. `internal/router/network.go:265–276` determines
policies to delete from the old current, which does not yet reflect the just
applied state. A cleanup error after successful nft does not roll back nft.

Subcases in `build/agent-audit/S2/namespace-fixtures/final2/`:

| Fixture | Observation before recovery | Repeated start |
| --- | --- | --- |
| `first-ln-fail` | ln function returns 1; nft new + 2 new IPv4 policies remain, current absent, candidate deleted, procd not called | Rejected: `Table inet nadkop exists without an owned generation` |
| `ln-fail`, `mv-fail` | Old pointer restored and old nft disabled, but 2 just-added policy objects remain | Passes in the model |
| `cleanup-after-nft-fail` | TPROXY disable: nft already deleted; delete of the old policy rule returns an error. Old current remains, candidate deleted; nft of the new configuration already applied | Passes after the mock fault is lifted |

In the first case rollback is not invoked at all: `previous` is empty. Start cannot run
again because of the guard on an existing table without an owned generation; Xray is not
registered. In the cleanup-failure case the fixture name means "cleanup
after nft completion, with a cleanup error"; **the nft call itself succeeds here**.

On an external-command error the kernel itself is not obliged to perform rollback; that
is the orchestration's duty, promised by the contract. Exact commands,
policy/table states, hashes, and the absence of procd are recorded in `evidence.json`/`run.stdout`.
The init + network patch proposal with a transaction/rollback token is kept separately
in `proposed-fixes.md` (local stand artifact).
It is an interface draft, **not a ready validated diff**; nothing was applied.

## F-S2-01 — File atomicity, crash-consistency, and durability boundaries

This is reproduced behavior with material impact, but **not a newly separately
confirmed contract defect** of a general transaction: the reviewed contract contains no such promise for
config/nodes/catalogue.

`atomicWrite` (`internal/router/rpc.go:545–572`) performs temp → write → file
fsync → close → rename → parent-directory fsync. On a write/file-fsync/rename
error the separate old JSON was preserved byte-for-byte. On parent-fsync EIO the file is already
**replaced**, although the function returns an error. No torn JSON was observed.
EIO and EPERM are syscall injection, not ENOSPC. Real EACCES was checked with chmod 0500
on its own directory and an unprivileged owner; rename onto a directory
was checked with a separate fixture, the destination directory was not overwritten.

`save` (`rpc.go:306–317`) does not form a single transaction for the three files:

| Fault | Config after fault | Nodes after fault | Next load+compile |
| --- | --- | --- | --- |
| nodes write/file-fsync EIO | Old | Old | Passes |
| nodes directory-fsync EIO | Old, with local/old | New, only new | `unknown node "local/old"` |
| config write/file-fsync EIO or rename EPERM | Old | Old values restored; LF added | Passes, byte revision changed |
| config directory-fsync EIO | **New**, with local/new | Rolled back only nodes to old values + LF | `unknown node "local/new"` |
| config-fsync + rollback-fsync EIO | Old | New | Explicit rollback error, compile rejected |
| SIGKILL before config rename | Old | New | `unknown node "local/old"` |
| catalogue cleanup write/fsync/rename EIO/EPERM | New | New | Passes; RPC explicitly reports `Configuration saved, but server cleanup failed` |
| SIGKILL before catalogue rename | New | New | Passes; unbound record left until next refresh |

In the listed fault snapshots config/nodes/catalogue are syntactically valid;
the semantic link is broken under some faults. In all save fixtures current
remains `generation.working`; SHA256 of the old runtime/xray before and after is identical:
`c334c777707ba2a89a875f6d0289cfd285d20dcb617969d18523956595e0c26b`.
Saving does not apply the runtime. The helper mode named `restart` is a **new process
invoking load + CompileWithGeo**, not a real Xray/procd start.

Exact full hashes of config/nodes/catalogue for each fault, references, validity,
missing nodeRefs/local tags, and the next-load result:
`file-state-matrix.tsv` (local stand artifact).
Each scenario also keeps an `evidence.json` with `before`, `afterFault`,
`afterRestart` where applicable. The hash is taken **before** recovery, which
may update the catalogue. ABSENT is recorded for missing files.

| Case | File | SHA256 before | SHA256 immediately after fault |
| --- | --- | --- | --- |
| save-nodes-dir-fsync-eio | config.json | `e218f0001420384cc3d552f02972fb0f26d371d644a7db0905971cc88a72e518` | `e218f0001420384cc3d552f02972fb0f26d371d644a7db0905971cc88a72e518` |
| save-nodes-dir-fsync-eio | nodes.json | `5986f4d545b5838c25aa79007a05fa04f2fa31d4a53a7388a1054351e315bb2a` | `66db430abbd9c6b5ee2cd8a7570bb20c9e94d1b48c0c8ac573e25f0f80bfc366` |
| save-nodes-dir-fsync-eio | servers.json | `17f356d62e5667fa6ef3d9aabd2d9c3e7ea8c94d1e7a3ec125f36f5327d69962` | `17f356d62e5667fa6ef3d9aabd2d9c3e7ea8c94d1e7a3ec125f36f5327d69962` |
| save-config-dir-fsync-eio | config.json | `5cf1b85164d0ba6822601ca3088370d7c8f14a9c09a43682aca277b550ded4dc` | `0f8e17f169c7f6b92e50703cb998a7bbb905f828f1fa3ec82a5c90a79de71b7c` |
| save-config-dir-fsync-eio | nodes.json | `5986f4d545b5838c25aa79007a05fa04f2fa31d4a53a7388a1054351e315bb2a` | `4871db7418096da87ea035d1ad7464cc1367d68671556c11aca1424daaf0c358` |
| save-config-dir-fsync-eio | servers.json | `17f356d62e5667fa6ef3d9aabd2d9c3e7ea8c94d1e7a3ec125f36f5327d69962` | `17f356d62e5667fa6ef3d9aabd2d9c3e7ea8c94d1e7a3ec125f36f5327d69962` |
| save-vs-stale-generator | config.json | `99501d2391cc942decc1eb5b0633712e78a34877282caf30d890248e6d5e1735` | `0cdf408fbd1bff99434d6b284a97da2e83feea8a72e7b682ef16243156a42be3` |
| save-vs-stale-generator | nodes.json | `5986f4d545b5838c25aa79007a05fa04f2fa31d4a53a7388a1054351e315bb2a` | `ceca88724b8570842c49170d5d9b452cd91270a680ac3368b8d82c3e1887ae6a` |
| save-vs-stale-generator | servers.json | `25f8b5e02cd67395d8ee21198c8bc4cf3797cc26b774dc6ff27ffcbca614467a` | `0264937927c19907a18c1a67c96660678be11a482784849b37f543be1f188861` |

## Other Verified Guarantees and Boundaries

**syncServers.** Separate write/file-fsync/rename failures leave the old
catalogue; a directory-fsync failure may leave the new version on error.
In all 5 catalogue fault fixtures bound A remained known; a new
load+compile passed. The lock is released after SIGKILL. This does not eliminate
the stale-config logic race in D-S2-01.

**InstallGeoFiles.** 8 syscall scenarios and a separate permission unit cover write #1/#2, rename EPERM,
SIGKILL between dat files, before current, and after current. The geosite/geoip order
in the map is nondeterministic; conclusions refer to the first/second entry, not one
fixed database kind. Before current the old readable pair remained:
`old.s2.example` + `192.0.2.1/32`; after current — the full new pair
`new.s2.example` + `192.0.2.2/32`. The previous generation was preserved; no mixed pair
was produced. An incomplete current with one geosite and no geoip was rejected. Geodatasets are
synthetic protobuf. The fsync-observation scenario did not trigger injection: **0 fsyncs**
in InstallGeoFiles. Data loss after power loss was not tested and is not declared
a reproduced bug.

**DeviceID.** Write/fsync/rename and SIGKILL before rename do not publish a partial ID;
a repeat call after the first creation receives a valid 32-character ID.
If directory fsync returns EIO after rename, the ID already exists; the repeat
call uses the same bytes. The existing synthetic ID did not change (mode
hardened to 0600). 24 concurrent callers received one ID; corrupted values
were not silently replaced. Creating a new ID after a first publication that did not finish is not
presented as rotation of an already issued identity.

**CLI generation.** The full output contains 10 artifact files; the old current
is unchanged. Write/rename failures and SIGKILL before publication left no incomplete
directory at the requested output; SIGKILL may leave hidden staging.
An existing output was rejected. Regeneration to a new path passed in all
8 scenarios. Fsync-observation did not reach injection: **0 fsyncs** when writing
the generation itself (the catalogue was already consistent). This is a process/rename check,
not a power-loss or flash/JFFS2/UBIFS test.

**Prepare/current/network.** Missing generation/runtime/rules/xray are rejected
before procd registration, old current is preserved; on compile and mock Xray error staging
is deleted, the old runtime hash matches. An nft apply error rolls back new policy
route/rule when the delete commands succeed. If the deletes themselves are rejected, the objects
remain: rollback in `network.go:229–233` ignores those errors. This is the boundary
of the proven guarantee, not a claim that a failed system
command can always be compensated. SIGKILL of init after network apply, before current,
left network new/current old; the subsequent start passed in the model. The contract states no separate
general crash guarantee for this point.

**Real ENOSPC.** A dedicated 64 KiB tmpfs was filled only by the S2 filler (a
1 MiB write attempt ended with ENOSPC). Atomic write also returned ENOSPC and preserved the JSON;
after filler removal the repeat write passed. SHA256 of the old file before/after:
`041e5924ebd1eca8a610f9391249b2e097f20f7a2802232408200cf883fb859a`.
`fillENOSPC`, `writeENOSPC`, `unchanged`, `jsonValid`, `retryOK` — all true.
This is not EFBIG/EPERM and not filling of the shared filesystem.

## What Was Not Checked

Power loss/reboot of real storage, fsck/replay of JFFS2/UBIFS,
metadata durability and byte-level corruption of the medium; real kernel nft/ip,
full LAN/DNS/VPN traffic, Xray/nfqws/procd on OpenWrt and a physical router;
the hourly updater; real B2/B3 VMs; absence of all possible hardware/SDK
failures. HTTPS/parser/preflight are not presented as a full network exchange.
The two diffs and the runtime-rollback draft are proposals without application or validation.

## SHA256 of Guarded Files Before and After

Manifest `reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json` contains **327**
production files. Before S2: **327/327** matched. After: **327/327** matched;
changes relative to the start of S2 — **0**; missing files — **0**.
SHA256 of the manifest itself: `09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.

Full expected and actual hashes:
`frozen-before.json` (local stand artifact),
`frozen-after.json` (local stand artifact).
Two Go tests, three S2 runners, and this unique report are the added test files;
all other entries live in `build/agent-audit/S2/`.
