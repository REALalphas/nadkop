# Nadkop

Selective routing manager for OpenWrt: Xray-core for proxying, nfqws/Zapret for DPI, `direct`/`proxy`/`block`/`zapret` rules, multiple subscriptions, and a LuCI web interface. Original implementation inspired by Podkop, without `happ://crypto`.

## Layout

- `cmd/nadkop/` — CLI and entry point.
- `internal/core/` — configuration, subscriptions, nodes, Xray generation, and geodata.
- `internal/router/` — networking, nftables, Zapret, diagnostics, and RPC.
- `openwrt/nadkop/` — service package, procd init, and rpcd plugin.
- `openwrt/nadkop-nfqws/` — nfqws build.
- `openwrt/luci-app-nadkop/` — JavaScript LuCI app, ACL, and static assets.
- `examples/` — sample configuration; `scripts/` — builds and tests.
- `docs/` — architecture and VM; `reports/` — check results and known issues.
- `build/` — artifacts, packages, and local testbed, not sources.

## Local checks

Requires Go 1.23+, no external Go dependencies:

```sh
go test ./...
go vet ./...
go build -o build/nadkop ./cmd/nadkop
./build/nadkop validate -config examples/config.json
```

The local CLI is no substitute for running the service on OpenWrt. Full `go test ./...` is green; restore-policy details are in `reports/verification.md`.

## Building and running on OpenWrt

Use the SDK of the same release and target as the router (25.12 → APK, 24.10 → IPK):

```sh
./scripts/build-openwrt.sh /path/to/sdk /path/to/output
```

Install the three packages: `scripts/install-openwrt.sh`. Then configure `/etc/nadkop/config.json`, add a subscription/nodes, and run:

```sh
nadkop validate
/etc/init.d/nadkop start
```

LuCI: **Services → nadkop**. Full install notes are in `docs/install.md`, the network-check testbed in `docs/openwrt-vm.md`. Packages in `build/` may lag behind sources; rebuild them after changes. Run network tests only on a dedicated VM.
