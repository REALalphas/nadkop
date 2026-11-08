#!/bin/sh
# Dedicated OpenWrt VM only. Synthetic namespaces, hosts and sources; always restore.
set -u
mode="${1:-lan}"
[ -r /etc/openwrt_release ] && [ "$(id -u)" = 0 ] || exit 2
umask 077
work="$(mktemp -d /tmp/nadkop-lan-test.XXXXXX)" || exit 2
client=nkt-client
server=nkt-server
http_pid= proxy_http_pid= v6_http_pid= upstream_pid= udp_pid= dns_pid= occupied_pid=
was_running=0
/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
for file in config.json nodes.json servers.json; do cp "/etc/nadkop/$file" "$work/$file" || exit 2; done
cp /etc/hosts "$work/hosts" || exit 2
old_v4="$(cat /proc/sys/net/ipv4/ip_forward)"
old_v6="$(cat /proc/sys/net/ipv6/conf/all/forwarding)"
old_flow="$(uci -q get firewall.@defaults[0].flow_offloading || true)"
failures=0
passes=0
restore_flow() {
 if [ -n "$old_flow" ]; then uci -q set "firewall.@defaults[0].flow_offloading=$old_flow"; else uci -q delete firewall.@defaults[0].flow_offloading; fi
}
cleanup() {
 /etc/init.d/nadkop stop >/dev/null 2>&1 || true
 for pid in "$occupied_pid" "$http_pid" "$proxy_http_pid" "$v6_http_pid" "$upstream_pid" "$udp_pid" "$dns_pid"; do [ -z "$pid" ] || kill "$pid" 2>/dev/null || true; done
 ip -4 rule del priority 10000 lookup 101 2>/dev/null || true
 ip netns del "$client" 2>/dev/null || true
 ip netns del "$server" 2>/dev/null || true
 ip link del nkt-lan 2>/dev/null || true
 ip link del nkt-wan 2>/dev/null || true
 rm -rf "/etc/netns/$client"
 restore_flow || true
 /etc/init.d/firewall reload >/dev/null 2>&1 || true
 printf '%s\n' "$old_v4" > /proc/sys/net/ipv4/ip_forward
 printf '%s\n' "$old_v6" > /proc/sys/net/ipv6/conf/all/forwarding
 cp "$work/hosts" /etc/hosts
 killall -HUP dnsmasq 2>/dev/null || true
 for file in config.json nodes.json servers.json; do cp "$work/$file" "/etc/nadkop/$file"; done
 [ "$was_running" = 0 ] || /etc/init.d/nadkop start
 # Restore the exact catalogue bytes after the restart's refresh too.
 cp "$work/servers.json" /etc/nadkop/servers.json
 rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM
must() { "$@" || { echo "SETUP FAILED: $*" >&2; exit 2; }; }
check() {
 name="$1";shift
 if "$@";then passes=$((passes+1));echo "PASS: $name";else failures=$((failures+1));echo "FAIL: $name";[ ! -s "$work/fetch-error" ] || cat "$work/fetch-error";fi
}
wait_ready() {
 n=0
 while [ "$n" -lt 15 ]; do
  ubus call service list '{"name":"nadkop"}' > "$work/service"
  pid="$(jsonfilter -i "$work/service" -e '@.nadkop.instances.xray.pid')"
  expected="$(readlink -f /var/run/nadkop/current/xray.json)"
  actual=
  [ -z "$pid" ] || actual="$(tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null | sed -n '4p')"
  if [ -n "$pid" ] && [ -n "$actual" ] && [ "$(readlink -f "$actual")" = "$expected" ] && curl -s --max-time 1 --socks5-hostname 127.0.0.1:1080 http://nk-direct.test:18080/ >/dev/null; then return 0; fi
  n=$((n+1));sleep 1
 done
 echo "READY failed pid=$pid actual=$actual expected=$expected" >&2
 cat "$work/service" >&2
 curl -v --max-time 3 --socks5-hostname 127.0.0.1:1080 http://nk-direct.test:18080/ >&2
 logread -e xray | tail -8 >&2
 return 1
}
fetch() { ip netns exec "$client" curl -4 --fail --silent --show-error --max-time 4 --noproxy '*' "http://$1:18080/"; }
body_is() { got="$(fetch "$1" 2>"$work/fetch-error")" && [ "$got" = "$2" ]; }
blocked() { ! fetch "$1" > "$work/blocked" 2>&1; }
fake_address() { ip netns exec "$client" dig @192.0.2.1 "$1" A +short +tries=1 +time=2 | sed -n '/^198\.1[89]\./p' | head -n 1; }
is_fake() { case "$1" in 198.18.*|198.19.*) return 0;; *) return 1;; esac; }
no_policy() { ! ip "$1" rule show | grep -q 'lookup 100'; }
current_unchanged() { [ "$(readlink /var/run/nadkop/current)" = "$saved_current" ]; }
reject_apply() { ! nadkop apply > "$work/rejected" 2>&1 && current_unchanged; }

