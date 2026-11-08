# Product contract 0.3

This contract was written before final testing. After the code freeze,
tests verify the stated requirements; bugs found are recorded without
changing the implementation. Totals and confirmation limits are in `reports/`.

## Setup and compatibility

- OpenWrt with firewall4/nftables, Xray-core, nfqws, ip-full, kmod-nft-tproxy,
  kmod-nft-queue. The package declares dependencies; the SDK must match the release and
  device architecture. APK and IPK are built with matching SDKs.
- Install keeps the user's config.json, nodes.json, hwid, and servers.json.
  A fresh install has no samples with nonexistent VPNs. SOCKS is 127.0.0.1:1080;
  subscription updates run every 6 hours; LAN interception is enabled explicitly.
- A missing `transparent` keeps compatibility with old configurations.
  `updateIntervalHours: 0` means manual updates, 1–168 is the interval in hours.
- Up to 16 sources, 1024 rules; up to 16 servers per group, 16 groups, and 64 probe IDs.
  Configurations, RPC, and subscriptions are limited to 4 MiB, a geodatabase file to 64 MiB.

## Rules

The first matching rule wins, including early-CIDR plus late-domain
combinations. Domains, IPs, and sets in one rule are alternatives. Domains are normalized
to lowercase and include subdomains; ASCII/punycode supported. IP/CIDR
is normalized for IPv4/IPv6. Nested and repeated domains are allowed: a broad early
rule may intentionally shadow a later one. In LuCI, arrows reorder rules.

`direct` uses a direct connection, `block` blocks, `proxy` uses
the selected server or group. `zapret` uses a separate freedom outbound with
mark 0x10000000. Plain direct and proxy sockets with Zapret present get
0x20000000 and never enter its queue. Mark 0x40000000 excludes repeated
nfqws packet processing. Custom sockopt bits are preserved; overlap
with reserved bits is rejected, including nested settings.

A group has `block` selected before the first probe and when all its servers fail.
A missing single nodeRef blocks applying; a missing group member
is skipped in selection. Priority picks the first available, ping/URL the lowest
HTTP latency with order-based tie-breaking. Probes time out after 5 seconds,
up to four at once; the pause after a cycle is 15 seconds. There is no direct fallback outlet.

Sets expand from the loaded geodatabase pair. An unsupported set, a missing
database, or regexp/keyword for Zapret blocks applying. Pair updates check
the sets in use before atomic publication; the previous generation is kept.

## LAN and DNS

Sample settings (on top of sources/rules/default):

```json
{
  "transparent": {
    "enabled": true,
    "interfaces": ["br-lan"],
    "port": 12345,
    "dnsPort": 1053,
    "dnsUpstream": "127.0.0.1",
    "ipv6": true,
    "bypass": []
  },
  "updateIntervalHours": 6
}
```

Only inbound TCP/UDP packets of the selected LAN devices are intercepted, no
router OUTPUT interception. DNS on TCP/UDP 53 is handled ahead of exclusions,
including requests to the router IP or an external DNS. FakeDNS binds IP and name for
protocol routing without HTTP/SNI. Pool addresses: 198.18.0.0/15 and fc00::/18.
Plain Xray IP conditions resolve via real DNS; `lan`, `local`, `home.arpa`
resolve via the plain upstream. The default is the router dnsmasq on
127.0.0.1:53; its UCI configuration is untouched.

The router's own addresses, private networks, link-local, and multicast pass without Xray;
extra exclusions go in bypass. DNS and FakeDNS pools are checked ahead of
these exclusions. That means direct connections to private IPs do not fall
under Xray rules. IPv6 is enabled together with its own policy route; with `ipv6:false`
its TCP/UDP is blocked on the selected LAN devices to prevent bypass.
ICMP, ARP, and IPv6 NDP are not proxied. The dedicated TPROXY/DNS ports are closed to
unmarked inbound connections outside loopback.

Clients with their own DoH/DoT get no FakeDNS from Nadkop. Their HTTP/TLS/QUIC
may match by sniffing, other connections by IP; a hidden
encrypted name without local DNS cannot be recovered. This is a mechanism boundary
that tests must not present as traffic-decryption support.

## Applying and OpenWrt interaction

