# Checking on an OpenWrt VM (testbed)

A dedicated libvirt/KVM testbed for checking the nadkop service on live OpenWrt.
Run network tests only on such an isolated VM, never on a home
router. Package change history is in `CHANGELOG.md`; below is the
reproducible procedure, not a chronicle of every revision.

Sample testbed configuration:

- VM: `nadkop-openwrt` (name is a sample), OpenWrt 25.12.x, x86_64.
- Resources: 2 vCPUs, 2 GiB RAM, 4 GiB disk (extended ext4 root).
- Disk: `<path-to>/openwrt.raw` (sparse raw).
- Shared network `eth0`: `203.0.113.10/24`, gateway `203.0.113.1`, address via DHCP,
  MAC `02:00:00:00:00:01`.
- Host-side management `eth1`: `192.0.2.10/24` via the libvirt `default` network,
  gateway `192.0.2.1`, MAC `02:00:00:00:00:02` (address pinned in libvirt DHCP).
- DHCP server and IPv6 RA on the VM interfaces are off: the VM must not change
  the main router or hand out addresses to home devices.

## Access

From other devices on the shared network, LuCI is at `http://203.0.113.10/`,
the nadkop UI at **Services → nadkop**, direct link like
`http://203.0.113.10/cgi-bin/luci/admin/services/nadkop`.

Macvtap gives no connection between the VM and the host's parent interface,
so from the host itself use the management address:

```sh
ssh -i ~/.ssh/nadkop_vm_key root@192.0.2.10
```

The `~/.ssh/nadkop_vm_key` key is a sample path: generate your own (`ssh-keygen`)
and bake the public key into the image during preparation. SSH accepts keys
only. The root password is locked; to log into LuCI set a password
with the interactive `passwd` command over SSH (this does not enable SSH password
authentication).

Managing the virtual machine:

```sh
virsh -c qemu:///system list --all
virsh -c qemu:///system console nadkop-openwrt
virsh -c qemu:///system shutdown nadkop-openwrt
virsh -c qemu:///system start nadkop-openwrt
```

Console exit: `Ctrl+]`. Live shared-network address inside the VM:
`ubus call network.interface.lan status`. Never run a second instance
with the same MAC/IP at the same time.

## Image

Use the official archives at
`https://downloads.openwrt.org/releases/<version>/targets/x86/64/`:
`openwrt-<version>-x86-64-generic-ext4-combined.img.gz` and the matching
SDK archive. Check both SHA256 against the published `sha256sums`.

`scripts/prepare-openwrt-vm.py` expands a **copy** of the image and its second partition
(e2fsck/resize2fs), adds network settings and the public SSH key via
debugfs without root mounts. The source image is untouched; the script refuses to overwrite an existing
target disk. The script targets a
two-partition MBR ext4 combined x86 image with two virtio interfaces.

```sh
python3 scripts/prepare-openwrt-vm.py <path-to>/openwrt-base.img \
  <path-to>/openwrt.raw --work /tmp/nadkop-vm \
  --tools <path-to>/e2fsprogs/bin --size-gib 4
```

## SDK and package builds

Build with the SDK of the same release and target as the router (25.12 → APK,
24.10 → IPK):

```sh
./scripts/build-openwrt.sh /path/to/sdk /path/to/output
```

Feeds are pinned to the SDK's source commits. For a fresh build it is easier
to keep the SDK outside the Go project tree so Go does not walk the downloaded
SDK sources on `go test ./...`. On NixOS, missing tools:
`nix-shell -p ncurses unzip wget file pkg-config`. If SDK dependency downloads
hang, add the `https://sources.openwrt.org` mirror
to `scripts/localmirrors` (see the build script).

## Installing the three packages