for command in ip curl dig socat uhttpd jsonfilter dnsmasq; do command -v "$command" >/dev/null || { echo "Missing test dependency: $command";exit 2; }; done
ip netns list | grep -q '^nkt-' && { echo 'Test namespaces already exist';exit 2; }
/etc/init.d/nadkop stop >/dev/null 2>&1 || true
sleep 1
must ip netns add "$client"
must ip netns add "$server"
must ip link add nkt-lan type veth peer name nkt-client
must ip link set nkt-client netns "$client"
must ip link add nkt-wan type veth peer name nkt-server
must ip link set nkt-server netns "$server"
for device in nkt-lan nkt-wan; do must ip link set "$device" up; done
must ip addr add 192.0.2.1/24 dev nkt-lan
must ip addr add 203.0.113.1/24 dev nkt-wan
must ip -6 addr add 2001:db8:1::1/64 dev nkt-lan nodad
must ip -6 addr add 2001:db8:2::1/64 dev nkt-wan nodad
for ns in "$client" "$server"; do must ip -n "$ns" link set lo up; done
must ip -n "$client" link set nkt-client up
must ip -n "$client" addr add 192.0.2.2/24 dev nkt-client
must ip -n "$client" -6 addr add 2001:db8:1::2/64 dev nkt-client nodad
must ip -n "$client" route add default via 192.0.2.1
must ip -n "$client" -6 route add default via 2001:db8:1::1
must ip -n "$server" link set nkt-server up
must ip -n "$server" addr add 203.0.113.2/24 dev nkt-server
must ip -n "$server" addr add 203.0.113.3/24 dev nkt-server
must ip -n "$server" -6 addr add 2001:db8:2::2/64 dev nkt-server nodad
mkdir -p "/etc/netns/$client"
printf 'nameserver 192.0.2.1\n' > "/etc/netns/$client/resolv.conf"
printf '1\n' > /proc/sys/net/ipv4/ip_forward
printf '1\n' > /proc/sys/net/ipv6/conf/all/forwarding
printf '\n203.0.113.2 nk-direct.test nk-proxy.test nk-block.test nk-default.test nk-dpi.test except.nk-dpi.test\n203.0.113.3 nk-ipfirst.test\n2001:db8:2::2 nk-v6.test\n192.0.2.1 nk-router.lan\n' >> /etc/hosts
killall -HUP dnsmasq
cat > "$work/test-hosts" <<'HOSTS'
203.0.113.2 nk-direct.test nk-proxy.test nk-block.test nk-default.test nk-dpi.test except.nk-dpi.test
203.0.113.3 nk-ipfirst.test
2001:db8:2::2 nk-v6.test
192.0.2.1 nk-router.lan
HOSTS
ip netns exec "$server" dnsmasq --no-daemon --conf-file=/dev/null --no-resolv --no-hosts --addn-hosts="$work/test-hosts" --port=53 --bind-interfaces --listen-address=203.0.113.2 --user=root --pid-file="$work/dns.pid" > "$work/dns.log" 2>&1 &
dns_pid=$!
mkdir "$work/direct" "$work/proxy"
printf 'direct-ok\n' > "$work/direct/index.html"
printf 'proxy-ok\n' > "$work/proxy/index.html"
chmod 711 "$work"
chmod 755 "$work/direct" "$work/proxy"
chmod 644 "$work/direct/index.html" "$work/proxy/index.html"
ip netns exec "$server" uhttpd -f -p 0.0.0.0:18080 -h "$work/direct" > "$work/http.log" 2>&1 &
http_pid=$!
ip netns exec "$server" uhttpd -f -p 0.0.0.0:18082 -h "$work/proxy" > "$work/proxy-http.log" 2>&1 &
proxy_http_pid=$!
ip netns exec "$server" uhttpd -f -p '[2001:db8:2::2]:18080' -h "$work/direct" > "$work/v6-http.log" 2>&1 &
v6_http_pid=$!
ip netns exec "$server" socat UDP4-RECVFROM:18081,fork EXEC:/bin/cat > "$work/udp.log" 2>&1 &
udp_pid=$!
cat > "$work/upstream.json" <<'JSON'
{"inbounds":[{"listen":"127.0.0.1","port":2088,"protocol":"socks","settings":{"auth":"noauth","udp":true}}],"outbounds":[{"tag":"redirect","protocol":"freedom","settings":{"redirect":"203.0.113.2:18082"}},{"tag":"udp","protocol":"freedom"}],"routing":{"rules":[{"type":"field","network":"udp","outboundTag":"udp"}]}}
JSON
xray run -config "$work/upstream.json" > "$work/upstream.log" 2>&1 &
upstream_pid=$!
cat > /etc/nadkop/nodes.json <<'JSON'
[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2088}]}}]
JSON
cat > /etc/nadkop/config.json <<'JSON'
{"sources":[{"name":"lab","file":"/etc/nadkop/nodes.json"}],"default":"block","socksPort":1080,"transparent":{"enabled":true,"interfaces":["nkt-lan"],"port":12345,"dnsPort":1053,"dnsUpstream":"203.0.113.2","ipv6":true},"wanInterface":"nkt-wan","zapretArgs":["--filter-tcp=80,443","--filter-udp=443","--dpi-desync=fake"],"rules":[{"ips":["203.0.113.3/32"],"action":"direct"},{"domains":["nk-direct.test","nk-v6.test","except.nk-dpi.test"],"action":"direct"},{"domains":["nk-proxy.test"],"action":"proxy","node":"lab/exit"},{"domains":["nk-block.test","nk-ipfirst.test"],"action":"block"},{"domains":["nk-dpi.test"],"action":"zapret"}]}
JSON
if [ "$mode" != zapret ]; then
 sed 's/"action":"zapret"/"action":"direct"/' /etc/nadkop/config.json > "$work/without-zapret"
 cp "$work/without-zapret" /etc/nadkop/config.json
