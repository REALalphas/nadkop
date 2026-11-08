# B3 — install, upgrade, and boot with TPROXY

**Status: closed for the specified x86_64 VM matrix.** On OpenWrt 24.10.8/IPK and
25.12.6/APK, clean install, upgrade, guest `reboot`, and a full
power-off/restart cycle with autostart enabled and TPROXY were performed. Real
network packets inside the VM, E02/E03, and mistaken-`start` protection were verified.
No new confirmed application defects were found; failed first stand runs
and their causes are listed below. This is not confirmation of a physical router or
external VPNs.

Test date: 2026-11-02. Product: core **0.3.0-r4**, LuCI **0.3.0-r8**,
nfqws **2026.11.01-r1**. All logs and extra tests listed below
are in ``build/agent-audit/B3/`` (local stand artifact).

## Matrix results

| Check | OpenWrt 24.10.8 IPK | OpenWrt 25.12.6 APK |
|---|---|---|
| Clean install and dependencies | Passed, official release feed and kernel 6.6.144 kmods; Xray **25.1.30-r1** | Passed, official release feed and kernel 6.12.112 kmods; Xray **26.10.21-r1** |
| Initial state | No sources, nodes, or server catalogue entries; service disabled and stopped | Same |
| Upgrade from 0.2.0-r34 | Core to r4, LuCI to r8; four files preserved byte-for-byte; enabled/running and disabled/stopped states verified separately | Same |
| Real LAN namespace + Zapret | 36/36; extra NFQUEUE test — 39/39 | 36/36; extra NFQUEUE test — 39/39 |
| Guest `reboot` with autostart | Boot ID changed; 18/18 | Boot ID changed; 18/18 |
| Guest `poweroff` and fresh QEMU launch | Boot ID changed; final run 19/19 after fixing test NDP | Boot ID changed; final run 19/19 |
| E02, E03, mistaken start | Repeat targeted checks passed | Repeat targeted checks passed |
| Xray API/server selection | Actual 25.1.30 from feed: configuration, API, and failover verified | Actual 26.10.21 from feed: same checks |

The install logs show dependencies fetched from release-specific paths.
Published checksums were verified for both images, and for the six target
packages — before and after the tests. `opkg` verified index signatures; local APKs from the
SDK were installed with the intended `--allow-untrusted`, while dependencies came from
official repositories. The 24.10.8 image matched the official SHA256
`23872c64fdb66d0765e0d17f71453a66e7a91e9ecb10606d5f738e6d166e14ae`;
25.12.6 — `f8538784d0288b4133a99ac2a68443e4922ffde6343e80df56a5882364b9d499`.

The upgrade was performed with the stock `scripts/install-openwrt.sh` in separate clean
VMs with the old version. A local
synthetic node, a non-empty server catalogue, a rule, and a random test
HWID were created in `/etc/nadkop/` in advance. `cmp -s` verified exact equality of `config.json`, `nodes.json`,
`servers.json`, `hwid` before and after the upgrade without printing values or hashes.
One VM set was upgraded with the service running and enabled, the second — with it
stopped and disabled. In IPK, opkg logs contain `Collected errors` about
`resolve_conffiles`: new package copies were written with the `-opkg` suffix,
installation completed successfully, and user files were preserved. APK
likewise left `.apk-new`.

## What was verified on the network and after boot

Inside each VM, separate client and server network namespaces are connected by
veth with real IPv4/IPv6 addresses. The lab network, synthetic
DNS, and local HTTP/UDP/SOCKS servers were started by a separate **test** init
`B3-lab` before Nadkop started; IPv6 NDP resolution for these two artificial
interfaces was provided by a separate test firewall4 include. After
guest boot, Nadkop, its rules, or routes were not applied manually.

Verified: service enablement, procd Xray/nfqws, queue 200 attachment, nft
rules `tproxy ip` and `tproxy ip6`, input allowance in firewall4, rules and
local routes of table 100 for IPv4 and IPv6. From the LAN client with no
SOCKS configured, direct HTTP, HTTP via the local SOCKS upstream, blocking,
UDP via FakeDNS, and IPv6 HTTP and DNS passed. After cold boot, DNS over IPv6 via UDP and TCP was verified separately,
including interception of a query to an external DNS address.
The extra 39-point test with `fake,badseq` observed the NFQUEUE counter change
under Zapret (24.10: 0→6; 25.12: 0→11), while direct/proxy left it
unchanged. Firewall reload, fail-closed proxy, and network rule removal on
stop were also covered by this test.

On Xray **25.1.30-r1**, installed specifically from the 24.10.8 feed, the configuration
passed `xray run -test`; a separate test verified monitor and Xray
RoutingService API calls (`api bo` and `api bi`), selection of two local SOCKS servers,
failover, and blocking when both failed. FakeDNS, IPv4/IPv6
TPROXY, and TCP/UDP worked in the network runs on the same engine. The same was done
on 25.12 with Xray 26.10.21. No Xray replacement was needed.

For E02, a new private generation 0700 with a non-empty hostlist 0600 was created;
the real `nadkop network check` ran the nfqws dry-run and nft preflight, and
the working nfqws had UID 2147483647. For E03, explicit IPv4/IPv6 families
in the generated rules and a successful nft check were verified, then live traffic. On
a mistaken `start` with a port conflict, the generation reference and Xray PID were preserved,
and direct and proxy HTTP kept working. Results: `24/regressions-retry.log`
and `25/regressions-retry.log`.

## Main commands and evidence

Images and packages were verified with `sha256sum` against the official `sha256sums`;
package manifest verification was run from the corresponding directories:

