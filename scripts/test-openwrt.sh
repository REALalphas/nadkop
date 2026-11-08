#!/bin/sh
# Run on a disposable OpenWrt VM after installing nadkop and curl.
set -eu
[ -r /etc/openwrt_release ] && [ "$(id -u)" = 0 ] || exit 1
command -v curl >/dev/null
command -v uhttpd >/dev/null
work="$(mktemp -d /tmp/nadkop-test.XXXXXX)"
# procd updates are asynchronous; ensure tests reach the intended generation.
wait_stopped() {
 n=0
 while awk '$2 == "0100007F:0438" && $4 == "0A" { found=1 } END { exit !found }' /proc/net/tcp; do
  n=$((n+1)); [ "$n" -lt 10 ] || { echo 'FAIL: previous SOCKS listener did not stop'; return 1; }; sleep 1
 done
}
wait_ready() {
 n=0
 while :; do
  ubus call service list '{"name":"nadkop"}' > "$work/ready"
  pid="$(jsonfilter -i "$work/ready" -e '@.nadkop.instances.xray.pid')"
  inode="$(awk '$2 == "0100007F:0438" && $4 == "0A" { print $10; exit }' /proc/net/tcp)"
  configured="$(jsonfilter -i "$work/ready" -e '@.nadkop.instances.xray.command[3]')"
  expected="$(readlink -f /var/run/nadkop/current/xray.json)"
  # procd can publish the new command while its PID still belongs to the
  # terminating generation. Verify the process arguments as well as its socket.
  actual=
  if [ -n "$pid" ] && [ -r "/proc/$pid/cmdline" ]; then
   actual="$(tr '\000' '\n' < "/proc/$pid/cmdline" | sed -n '4p')"
  fi
  if [ -n "$pid" ] && [ -n "$inode" ] && [ -n "$actual" ] && [ "$(readlink -f "$actual")" = "$expected" ] && [ "$(readlink -f "$configured")" = "$expected" ] && ls -l "/proc/$pid/fd" 2>/dev/null | grep -q "socket:\[$inode\]"; then
   if [ ! -s /var/run/nadkop/current/zapret-hosts.txt ] || grep -q '^ *200 ' /proc/net/netfilter/nfnetlink_queue; then return 0; fi
  fi
  n=$((n+1)); [ "$n" -lt 10 ] || { echo 'FAIL: intended Xray generation did not own the SOCKS listener'; return 1; }; sleep 1
 done
}
was_running=0
/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
cp /etc/nadkop/config.json "$work/config.backup"
cp /etc/nadkop/nodes.json "$work/nodes.backup"
had_catalog=0
if [ -f /etc/nadkop/servers.json ]; then
	cp /etc/nadkop/servers.json "$work/servers.backup"
	had_catalog=1