fi
cp /etc/nadkop/config.json "$work/valid-config"
must /etc/init.d/nadkop start
# Synthetic interfaces have no fw4 zone: permit NDP for the test links only.
must nft insert rule inet fw4 input 'iifname { "nkt-lan", "nkt-wan" } meta l4proto ipv6-icmp accept comment "nadkop-test-ndp"'
must wait_ready
check 'plain LAN HTTP direct, without SOCKS configuration' body_is nk-direct.test direct-ok
check 'plain LAN HTTP proxy uses redirected upstream' body_is nk-proxy.test proxy-ok
check 'LAN domain block' blocked nk-block.test
check 'LAN default block' blocked nk-default.test
check 'early IP rule wins over later domain block' body_is nk-ipfirst.test direct-ok
check 'direct exception within Zapret domain' body_is except.nk-dpi.test direct-ok
check "$mode route reaches direct endpoint" body_is nk-dpi.test direct-ok
fake="$(fake_address nk-direct.test)"
check 'LAN UDP DNS returns FakeDNS pool' is_fake "$fake"
fake_tcp="$(ip netns exec "$client" dig @9.9.9.9 nk-direct.test A +tcp +short +tries=1 +time=2 | tail -n 1)"
check 'external TCP DNS is intercepted' is_fake "$fake_tcp"
local_dns="$(ip netns exec "$client" dig @192.0.2.1 nk-router.lan A +short +tries=1 +time=2 | tail -n 1)"
check 'local LAN names use real upstream' test "$local_dns" = 192.0.2.1
udp_echo() { address="$(fake_address "$1")"; is_fake "$address" || return 1; reply="$(printf 'udp-ok' | ip netns exec "$client" socat -T 3 - "UDP4-DATAGRAM:$address:18081" 2>"$work/udp-error")"; [ "$reply" = udp-ok ]; }
udp_blocked() { address="$(fake_address nk-block.test)"; is_fake "$address" || return 1; reply="$(printf 'udp-block' | ip netns exec "$client" socat -T 2 - "UDP4-DATAGRAM:$address:18081" 2>"$work/udp-error")"; [ -z "$reply" ]; }
check 'UDP without SNI routes by FakeDNS domain directly' udp_echo nk-direct.test
check 'UDP through selected SOCKS upstream' udp_echo nk-proxy.test
check 'blocked domain also blocks UDP' udp_blocked
v6_direct() { reply="$(ip netns exec "$client" curl -6 --fail --silent --show-error --max-time 4 --noproxy '*' http://nk-v6.test:18080/ 2>"$work/v6-error")" && [ "$reply" = direct-ok ]; }
check 'IPv6 LAN DNS and transparent HTTP' v6_direct
check 'IPv4 reserved policy rule installed' sh -c 'ip -4 rule show | grep -q "fwmark 0x10000/0x10000 lookup 100"'
check 'IPv6 reserved policy rule installed' sh -c 'ip -6 rule show | grep -q "fwmark 0x10000/0x10000 lookup 100"'
if [ "$mode" = zapret ]; then
 check 'generation-specific nfqws hostlist avoids stale procd process' sh -c 'ubus call service list "{\"name\":\"nadkop\"}" | jsonfilter -e "@.nadkop.instances.nfqws.command[*]" | grep -q "hostlist=/var/run/nadkop/generation\."'
