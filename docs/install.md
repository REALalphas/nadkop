# Install Nadkop

## One command

Run on the router as root. The one-liner fetches `get-nadkop.sh`
from the latest GitHub release and runs it; the script's default
`BASE_URL` already points at that same release, so no arguments
are needed:

```sh
uclient-fetch -O /tmp/get-nadkop.sh https://github.com/REALalphas/nadkop/releases/latest/download/get-nadkop.sh && sh /tmp/get-nadkop.sh
```

The script detects `apk` vs `opkg`, downloads the pinned release
(core `0.3.0-r6`, LuCI `0.3.0-r13`, nfqws `2026.11.01-r1`), verifies
SHA256 checksums for the packages and installer, and runs the installer. Override the release with
`NADKOP_CORE`, `NADKOP_UI`, `NADKOP_NFQWS` env vars; pass the CPU
architecture as the second argument if it differs from `uname -m`.
To install from another location, pass its URL explicitly —
it must host `get-nadkop.sh`, `install-openwrt.sh`, `sha256sums`
and the packages (same layout as the release assets):

```sh
sh /tmp/get-nadkop.sh BASE_URL [ARCH]
```

## Manual install

1. Pick packages matching the router: OpenWrt **25.12** needs `.apk`,
   **24.10** needs `.ipk`; the architecture and package ABI must match too.
   Published targets are `x86_64` and ARM64 `aarch64_generic` (asset suffix
   `aarch64`). Check `opkg print-architecture` or `apk --print-arch` on
   the router. Other ARM64 ABIs and other architectures require a matching
   SDK build; changing the filename suffix does not change package compatibility. Download them from the GitHub release
   (`https://github.com/REALalphas/nadkop/releases/latest`) together
   with `sha256sums` and `install-openwrt.sh`.
2. Copy the three packages (`nadkop`, `nadkop-nfqws`,
   `luci-app-nadkop`) plus `sha256sums` and `install-openwrt.sh`
   to the router, e.g. into `/tmp/` (names below are the x86_64
   release assets; on other architectures the arch suffix differs):

   ```sh
   scp nadkop-0.3.0-r6-x86_64.apk nadkop-nfqws-2026.11.01-r1-x86_64.apk luci-app-nadkop-0.3.0-r13.apk sha256sums install-openwrt.sh root@<router-ip>:/tmp/
   ```

3. On the router, verify checksums for the three packages and installer:

   ```sh
   cd /tmp && awk '$2 == "nadkop-0.3.0-r6-x86_64.apk" || $2 == "nadkop-nfqws-2026.11.01-r1-x86_64.apk" || $2 == "luci-app-nadkop-0.3.0-r13.apk" || $2 == "install-openwrt.sh"' sha256sums > nk.sums && sha256sum -c nk.sums
   ```

4. Install:

   ```sh
   sh /tmp/install-openwrt.sh /tmp/nadkop-nfqws-2026.11.01-r1-x86_64.apk /tmp/nadkop-0.3.0-r6-x86_64.apk /tmp/luci-app-nadkop-0.3.0-r13.apk
   ```

   Confirm that `nk.sums` contains all four entries before installing.

   For opkg-based systems use the `.ipk` files:
   `nadkop_0.3.0-r6_x86_64.ipk`, `nadkop-nfqws_2026.11.01-r1_x86_64.ipk`,
   `luci-app-nadkop_0.3.0-r13_all.ipk`.

5. Open LuCI → **Services → nadkop**, add a subscription and enable
   the rules. First-time setup notes (LAN device, flow offloading,
   WAN interface name) are printed by the installer at the end.

## Upgrading

Back up `/etc/nadkop/` before upgrading; it contains subscription credentials
and the device identity. Keep the backup private. The installer preserves an
existing service's enabled and running state; a fresh install remains stopped
until configured. After an upgrade, check LuCI diagnostics and test the rules
from a LAN client.

## Coexistence with Podkop

Podkop (sing-box based transparent proxy) and Nadkop can be installed
side by side — nft tables (`inet nadkop` vs `inet PodkopTable`), policy
routes (table 100 vs 105/podkop) and LuCI pages do not collide — but
their **transparent modes are mutually exclusive**: both claim the
`198.18.0.0/15` FakeDNS range, both hook prerouting/mangle, both
intercept port 53. Enabling Nadkop transparent routing while Podkop
interception is live is refused by the pre-apply check (`nadkop
validate` / check / LuCI diagnostics report the reason).

Safe combinations:

- Nadkop transparent **off** + Podkop active: works. A `coexistence`
  warning in diagnostics reminds that Podkop's sing-box upstream TLS
  passes Nadkop's Zapret NFQUEUE — list Podkop VPN endpoints in Zapret
  bypass prefixes or disable Zapret rules if Podkop tunnels break.
- Nadkop transparent **on** + Podkop fully stopped (no `PodkopTable`,
  no `fwmark 0x100000` policy route, dnsmasq restored): works.
- A merely installed but idle Podkop neither blocks nor warns.

Nadkop never rewrites Podkop settings (sing-box config, dnsmasq,
UCI `podkop` section).

## Build from source

Needs the OpenWrt SDK of the same release and target as the router
(25.12 → APK, 24.10 → IPK):

```sh
./scripts/build-openwrt.sh /path/to/sdk /path/to/output
```

Details: `docs/openwrt-vm.md`.