Reserved: inet nadkop, NFQUEUE 200, bit 0x10000, routing table 100,
policy-rule priority 10000, internal health ports 61080/61081.
Pre-apply checks detect busy ports, a missing LAN interface,
a busy table/mark/priority, missing firewall4, enabled flow offloading,
and live Podkop interception (its `inet PodkopTable`, fwmark 0x100000 policy
route, or dnsmasq hijack to 127.0.0.42): transparent routing is refused while
Podkop intercepts LAN/DNS, because both claim the same FakeDNS ranges and
prerouting hooks. A merely installed but idle Podkop does not block.
Unsuitable settings are rejected; third-party service settings are not rewritten.
A non-fatal `coexistence` diagnostic step (and runtime check) warns when Podkop
traces are present: its sing-box upstream TLS passes Nadkop's Zapret NFQUEUE
(mark 0x100000 does not bypass the 0x60000000 mask), so list Podkop VPN
endpoints in Zapret bypass prefixes or disable Zapret rules.

Xray config, hostlist/ipset, nft, and runtime.json are published as one directory
with private files. Before procd is updated: load, compile,
Xray, nfqws, and nft checks. An error at these stages keeps the working generation.
Nft applies in a single transaction. Before the network changes, the journal of the
previous state is saved. On apply or current-publishing errors, Nadkop's own
policy routes/rules and firewall are restored; a restore failure keeps
the journal and returns an error for a later retry. Publishing current completes
the network operation: a failure to clean its journal is logged and does not interrupt process
registration while current points at this generation. No overall crash transaction for
current/procd is guaranteed. Only Nadkop's own table and the `nadkop-tproxy` exception in
fw4 input change. Include files restore the table and exception on firewall reload.
For that, firewall4 auto_includes must be enabled.

`restart` and `reload` stage first, then update procd.
Switching to new processes may briefly interrupt connections; outbound
TCP connections do not migrate between processes. `stop` removes the table, the input
exception, and its own policy routes, then drops the current pointer so firewall
does not restore disabled interception. A damaged runtime does not cancel cleanup
of Nadkop's own policies; command errors are returned, foreign conflicting rules
are not deleted. System LAN/SSH/LuCI do not depend on Xray.

Subscription updates run in a separate procd process. Requests are time-limited;
on error no new runtime is applied. Geodatabases update separately
via a LuCI button and take effect on the next apply. Real Zapret-strategy
effectiveness depends on the provider and does not follow from a syntax check.

## Storage and interface

Rules reference stable catalogue IDs, no secret copying. Renaming
and reordering servers, credential rotation on unambiguous matching keep
IDs; ambiguity never swaps a reference. Needed vanished servers stay,
unbound ones are cleaned only on successful update or save.

CLI generate/nodes and RPC synchronize config reads and catalogue changes
under a shared lock. On partial restore, unknown saved nodeRef/nodeRefs
and pins, including the inactive selection, are rejected before new IDs are created. Migration
needs the original config.json plus servers.json pair; identity is never guessed.

Saving checks the revision, rule fields, local links, and geosets.
Final config/nodes/catalogue, including the trailing newline, are each limited to
4 MiB. Overflow is checked before publishing; no overall crash-commit of several
files under fsync/SIGKILL is guaranteed by this. Source adding validates
the complete future configuration before catalogue sync and writing.
Unsaved changes stay in the editor when a source is added separately.
New controls during an operation keep the busy lock; a late draft
edit gets no saved status. A nonzero ubus code is rejected,
a validate/save error aborts the chain before restart.
The error points at the rule/field; diagnostics reflect the applied state.
Credential-bearing RPC is write-ACL only; status returns no credentials.
Test logs use synthetic sources; user subscriptions and HWID
must not end up in reports.


## Rule merging in core r4 / UI r8

`conditions` separately enables `domains`, `ips`, and `sets`. Without the object,
all legacy fields are active. Disabled values persist across save/reload but
do not affect routing or geodatabase checks; re-enabling validates them again. An inactive VPN selection, its probe URL, and an inactive
Zapret strategy persist too; the runtime uses only the active action.

Before saving, LuCI validates the draft via RPC validate without writing to disk.
Stock DeviceSelects take OpenWrt Linux devices and serialize string names
for LAN/WAN. Incompatible and inverted sets are marked in the catalogue; geosite attributes
are checked against their own makeup. For an impossible action the UI explains
the reason, and the backend keeps the final boundary check.

Original check of the pinned version:
`reports/runs/resume-0.3.0-r4-ui-r8/verification.md`. Later allowed
blocker fixes and their confirmation limits:
[fix acceptance](../reports/blocker-closure/acceptance.md).
