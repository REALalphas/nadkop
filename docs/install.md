# Install Nadkop

## One command

Run on the router as root. `BASE_URL` is a directory that hosts
`get-nadkop.sh`, `install-openwrt.sh`, `sha256sums` and the packages
(same layout as `build/packages/apk-x86_64/`):

```sh
uclient-fetch -O /tmp/get-nadkop.sh BASE_URL/get-nadkop.sh && sh /tmp/get-nadkop.sh BASE_URL
```

The script detects `apk` vs `opkg`, downloads the pinned release
(core `0.3.0-r6`, LuCI `0.3.0-r11`, nfqws `2026.11.01-r1`), verifies
SHA256 checksums and runs the installer. Override the release with
`NADKOP_CORE`, `NADKOP_UI`, `NADKOP_NFQWS` env vars; pass the CPU
architecture as the second argument if it differs from `uname -m`.

No package hosting exists yet, so today `BASE_URL` is your own file
server (or a copy of `build/packages/apk-x86_64/` / `ipk-x86_64/`
served over HTTP). Once hosting appears, this page will point at it.

## Manual install

1. Pick packages matching the router: OpenWrt **25.12** needs `.apk`,
   **24.10** and older need `.ipk`; the architecture must match too
   (`x86_64`, `aarch64`, …). Ready-made x86_64 builds live in
   `build/packages/apk-x86_64/` and `build/packages/ipk-x86_64/`,
   each with `sha256sums` and `install-openwrt.sh`.
2. Copy the three packages (`nadkop`, `nadkop-nfqws`,
   `luci-app-nadkop`) plus `sha256sums` and `install-openwrt.sh`
   to the router, e.g. into `/tmp/`:

   ```sh
   scp nadkop-0.3.0-r6.apk nadkop-nfqws-2026.11.01-r1.apk luci-app-nadkop-0.3.0-r11.apk sha256sums install-openwrt.sh root@<router-ip>:/tmp/
   ```

3. On the router, verify checksums (only for the three files):

   ```sh
   cd /tmp && grep -E "nadkop-0.3.0-r6.apk|nadkop-nfqws-2026.11.01-r1.apk|luci-app-nadkop-0.3.0-r11.apk" sha256sums > nk.sums && sha256sum -c nk.sums
   ```

4. Install:

   ```sh
   sh /tmp/install-openwrt.sh /tmp/nadkop-nfqws-2026.11.01-r1.apk /tmp/nadkop-0.3.0-r6.apk /tmp/luci-app-nadkop-0.3.0-r11.apk
   ```

   For opkg-based systems use the `.ipk` files:
   `nadkop_0.3.0-r6_x86_64.ipk`, `nadkop-nfqws_2026.11.01-r1_x86_64.ipk`,
   `luci-app-nadkop_0.3.0-r11_all.ipk`.

5. Open LuCI → **Services → nadkop**, add a subscription and enable
   the rules. First-time setup notes (LAN device, flow offloading,
   WAN interface name) are printed by the installer at the end.

## Build from source

Needs the OpenWrt SDK of the same release and target as the router
(25.12 → APK, 24.10 → IPK):

```sh
./scripts/build-openwrt.sh /path/to/sdk /path/to/output
```

Details: `docs/openwrt-vm.md`.
