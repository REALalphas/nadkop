**S3 — Backup and Recovery of User State**

Checked frozen core **0.3.0-r4** and LuCI **0.3.0-r8**, locally on Linux
x86_64, Go 1.26.10, GNU tar 1.35, Python 3.13.17. Date: 2026-11-02 UTC.
Nadkop has no dedicated backup/restore API/CLI; the checks covered preservation of declared
files, unpacking a synthetic archive into a new fixture, public core APIs, and
the real CLI validate/generate with explicit temporary paths.
**No real sysupgrade, package installation, OpenWrt boot, or VPN network exchange
was performed.**

The full set was restored successfully: **3/3 of the same server IDs**, one single
rule, a group of two IDs, two pins, the order of three servers, HWID,
disabled fields, and two geodata generations. Negative checks found
**S3-F01 — catalogue regeneration on partial restore** and **S3-F02 — geodata
reads through symlinks outside the root**. Acceptance of wrong mode/UID is recorded separately as
conditional notes S3-F03/F04. A missing HWID is restore-recognition limitation
S3-R01, identical to regular first-time initialization.

**What Changed in the Workspace**

Only its own [test](../../internal/core/s3_backup_test.go),
[runner](../../scripts/test-s3-backup.py), this report, and
`S3 artifacts` (local stand artifact) were created. All test functions carry the
TestS3 prefix; test publication was done via atomic rename. Other tests/runners, shared
reports, README, recipes, go.mod/go.sum, SDK, and production were not modified.
There was no SSH, no VM/other-process control, and no firewall/routes/sysctl/
clock commands. Real backups, subscriptions, credentials, and the host /etc/nadkop
were not used. No subagents/delegates/PRs/messages to other people were created.

**Reproducible Commands and Measurements**

Final run:

```sh
python3 scripts/test-s3-backup.py --label s3-verified
```

The runner executes:

```sh
go build -p 1 -o "$S3_NADKOP_BIN" ./cmd/nadkop
go test -p 1 -vet=off ./internal/core -run '^TestS3' -count=1 -parallel=1 -timeout 90s -json
```

The second binary/cache argument is set by the runner through its own temporary
directory. Variables: GOMAXPROCS=2, GOFLAGS=-mod=readonly, GOTOOLCHAIN=local,
GOPROXY=off, GOSUMDB=off, dedicated GOCACHE/GOTMPDIR. Limited: single-file size
128 MiB, core dump=0, wall timeout 180 s for build / 240 s for test subprocess;
private tmpfs for owners — 8 MiB. This is not a project-wide run.
New safety assertions are intentionally not suppressed: final **exit code 1**.

Measured: build+test+parsing **24.483 s**, the Go package itself
**2.453s**, child-process max RSS
**281152 KiB** (this is a maximum, not a sum and not
router memory). After private-cache removal the S3 artifacts occupied about
412 KiB; the final synthetic archive was **1798 bytes**.
Intermediate runs s3-main/s3-final are kept; final results and
test SHA256 refer to **s3-verified**.

Total **16 top-level TestS3: 10 PASS, 6 FAIL, 0 SKIP**.
The Go JSON contains **62 results: 36 PASS / 26 FAIL / 0 SKIP**, including
parent functions; these are not 62 independent scenarios. Among them are 46 named
subtests (26 PASS / 20 FAIL). Another **8 owner-subtests** inside a separate
namespaced subprocess are reflected as log text, not additional JSON events.
All 8 detected acceptance of a foreign owner. The overall FAIL includes the desired-behavior
check for partial-HWID, not only confirmed contract defects.

