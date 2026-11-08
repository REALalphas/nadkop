#!/bin/sh
# Diagnose the default DNS prerequisite independently of Nadkop, then restore hosts.
set -eu
umask 077
work="$(mktemp -d /tmp/nadkop-local-dns-diagnosis.XXXXXX)"
cp /etc/hosts "$work/hosts"
cleanup(){ cp "$work/hosts" /etc/hosts;killall -HUP dnsmasq 2>/dev/null || true;rm -rf "$work"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
printf '\n203.0.113.3 nk-ipfirst.test\n192.0.2.1 nk-router.lan\n' >> /etc/hosts
killall -HUP dnsmasq
sleep 1
for domain in nk-ipfirst.test nk-router.lan;do
 echo "QUERY: $domain A via 127.0.0.1 before Nadkop"
 dig @127.0.0.1 "$domain" A +noall +comments +answer +tries=1 +time=2
 echo "SYSTEM HOSTS: $domain"
 grep "$domain" /etc/hosts | sed -n '1p'
done
