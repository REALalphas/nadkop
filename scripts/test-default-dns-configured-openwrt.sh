#!/bin/sh
# Give dnsmasq synthetic data through OpenWrt's actual addn-hosts directory.
set -eu
fixture=/tmp/hosts/nadkop-resume-dns-test
[ ! -e "$fixture" ] || exit 2
cleanup(){ rm -f "$fixture";killall -HUP dnsmasq 2>/dev/null || true;}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
cat > "$fixture" <<'HOSTS'
203.0.113.2 nk-direct.test nk-proxy.test nk-block.test nk-default.test nk-dpi.test except.nk-dpi.test
203.0.113.3 nk-ipfirst.test
2001:db8:2::2 nk-v6.test
192.0.2.1 nk-router.lan
HOSTS
chmod 644 "$fixture"
killall -HUP dnsmasq
n=0
while [ "$(dig @127.0.0.1 nk-router.lan A +short +tries=1 +time=1 | tail -n 1)" != 192.0.2.1 ];do
 n=$((n+1));[ "$n" -lt 5 ] || { echo 'SETUP FAILED: dnsmasq fixture not loaded';exit 2;};sleep 1
done
[ "$(dig @127.0.0.1 nk-ipfirst.test A +short +tries=1 +time=1 | tail -n 1)" = 203.0.113.3 ]
echo 'PASS: local DNS fixture resolves the exact test names before enabling Nadkop'
sh /tmp/test-default-dns-openwrt.sh
