#!/bin/sh
# Independently verify queued HTTP with a fake-packet strategy suited to a one-hop fixture.
set -eu
work="$(mktemp -d /tmp/nadkop-queue-badseq.XXXXXX)"
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM
sed 's/"--dpi-desync=fake"/"--dpi-desync=fake","--dpi-desync-fooling=badseq"/' /tmp/test-transparent-openwrt.sh > "$work/transparent.sh"
sed "s#/tmp/test-transparent-openwrt.sh#$work/transparent.sh#" /tmp/test-zapret-isolation-openwrt.sh > "$work/isolation.sh"
sh "$work/isolation.sh"
