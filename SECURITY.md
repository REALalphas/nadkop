# Security Policy

## Supported Versions

Only the latest release (see `CHANGELOG.md`) receives security fixes.
Supported targets: OpenWrt 25.12 (APK) and 24.10 (IPK).

## Reporting a Vulnerability

Use [GitHub private vulnerability reporting](https://github.com/REALalphas/nadkop/security/advisories/new)
when enabled. If unavailable, request a private contact channel from the
[maintainer](https://github.com/REALalphas), without publishing vulnerability
details. Include the following in the private report:

- affected version (core / LuCI revisions),
- OpenWrt release and architecture,
- steps to reproduce or a minimal config (remove subscription URLs and keys).

The maintainer aims to acknowledge reports within 7 days. Once fixed, the advisory and
credit (if desired) are published in `CHANGELOG.md`.

## Scope

In scope: the Go daemon (`cmd/`, `internal/`), the OpenWrt packaging
(`openwrt/`), the installer scripts (`scripts/install-openwrt.sh`,
`scripts/get-nadkop.sh`) and the LuCI app (`openwrt/luci-app-nadkop/`).

Out of scope: the private test VM setup (`docs/openwrt-vm.md` describes a
local stand, not production), upstream components (Xray-core, zapret/nfqws,
OpenWrt itself) — report those upstream.