| Test | Result | Seconds |
| --- | --- | --- |
| [TestS3FullBackupRestore](../../internal/core/s3_backup_test.go:288) | PASS | 0.05 |
| [TestS3MissingServerReturnAfterRestore](../../internal/core/s3_backup_test.go:378) | PASS | 0.09 |
| [TestS3PartialCatalogueMustNotRegenerateBoundIDs](../../internal/core/s3_backup_test.go:433) | FAIL | 0.55 |
| [TestS3CorruptCatalogueRejectedWithoutRewrite](../../internal/core/s3_backup_test.go:494) | PASS | 0.25 |
| [TestS3GroupOnlyPartialCatalogueBlocksUnresolvedMembers](../../internal/core/s3_backup_test.go:522) | PASS | 0.07 |
| [TestS3HWIDCorruptionAndPermissionRepair](../../internal/core/s3_backup_test.go:558) | PASS | 0.19 |
| [TestS3PartialHWIDCannotSilentlyReinitialize](../../internal/core/s3_backup_test.go:593) | FAIL | 0.13 |
| [TestS3FreshInstallationBootstrap](../../internal/core/s3_backup_test.go:617) | PASS | 0.01 |
| [TestS3PartialConfigNodesFailClosed](../../internal/core/s3_backup_test.go:635) | PASS | 0.22 |
| [TestS3PartialGeodataRejectsWithoutFallback](../../internal/core/s3_backup_test.go:697) | PASS | 0.22 |
| [TestS3GeodataArchiveSymlinkBoundary](../../internal/core/s3_backup_test.go:751) | FAIL | 0.19 |
| [TestS3RestoredWeakSensitiveModes](../../internal/core/s3_backup_test.go:818) | FAIL | 0.11 |
| [TestS3CLIGenerateAfterRestore](../../internal/core/s3_backup_test.go:860) | FAIL | 0.09 |
| [TestS3AmbiguousReturnedServersAreNotSubstituted](../../internal/core/s3_backup_test.go:941) | PASS | 0.07 |
| [TestS3SensitiveSymlinksFailClosed](../../internal/core/s3_backup_test.go:1011) | PASS | 0.11 |
| [TestS3RestoredForeignOwners](../../internal/core/s3_backup_test.go:1045) | FAIL | 0.11 |

**Backup Contents and Simulation Fidelity**

Recipe [openwrt/nadkop/Makefile:30](../../openwrt/nadkop/Makefile:30) declares
config.json, nodes.json, hwid, servers.json as conffiles.
[nadkop.keep:1](../../openwrt/nadkop/files/nadkop.keep:1) adds
`/etc/nadkop/geodata/` recursively.
Install paths were statically verified: the keep file installs to
`/lib/upgrade/keep.d/nadkop`, nodes.json gets 0600; the remaining sensitive
conffiles use INSTALL_CONF. Recipe execution/IPK/APK installation was
not checked here.

Only the SDK's original sysupgrade scripts were read, without running/modifying the SDK:
`build/openwrt-vm/sdk/feeds/base_root/package/base-files/files/sbin/sysupgrade`
lines 168–177, 319, 361 and
`build/openwrt-vm/sdk-24.10.8/feeds/base/package/base-files/files/sbin/sysupgrade`
lines 164–173, 295, 337. They use `find ... ( -type f -o -type l )` and
`tar -C / -T list`. The fixture models this list from recipe/keep, including
four deliberately modified synthetic conffiles. Real package-manager
selection by checksum, `-u`, custom sysupgrade.conf, platform_restore_backup,
BusyBox tar, and overlay/flash mechanics were not executed.

The archive contains **10 entries**: four conffiles, geodata/.update.lock,
relative `geodata/current -> generation.*`, two .dat files each in the current and
previous generations. After three InstallGeoFiles, two generations remain.
All nine regular files are **0600**, the symlink is 0777 (for a symlink this is not
content access permission). Bytes and mode of all entries matched after restore.
Tar creates the symlink without including the external target's contents. Archive
entry names are stored in `s3-full-archive-list.txt` (local stand artifact).

`/var/run/nadkop/current` and the runtime generation are not listed in the backup:
in the test they were present before packing and absent after restore.
This is expected ephemeral state. A real `generate` recreated
**10 runtime artifacts with 0600 and a 0700 directory**, preserving 3 IDs.
No init/procd, Xray/nfqws/nft preflight, Apply, or firewall was executed.

Directory headers are absent from the simulated sysupgrade list.
Hence with umask 022 the geodata/generation directories became **0755 instead of 0700**.
Files stayed 0600; this is an observed loss of directory modes under this simulation,
**not a confirmed leak** and not proof of real BusyBox/
platform-restore behavior. The main transfer runs with `--no-same-owner` as regular UID
1000; UID preservation was checked separately in a namespace, described below.

**What Was Successfully Verified**

- A full tar round trip into a new root preserved config/nodes/catalogue/
  HWID/dat bytes, server IDs, pins/order, a single nodeRef, and two group members.
  Unchanged SyncServers did not rewrite the catalogue. The local source loads
  via LoadContext; only its absolute path is translated into the in-memory fixture.
  The synthetic remote source and its disabled HWID/custom fields are preserved
  but not requested over the network. For the real generate CLI, a separate
  copy of the fixture config is created with only the rebased local source.
- Disabled IPs/sets, VPN selection and check URLs, inactive ZapretArgs/
  WANInterface, and disabled source HWID/model survive ReadConfig,
  serialization/reload, and restore. The runtime uses the active fields.
  Re-enabling invalid disabled IPs/sets triggers a validation rejection.