From the host (the image's Dropbear speaks SCP, `-O` is needed):

```sh
scp -O -i ~/.ssh/nadkop_vm_key \
  nadkop-<version>.apk nadkop-nfqws-<version>.apk luci-app-nadkop-<version>.apk \
  scripts/install-openwrt.sh scripts/test-openwrt.sh scripts/test-openwrt-rpc.sh \
  root@192.0.2.10:/tmp/
```

Inside OpenWrt:

```sh
sh /tmp/install-openwrt.sh /tmp/nadkop-nfqws-<version>.apk /tmp/nadkop-<version>.apk /tmp/luci-app-nadkop-<version>.apk
apk add curl
sh /tmp/test-openwrt.sh
sh /tmp/test-openwrt-rpc.sh
```

The test targets a **dedicated VM**: it temporarily stops nadkop,
changes config/hosts, and raises local HTTP/SOCKS servers; on
exit it restores files and service state. `curl`, `uhttpd`,
`jsonfilter` are needed (the last two already ship in the LuCI image).

What is checked:

- `nadkop validate`, node loading, `nadkop check` / Xray preflight
  (`Xray Configuration OK`);
- procd manages Xray and nfqws; nfqws runs under an unprivileged UID;
- HTTP via SOCKS: direct, proxy via a second SOCKS server, block;
- with the upstream stopped, proxy is not silently replaced by direct;
- NFQUEUE 200 attached; off-hostlist HTTPS passes with no queue loss;
- restart creates a new generation, stop removes processes and the nftables table;
- direct-only config works with no nfqws and no nadkop table;
- LuCI: five tabs (**Services → nadkop**) open, RPC get/status,
  revisions, Xray check, restart/stop; an anonymous ubus session has
  no access to credential-bearing get.

After the tests, enable nadkop autostart and check a guest reboot:
the shared-network address persists, Xray and nfqws start via procd, queue
200 reattaches. Autostarting the VM itself on host boot is optional.

Testbed config after checking: default direct,
demo zapret/block domains, WAN device is the shared-network
interface. The image carries no real subscriptions or VPN credentials; for proxy
checks the test raises a temporary local upstream. Do not run
smoke scripts while editing LuCI.

## Current check example

Installing the core `0.3.0-r6` + LuCI `0.3.0-r11` bundle (package SHA256
checked against the local manifest; earlier revisions see `CHANGELOG.md`):

- sources matched the build manifest, 0 differences; `go vet ./...`, build, and `nadkop validate` passed locally;
- the candidate was checked in isolation: `apk extract`, `generate` with copies of
  `config.json`, `servers.json`, and geodata produced a 91-outbound object,
  Xray answered `Configuration OK`;
- after install, SHA256 of `config.json`, `nodes.json`, and `hwid` match
  the pre-upgrade state; the server catalogue updated normally, rule
  links resolve; `config.json`, `nodes.json`, `hwid`, `servers.json`
  are 0600 and listed in conffiles / `sysupgrade -l` (keep.d);
- Xray, nfqws, and monitor run under procd, NFQUEUE 200 and the
  `inet nadkop` table in place, autostart on; LuCI statics serve
  HTTP 200, the page itself without a session correctly returns 403;
- the UI was checked for focus-preserving polling: background polling every 2 s
  does not set `disabled` on inputs, the diagnostics list redraws
  only on state-signature change.

This check confirms install, node loading, and config
correctness, but not every external server's reachability, nor
bypass effectiveness. Transparent LAN interception and zapret effectiveness
on blocked resources are not covered by this test.

## Typical pitfalls

- **`ui.lock` race.** The installer runs `nadkop apply` but may
  lose the race to the service start from the post-upgrade hook. Symptom: a
  lock error in the install log with the service live on the new generation.
  Fix: a single repeated `nadkop apply`; this is expected behavior,
  not a build bug.
- **Flow offloading on.** Hardware/software acceleration bypasses
  nftables/NFQUEUE: zapret and selective routing silently do nothing.
  Turn offloading off on the interfaces carrying nadkop traffic.
- **Port 1080 busy.** Nadkop's local SOCKS listens on 1080 by default;
  a second SOCKS/proxy on the same port breaks smoke tests
  (direct/proxy/block) and diagnostics. Keep the port free or
  split instances across ports.
- **`/var` is a symlink to `/tmp`.** On OpenWrt `/var/run/nadkop` resolves
  to `/tmp/run/nadkop`; if the monitor compares the post-`EvalSymlinks` path
  against the literal `/var/run/nadkop`, `monitor-status.json` may never be
  created even though the monitor itself runs. Affects diagnostics display only.
- **Upgrades keep state.** On package upgrades the SHA256 of
  `config.json`, `nodes.json`, `hwid` must persist, a disabled
  service must stay disabled; the new sample config lands next to it
  as `config.json.apk-new`. Keep state backups before upgrading
  local and unpublished (they hold subscription secrets).
- **A separate `xray` service interferes.** If the image has its own
  `xray` service, disable it: nadkop manages processes via procd.

## Local artifacts

The `build/` directory is git-ignored: packages, SDKs, VM descriptions (`domain.xml`),
logs (`*.log`), and check totals (`verification.json`) live only
locally and may lag behind sources — rebuild packages after changes.
The image-preparation scratch dir is `/tmp/nadkop-*`.
Never copy private backups, subscription URLs, passwords, or subscription contents into docs
and build artifacts.
