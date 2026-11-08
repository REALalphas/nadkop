#!/bin/sh
# Extend the existing namespace fixture with actual port-80 NFQUEUE measurements.
set -eu
work="$(mktemp -d /tmp/nadkop-queue-isolation.XXXXXX)"
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM
cat > "$work/insert" <<'TEST'
queue_count(){ awk '$1==200{print $8}' /proc/net/netfilter/nfnetlink_queue; }
port80_fetch(){ ip netns exec "$client" curl -4 --fail --silent --show-error --max-time 4 --noproxy '*' "http://$1/"; }
queue_direct_bypass(){
 before="$(queue_count)"
 reply="$(port80_fetch except.nk-dpi.test 2>"$work/fetch-error")" || return 1
 after="$(queue_count)"
 echo "queue direct-exception: $before -> $after" >&2
 [ "$reply" = direct-ok ] && [ "$before" = "$after" ]
}
queue_proxy_bypass(){
 before="$(queue_count)"
 reply="$(port80_fetch nk-proxy.test 2>"$work/fetch-error")" || return 1
 after="$(queue_count)"
 echo "queue proxy: $before -> $after" >&2
 [ "$reply" = proxy-ok ] && [ "$before" = "$after" ]
}
queue_zapret_used(){
 before="$(queue_count)"
 reply="$(port80_fetch nk-dpi.test 2>"$work/fetch-error")" || return 1
 after="$(queue_count)"
 echo "queue zapret: $before -> $after" >&2
 [ "$reply" = direct-ok ] && [ "$after" -gt "$before" ]
}
ip netns exec "$server" uhttpd -f -p 0.0.0.0:80 -h "$work/direct" > "$work/http80.log" 2>&1 &
occupied_pid=$!
check 'port 80 direct exception bypasses NFQUEUE' queue_direct_bypass
check 'port 80 proxy connection bypasses NFQUEUE' queue_proxy_bypass
check 'port 80 Zapret route actually traverses NFQUEUE' queue_zapret_used
kill "$occupied_pid" 2>/dev/null || true
wait "$occupied_pid" 2>/dev/null || true
occupied_pid=
TEST
awk 'FNR==NR{extra=extra $0 "\n";next} /^saved_current=/{printf "%s",extra} {print}' "$work/insert" /tmp/test-transparent-openwrt.sh > "$work/fixture.sh"
sh "$work/fixture.sh" zapret