```sh
cd build/packages/verified-resume/apk-x86_64 && sha256sum -c sha256sums
cd build/packages/verified-resume/ipk-x86_64 && sha256sum -c sha256sums
```

New disks were prepared from the official ext4 combined image with separate
keys via a local adaptation of the reviewed `scripts/prepare-openwrt-vm.py`:

```sh
python3 build/agent-audit/B3/B3-prepare-openwrt.py SOURCE.raw DEST.raw \
  --work WORK_DIR --tools /nix/store/k79ra63k4vr2srkwvjrqcppm5pamac5n-e2fsprogs-1.47.4-bin/bin \
  --size-gib 2
qemu-system-x86_64 -name B3-openwrt-24 -enable-kvm -cpu host -smp 2 -m 1024 \
  -drive file=DEST.raw,format=raw,if=virtio \
  -device virtio-net-pci,netdev=n0,mac=02:00:00:00:00:01 \
  -netdev user,id=n0,net=198.18.0.0/15,hostfwd=tcp:127.0.0.1:40224-:22 \
  -display none -monitor none -serial file:CONSOLE.log -pidfile PIDFILE -daemonize
```

Separate disks, MACs, networks, and ports 40225,
40324/40325, and 40424/40425 were used likewise. The following commands were run in the guests:

```sh
sh /tmp/install-openwrt.sh /tmp/nadkop-nfqws_PACKAGE /tmp/nadkop_PACKAGE /tmp/luci-app-nadkop_PACKAGE
sh /tmp/B3-test-transparent.sh zapret
sh /tmp/B3-test-selection.sh
sh /tmp/B3-test-zapret-badseq.sh
sh /tmp/B3-regressions.sh
reboot
sh /etc/B3-boot-check.sh
poweroff
# New QEMU process with the same dedicated guest disk, then:
sh /etc/B3-boot-check.sh
```

`B3-test-transparent.sh` and the two queue tests are copies/adaptations of existing
tests under separate names; `B3-lab-init`, `B3-ndp.nft`,
`B3-boot-config.json`, `B3-boot-check.sh`, `B3-regressions.sh`, and
`B3-upgrade-seed.sh` are standalone test files. Stand setup steps,
configuration content, and checks are given in them; full outputs require no
disclosure of the test HWID or keys.

Main logs in the artifact directory:

- `24/install-fresh.log`, `25/install-fresh.log`, `*/fresh-state.log`,
  `apk-package-hashes.log`, `ipk-package-hashes.log`;
- `24-upgrade/upgrade.log`, `25-upgrade/upgrade.log`,
  `24-disabled/upgrade.log`, `25-disabled/upgrade.log`, as well as
  `24-upgrade/strict-state.log` and `25-upgrade/strict-state.log`;
- `24/transparent-zapret-retry.log`, `25/transparent-zapret-retry.log`,
  `24-upgrade/nfqueue-badseq.log`, `25-upgrade/nfqueue-badseq.log`;
- `24/api-selection.log`, `25/api-selection.log`, `*/regressions-retry.log`;
- `*/reboot-check.log`, `*/cold-boot-fixed.log`, `*/ipv6-dns.log`;
- `integrity-before.json`, `integrity-after.json`, `cleanup.log`.

## Saved failed runs and limits

- The first network runs `*/transparent-zapret.log` exited 2:
  the clean image lacked the **test** `kmod-veth`. After installing the module
  from the release feed, the retry gave 36/36. This is a stand-modeling module.
- The first `*/boot-fixture-setup.log` runs gave 15/18: the test DNS for local names
  had not been added to the VM's own resolver. After fixing only the lab
  init, the result was 18/18 (`*/boot-fixture-retry.log`).
- The first 24.10 cold run gave 17/18: IPv6 did not come up. The
  `24/cold-ndp-debug.log` log showed the missing temporary B3-NDP rule and
  neighbor state `FAILED`; firewall4 reloaded rules and removed the manually
  inserted test allowance. On 25.12 the first cold run gave 18/18.
  A permanent **test** include 91-B3-ndp in both VMs removed this cause;
  after a fresh full power-off and launch, 19/19 was achieved on both versions.
- The first `24-upgrade/seed.log` and `25-upgrade/seed.log` rejected the
  `updateIntervalHours` field, missing in the old version. It was removed from the
  **test** input configuration; repeat preparation and upgrade passed.
  The first `*/regressions.log` runs stopped because `stat` was missing from the images;
  the test replaced the mode check with `ls`, and the retry passed.
- `gzip -dc` of the official 24.10.8 image printed `decompression OK, trailing
  garbage ignored` and code 2; SHA256 matched the official one, and the extracted image
  booted in three separate VMs. The first manual verification commands from the wrong
  working directory also failed with a path error; retries from the correct
  directories passed. On the first mistaken command, empty directories were temporarily
  created in the APK directory, then removed; package files were unchanged.

LAN and sources, including local SOCKS, are synthetic. A physical router,
real external DNS/VPN, and effectiveness against a specific operator's DPI were not
tested. Hardware compatibility and universal Zapret effectiveness do not follow from
this result.

## Integrity and cleanup

All **327** files from
`reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json` were verified before and after: no changed or
missing files; manifest SHA256
`09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.
All six packages matched their manifests again. Production files,
recipes, README, shared SDKs, and existing reports were not edited.

All six created QEMU guests were stopped via guest `poweroff`; no processes named
B3 remain in `/proc`. The original `nadkop-openwrt` VM stayed running; its disk,
guest configuration, and network were not used. QEMU user networking created no
host network objects. Only B3's own disks/intermediate copies and
temporary private B3 SSH keys were deleted; logs and tests are preserved. No patch was proposed,
since no confirmed application defect was found.