fi
cp /etc/hosts "$work/hosts.backup"
http_pid=
upstream_pid=
cleanup() {
	/etc/init.d/nadkop stop >/dev/null 2>&1 || true
	[ -z "$http_pid" ] || kill "$http_pid" 2>/dev/null || true
	[ -z "$upstream_pid" ] || kill "$upstream_pid" 2>/dev/null || true
	cp "$work/config.backup" /etc/nadkop/config.json
	cp "$work/nodes.backup" /etc/nadkop/nodes.json
	if [ "$had_catalog" = 1 ]; then cp "$work/servers.backup" /etc/nadkop/servers.json; else rm -f /etc/nadkop/servers.json; fi
	cp "$work/hosts.backup" /etc/hosts
	chmod 600 /etc/nadkop/*.json
	[ "$was_running" = 0 ] || /etc/init.d/nadkop start
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

/etc/init.d/nadkop stop >/dev/null 2>&1 || true
wait_stopped
printf '127.0.0.1 nadkop-direct.test nadkop-proxy.test nadkop-block.test nadkop-dpi.test\n' >> /etc/hosts
printf 'nadkop-smoke-ok\n' > "$work/index.html"
uhttpd -f -p 127.0.0.1:18080 -h "$work" &
http_pid=$!
cat > "$work/upstream.json" <<'EOF'
{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":2080,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom"}]}
EOF
xray run -config "$work/upstream.json" > "$work/upstream.log" 2>&1 &
upstream_pid=$!
cat > /etc/nadkop/nodes.json <<'EOF'
[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]
EOF
cat > /etc/nadkop/config.json <<'EOF'
{"sources":[{"name":"lab","file":"/etc/nadkop/nodes.json"}],"default":"direct","socksPort":1080,"wanInterface":"eth0","rules":[{"domains":["nadkop-proxy.test"],"action":"proxy","node":"lab/exit"},{"domains":["nadkop-block.test"],"action":"block"},{"domains":["nadkop-dpi.test"],"action":"zapret"}],"zapretArgs":["--filter-tcp=80,443","--dpi-desync=fake","--dpi-desync-ttl=2"]}
EOF
chmod 600 /etc/nadkop/*.json
nadkop validate
[ "$(nadkop nodes)" = 'lab/exit' ]
/etc/init.d/nadkop start
wait_ready
/etc/init.d/nadkop running
xray run -test -config /var/run/nadkop/current/xray.json
nft list table inet nadkop >/dev/null
grep -q '^ *200 ' /proc/net/netfilter/nfnetlink_queue
nfq_pid="$(ubus call service list '{"name":"nadkop"}' | jsonfilter -e '@.nadkop.instances.nfqws.pid')"
[ "$(awk '/^Uid:/ {print $2}' "/proc/$nfq_pid/status")" = 2147483647 ]
case "$(ls -l /var/run/nadkop/current/xray.json)" in
	'-rw------- '*) ;;
	*) echo 'FAIL: Xray config permissions are not 0600' >&2; exit 1 ;;
esac

fetch() {
	curl --fail --silent --show-error --noproxy '' --connect-timeout 2 --max-time 3 --socks5-hostname 127.0.0.1:1080 "http://$1:18080/"
}
[ "$(fetch nadkop-direct.test)" = nadkop-smoke-ok ]
[ "$(fetch nadkop-proxy.test)" = nadkop-smoke-ok ]
if fetch nadkop-block.test > "$work/blocked.out" 2>&1; then
	echo 'FAIL: blocked route was reachable' >&2; exit 1
fi
echo 'PASS: direct, proxy via a second SOCKS server, block'
kill "$upstream_pid"
wait "$upstream_pid" 2>/dev/null || true
upstream_pid=
if fetch nadkop-proxy.test > "$work/proxy-down.out" 2>&1; then
	echo 'FAIL: proxy route bypassed the unavailable upstream' >&2; exit 1
fi
[ "$(fetch nadkop-direct.test)" = nadkop-smoke-ok ]
echo 'PASS: proxy upstream failure does not fall back to direct'

# Non-hostlist HTTPS must still work while NFQUEUE is attached.
curl --fail --silent --show-error --connect-timeout 5 --max-time 15 -I https://downloads.openwrt.org/ >/dev/null
cat /proc/net/netfilter/nfnetlink_queue
echo 'PASS: NFQUEUE 200 attached; HTTPS outside the hostlist works'
previous="$(readlink /var/run/nadkop/current)"
/etc/init.d/nadkop restart
wait_ready
[ "$previous" != "$(readlink /var/run/nadkop/current)" ]
/etc/init.d/nadkop running
grep -q '^ *200 ' /proc/net/netfilter/nfnetlink_queue
/etc/init.d/nadkop stop
wait_stopped
if nft list table inet nadkop >/dev/null 2>&1; then
	echo 'FAIL: nadkop nftables table survived stop' >&2; exit 1
fi
if /etc/init.d/nadkop running >/dev/null 2>&1; then
	echo 'FAIL: nadkop processes survived stop' >&2; exit 1
fi
echo 'PASS: restart publishes a new generation; stop removes processes and table'
cat > /etc/nadkop/config.json <<'EOF'
{"sources":[],"default":"direct","socksPort":1080,"rules":[]}
EOF
/etc/init.d/nadkop start
wait_ready
/etc/init.d/nadkop running
if nft list table inet nadkop >/dev/null 2>&1 || grep -q '^ *200 ' /proc/net/netfilter/nfnetlink_queue; then
	echo 'FAIL: direct-only configuration enabled NFQUEUE' >&2; exit 1
fi
[ "$(fetch nadkop-direct.test)" = nadkop-smoke-ok ]
echo 'PASS: direct-only configuration runs without nfqws or an nftables table'
echo 'OpenWrt smoke tests passed'
