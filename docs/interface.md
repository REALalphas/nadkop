# LuCI interface

Open **Services → nadkop**. The interface follows the selected LuCI theme and
provides five tabs: Rules, Subscriptions, Settings, Import and Diagnostics.

## Rules and servers

Rules are evaluated from top to bottom; reorder them with the arrow controls
or drag handles. Domains, IP/CIDR ranges and geodata sets within a rule are
alternatives. Each condition can be disabled while keeping its saved values.

Choose Direct, VPN, Block or Zapret. VPN rules use one server or a group. Groups
can choose the first available server or the server with the lowest measured
HTTP latency. The URL mode lets you choose the probe endpoint. Failed groups
block traffic rather than silently switching to a direct connection.

The server picker supports search, subscription filters, pins and ordering.
Country flags are bundled with the app and served locally; system emoji provide
a fallback. Server cards show protocol, transport, subscription and probe status.

## Saving

**Save** writes the configuration. **Save and apply** also validates and applies
it to the running service. Pending changes stay in the editor after an error.
**Discard changes** restores the last saved state without writing files.

Field errors expand the affected rule and focus the invalid control. A lost
save response is reported as unconfirmed; it is not treated as a successful
apply. See [interrupted saves](interrupted-save.md) for recovery.

## Settings and diagnostics

Settings cover LAN/WAN devices, subscription updates, Zapret, routing databases
and advanced listener options. LAN interception must be enabled explicitly;
firewall flow offloading must be disabled first.

Diagnostics shows configuration checks, process state, NFQUEUE and nftables
results with expandable details. Service controls are available here. The
[Zapret tuner](zapret-tuner.md) proposes a strategy for the draft; applying it
still requires **Save and apply**.

## UI checks

Browser tests use Chromium and synthetic RPC responses. Set dependency paths
for your machine before running them:

```sh
NADKOP_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
NADKOP_LUCI_THEME_ROOT=/path/to/sdk/feeds/luci/themes \
node scripts/test-picker.mjs
```

The shared RPC fixture also accepts `S6_LUCI_RPC` and `S6_LUCI_THEME_ROOT` for
the SDK's LuCI RPC module and theme directory. These checks cover keyboard
controls, focus, saving and narrow layouts; real router behavior needs the
[OpenWrt VM checks](openwrt-vm.md).