fi
check 'firewall4 reload succeeds with active includes' /etc/init.d/firewall reload
check 'LAN rules survive firewall reload' body_is nk-proxy.test proxy-ok
check 'fw4 TPROXY input exception restored' sh -c 'nft list chain inet fw4 input | grep -q "nadkop-tproxy"'
saved_current="$(readlink /var/run/nadkop/current)"
sed 's/"port":12345/"port":1080/' "$work/valid-config" > /etc/nadkop/config.json
check 'invalid conflicting port preserves running generation' reject_apply
reject_start() { ! /etc/init.d/nadkop start > "$work/rejected-start" 2>&1 && current_unchanged; }
check 'package start with invalid configuration preserves running generation' reject_start
cp "$work/valid-config" /etc/nadkop/config.json
must ip -4 rule add priority 10000 lookup 101
check 'foreign policy priority is rejected before replacement' reject_apply
must ip -4 rule del priority 10000 lookup 101
must uci set 'firewall.@defaults[0].flow_offloading=1'
check 'flow offloading is rejected before replacement' reject_apply
restore_flow || true
check 'rejected updates leave direct route functional' body_is nk-direct.test direct-ok
kill "$upstream_pid";wait "$upstream_pid" 2>/dev/null || true;upstream_pid=
check 'failed proxy has no direct fallback' blocked nk-proxy.test
check 'failed proxy leaves direct rule functional' body_is nk-direct.test direct-ok
sed 's/"ipv6":true/"ipv6":false/' "$work/valid-config" > /etc/nadkop/config.json
check 'apply IPv4 mode' nadkop apply
must wait_ready
check 'IPv6 policy removed in IPv4 mode' no_policy -6
ipv6_blocked() { ! ip netns exec "$client" curl -6 --fail --silent --max-time 2 --noproxy '*' --resolve 'nk-v6.test:18080:[2001:db8:2::2]' http://nk-v6.test:18080/ > "$work/v6-blocked" 2>&1; }
check 'IPv4 mode does not leak literal IPv6 connections' ipv6_blocked
check 'stop succeeds' /etc/init.d/nadkop stop
check 'stop removes IPv4 policy' no_policy -4
check 'stop removes applied-generation pointer' test ! -e /var/run/nadkop/current
check 'stop removes own nft table' sh -c '! nft list table inet nadkop >/dev/null 2>&1'
check 'firewall reload after stop succeeds' /etc/init.d/firewall reload
check 'firewall reload does not resurrect stopped rules' sh -c '! nft list table inet nadkop >/dev/null 2>&1'
[ "$failures" = 0 ] || logread -e xray | tail -15
 echo "RESULT: $passes passed, $failures failed"
[ "$failures" = 0 ]