- Two bound vanished servers persisted in the catalogue as missing.
  After the archive round trip, return, rename/reorder, and credential rotation,
  both received their previous IDs; references/pins were not lost.
- Two returned servers with an ambiguous identical endpoint did not take
  the old IDs: they received new ones, the two old records stayed missing and bound;
  compile of the old single nodeRef was rejected. No server substitution was found.
- Malformed catalogue JSON, unsupported version, array instead of object,
  duplicate/invalid IDs were rejected without rewrite (5 variants).
  JSON null is called out as a defect below, not claimed as successfully rejected.
- A missing/empty/truncated config and missing/truncated nodes
  produce an explicit rejection. Empty `nodes=[]` keeps 3 bound IDs as missing;
  a single reference blocks compilation. Groups keep the block selector.
- Without current, with a dangling current, without one of the dat files, or with a corrupted dat,
  OpenGeoDatabase and compile of active sets fail (6 variants); neighboring
  config/nodes/servers/HWID are unchanged. There is no automatic fallback to previous.
- A non-empty corrupted HWID (short, invalid characters, too long)
  is not overwritten; a valid 0600/0644 preserves the ID, mode becomes 0600.
  HWID/nodes/catalogue symlinks are rejected; external synthetic sentinels
  remain unchanged.

**Confirmed Defects and Conditional Notes**

**S3-F01 — High: ID regeneration on an incomplete catalogue with preserved references.**
Condition: the restored config already contains NodeRef/NodeRefs/pins, while servers.json
is missing, empty/whitespace, or contains null, version-only,
null/empty servers, or an incomplete valid list. Fact: SyncServers returns
nil and saves a new catalogue; **7/7 empty/incomplete forms yield 0/3 previous IDs**,
a non-empty subset yields **1/3**. The CLI then exits 1 with no new runtime,
but the catalogue is already replaced. Preserved references/pins are not rewritten, so
they become unresolvable. For group-only compile this is acceptable: old members have
empty Tags, the selector stays block. **Server substitution, direct bypass, and successful
network exchange are not proven and not claimed.** Returning the original full catalogue
remains a way to restore references; the function cannot recover random
old IDs from nodes.json alone.

Causes/paths: [servers.go:49](../../internal/core/servers.go:49) — absence;
[servers.go:66](../../internal/core/servers.go:66) — emptiness and decoding into a pre-
initialized struct, so JSON null passes too;
[servers.go:164](../../internal/core/servers.go:164) — no preserved-reference
completeness check before reconcile;
[servers.go:364](../../internal/core/servers.go:364) — random ID for unmatched;
[main.go:105](../../cmd/nadkop/main.go:105) — SyncServers before CompileWithGeo;
[compile.go:190](../../internal/core/compile.go:190) — single-reference rejection.
Reproduction: TestS3PartialCatalogueMustNotRegenerateBoundIDs,
TestS3CLIGenerateAfterRestore/missing-catalogue.
Proposed: a schema/null check plus presence of all active bindings/pins in
the catalogue **before** reconciliation; no guessing of IDs.
`Unapplied diff` (local stand artifact).

**S3-F02 — Medium: geodata current/files pass through external symlinks.**
Condition: the restore contains an absolute/relative escaping current, a
generation or single-dat symlink, and the target exists with a valid protobuf database.
Result: **5/5** such archives are accepted by OpenGeoDatabase, Expand, and CompileWithGeo;
the domain comes from the external database, and a single dat can mix pair components.
Causes: [geodata.go:240](../../internal/core/geodata.go:240) resolves current
via EvalSymlinks without a root restriction, [geodata.go:247](../../internal/core/geodata.go:247)
opens dat with a plain os.Open. External sentinel files were not modified.
This is proven out-of-root database read, not proof of tar arbitrary-write,
remote execution, or reads of real secrets. With a missing target,
regular rejection was already confirmed by a separate test.
Proposed: relative current to a direct generation.*, opening generations and dat
via dirfd/openat/O_NOFOLLOW, stat of regular files before reading.
`Unapplied diff` (local stand artifact).

**S3-F03 — Medium on restore of unsafe modes; a metadata-policy note.**
Three synthetic archives with config/nodes/servers 0644 were read successfully; mode
stayed 0644. The real validate CLI was used for config, LoadContext for
nodes, SyncServers without content change — for servers.
Paths: [main.go:84](../../cmd/nadkop/main.go:84),
[subscription.go:60](../../internal/core/subscription.go:60),
[servers.go:51](../../internal/core/servers.go:51),
[servers.go:235](../../internal/core/servers.go:235) — unchanged content does not
produce a private replacement. HWID is the positive exception: mode is repaired
to 0600 ([hwid.go:105](../../internal/core/hwid.go:105)).
A healthy 0600 backup was not weakened; insecure input metadata was deliberately introduced
by the test. Another user's access depends on parent-directory permissions;
actual leakage/exploit was not checked. The contract explicitly promises privacy
of the new runtime; this test does not refute it and does not declare a defect in regular installation.

