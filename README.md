# Nadkop

[![CI](https://github.com/REALalphas/nadkop/actions/workflows/ci.yml/badge.svg)](https://github.com/REALalphas/nadkop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/REALalphas/nadkop)](https://github.com/REALalphas/nadkop/releases)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)

Selective routing for OpenWrt: matching domains go through a proxy (Xray), DPI-blocked ones go through circumvention (nfqws/Zapret), everything else goes direct. Rules are `direct` / `proxy` / `block` / `zapret`, multiple subscriptions are supported, configuration lives in the LuCI web UI (Services → nadkop).

## Install

```sh
uclient-fetch -O /tmp/get-nadkop.sh https://github.com/REALalphas/nadkop/releases/latest/download/get-nadkop.sh && sh /tmp/get-nadkop.sh
```

Then open LuCI → Services → nadkop, add a subscription and enable the rules you need.

## Why run it

- Open blocked sites without pushing all traffic through a VPN: only the domains in your rules get proxied.
- Block ads and trackers at the router — once, for every device on the LAN.
- Bypass DPI blocks without installing a circumvention client on each device.

## Learn more

- `docs/openwrt-vm.md` — detailed installation and verification on a test VM.
- `docs/product-contract.md` — what the product can and cannot do.
- `docs/interface.md` — UI and rules; `docs/zapret-tuner.md` — DPI strategy tuner.
- `examples/config.json` — sample configuration for local checks (`AGENTS.md` has the commands).

## License

AGPL-3.0-or-later, see [LICENSE](LICENSE).

Copyright (C) 2026 REALalphas.

Third-party components retain their upstream licenses:

- nfqws/Zapret: MIT; its license is installed in
  `/usr/share/licenses/nadkop-nfqws/`.
- Lucide icons: ISC; see [LICENSE.lucide](openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/LICENSE.lucide).
- flag-icons: MIT; see [LICENSE.flag-icons](openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/LICENSE.flag-icons).
- Xray-core, OpenWrt SDK packages, LuCI base and geodata: their upstream
  licenses apply. Geodata sources are listed in `internal/core/geodata.go`.

Contributions: see [CONTRIBUTING.md](CONTRIBUTING.md).
