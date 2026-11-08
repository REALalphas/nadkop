#!/bin/sh
# Same complete LAN fixture, but exercise the product's default local DNS upstream.
set -eu
fixture="$(mktemp /tmp/nadkop-default-dns-test.XXXXXX)"
trap 'rm -f "$fixture"' EXIT
trap 'exit 1' HUP INT TERM
sed 's/"dnsUpstream":"203.0.113.2"/"dnsUpstream":"127.0.0.1"/' /tmp/test-transparent-openwrt.sh > "$fixture"
sh "$fixture" lan