**S3-F04 — Medium on restore of a foreign UID; a conditional file-trust note.**
Four files × two modes: **8/8** files owned by UID/GID 12345 remain
foreign and are readable by EUID 0. They genuinely passed tar restore with
`--same-owner --numeric-owner` in a private tmpfs. HWID repairs 0644→0600,
but not UID; other modes are preserved. Same paths plus
[hwid.go:98](../../internal/core/hwid.go:98). The stat checks have no owner policy.
Root only inside `unshare --user --map-auto --map-root-user --mount
--propagation private`; the tmpfs is limited to 8 MiB and was unmounted. Host UID root
was not used; no OpenWrt ACL/exploit proof by another user is
claimed. The risk is conditional: if on the restored system a file is accessible to a foreign
owner, 0600 does not establish trust in its future contents.

The shared proposed `metadata diff` (local stand artifact)
checks owner and normalizes modes of canonical private state for CLI/RPC/HWID/
catalogue. This is a sketch requiring a policy decision on arbitrary Source.File;
it is not claimed to close all asserts of the shared file-loader test.

**S3-R01 — partial-restore limitation: a lost HWID is indistinguishable from bootstrap.**
A missing/empty/whitespace HWID yields a new ID without error, **3/3** variants.
[hwid.go:110](../../internal/core/hwid.go:110),
[hwid.go:120](../../internal/core/hwid.go:120). DeviceID receives no restore flag;
the package deliberately ships an empty hwid, and a fresh install must create it.
Therefore this is not a separately confirmed bug in the original install contract.
The requirement "partial backup does not reset identity" without a restore manifest
is not met. An explicit restore preflight is needed before state use,
checking the full set and an existing valid HWID, with a separate explicit
reset/new-install path. `Opt-in API design diff, not wired` (local stand artifact).

All diffs are stored separately, were **not applied**, and were **not verified as a fixed
product**. Only gofmt of the proposals was performed. Detailed independent
commands and conditions: `S3-findings-repro.md` (local stand artifact).

**Logs, Artifacts, SHA256**

- `Final raw go test JSONL` (local stand artifact)
  and `readable log` (local stand artifact).
- `Test results/commands/resources/SHA` (local stand artifact),
  `build log` (local stand artifact),
  `environment and archive contents` (local stand artifact).
- `Full synthetic backup` (local stand artifact),
  SHA256 `93eecf349b69d4515f60c71defdf817fb3fe0ce2bd67c13b27cacf3efc684a1e`. No real backup was read.
- `Full verification BEFORE` (local stand artifact),
  `AFTER` (local stand artifact),
  `integrity summary` (local stand artifact).

**327/327** guarded production files verified.
Before: `2026-11-02T17:11:41.063286+00:00`, after: `2026-11-02T17:28:55.500824+00:00`.
Mismatches against frozen-source.json before/after: **0/0**; files changed between verifications: **0**.
SHA256 of frozen-source.json itself is identical before/after:
`09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.
SHA256 of the ordered JSON map of individual file hashes is identical before/after:
`c5a24029805f198f40f21f98822ab385e7561eb4b7c34fc3fcc18cfe70136c`.
SHA256 of the final S3 test source:
`c6edc90ddeac7f557899eee76346746c97a9557367d24411dfc9284baf4b975c`.
This verification covers the manifest's 327 paths, not the entire shared workspace.

**What Was Skipped and Limits of Conclusions**

Not checked: physical router, real package install/upgrade/boot,
real sysupgrade/BusyBox/platform restore, cold boot, flash/fsync under
crash (S2 scope), the full SDK/release/architecture matrix, and UID-mapping effects
on specific OpenWrt. B2/B3 VMs were not used. For arbitrary custom
Source.File entries outside /etc/nadkop/nodes.json, system-backup completeness is not promised.
Not checked: TLS/HTTP/protocol exchanges, Xray run -test, nfqws, nft, procd,
LAN/DNS/IPv6, and working connections. Build/parse/compile checks are not
network tests. File defects and conditional notes are described only under actually
reproduced conditions; successful recovery of the full set does not
substitute for sysupgrade verification on the device.
