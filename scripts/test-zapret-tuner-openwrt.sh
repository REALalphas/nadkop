#!/bin/sh
# Dedicated VM only. Every network mutation is inside two disposable namespaces.
# Does not stop Nadkop or write config/nodes/catalogue/HWID/firewall/DNS settings.
set -eu
[ "${NADKOP_TUNER_VM_TEST:-}" = 1 ] || { echo 'Set NADKOP_TUNER_VM_TEST=1 on a dedicated OpenWrt VM';exit 2; }
[ -r /etc/openwrt_release ] && [ "$(id -u)" = 0 ] || exit 2
[ -x /tmp/nadkop-tuner-http ] || { echo 'Build/upload scripts/fixtures/tuner-http.go as /tmp/nadkop-tuner-http';exit 2; }
umask 077
work="$(mktemp -d /tmp/nadkop-tuner-test.XXXXXX)"
router=nktune-router
server=nktune-server
engine= fixture= job=
passes=0
runtime=/var/run/nadkop/zapret-tune
if [ -f "$runtime/active" ]; then
 previous="$(cat "$runtime/active")"
 [ "$(jsonfilter -i "$runtime/$previous/job.json" -e '@.restored')" = true ] || { echo 'Previous tune has not restored; refusing test';rm -rf "$work";exit 2; }
fi
for ns in "$router" "$server";do [ ! -e "/var/run/netns/$ns" ] || { rm -rf "$work";exit 2; };done
if [ -d "$runtime" ]; then mv "$runtime" "$work/previous-tuner";fi
snapshot(){
 sha256sum /etc/nadkop/config.json /etc/nadkop/nodes.json /etc/nadkop/servers.json /etc/nadkop/hwid /etc/config/firewall /etc/config/dhcp
 if [ -e /etc/nadkop/.save-state.json ];then sha256sum /etc/nadkop/.save-state.json;else echo 'save-state absent';fi
 if /etc/init.d/nadkop enabled;then echo 'autostart enabled';else echo 'autostart disabled';fi
 readlink /var/run/nadkop/current
 ubus call service list '{"name":"nadkop"}' | jsonfilter -e '@.nadkop.instances.*.pid'
 nft list table inet nadkop
}
snapshot > "$work/before"
cleanup(){
	cleanup_result=$?
	trap - EXIT HUP INT TERM
	set +e
 if [ -n "$job" ]; then rpc "{\"action\":\"cancel\",\"job\":\"$job\"}" >/dev/null 2>&1 || true; sleep 1;fi
 [ -z "$fixture" ] || kill "$fixture" 2>/dev/null || true
 [ -z "$engine" ] || kill "$engine" 2>/dev/null || true
 ip netns del "$router" 2>/dev/null || true
 ip netns del "$server" 2>/dev/null || true
 rm -rf "/etc/netns/$router"
 # Reaping known workers precedes removing their runtime journal.
 if [ -f "$runtime/active" ];then
  active="$(cat "$runtime/active")"
  for kind in workerPID enginePID;do pid="$(jsonfilter -i "$runtime/$active/job.json" -e "@.$kind" 2>/dev/null || true)";[ -z "$pid" ] || kill "$pid" 2>/dev/null || true;done
 fi
 rm -rf "$runtime"
 [ ! -d "$work/previous-tuner" ] || mv "$work/previous-tuner" "$runtime"
 snapshot > "$work/after" || cleanup_result=1
 if ! cmp -s "$work/before" "$work/after";then cleanup_result=1;echo 'FAIL: production state changed';diff -u "$work/before" "$work/after" || true;else echo 'PASS: config/nodes/catalogue/HWID/firewall/DNS/generation/PIDs/nft unchanged';fi
 rm -rf "$work"
 exit "$cleanup_result"
}
trap cleanup EXIT
trap 'exit 2' HUP INT TERM
must(){ "$@" || { echo "FAIL: $*";exit 1; }; }
ok(){ passes=$((passes+1));echo "PASS: $*"; }
rpc(){ printf '%s\n' "$1" | ip netns exec "$router" /usr/bin/nadkop rpc call zaprettune; }
field(){ jsonfilter -i "$runtime/$job/job.json" -e "@.$1"; }
wait_terminal(){
 n=0
 while [ "$n" -lt 250 ];do
  state="$(field status)"
  case "$state" in queued|running|restoring) ;; *) return 0;; esac
  n=$((n+1));sleep 1
 done
 echo "FAIL: timeout waiting for terminal result ($state)";exit 1
}
start(){
 response="$(rpc "{\"action\":\"start\",\"wan\":\"nktune-wan\",\"targets\":[\"$1\"],\"protocols\":[\"$2\"],\"revision\":\"$revision\"}")"
 job="$(printf '%s' "$response" | jsonfilter -e '@.job.id')"
 [ -n "$job" ] || { echo "$response";exit 1; }
}
wait_queue(){
 n=0
 while [ "$n" -lt 120 ];do
  if ip netns exec "$router" cat /proc/net/netfilter/nfnetlink_queue | awk '$1==201{found=1} END{exit !found}';then return 0;fi
  n=$((n+1));sleep 1
 done
 echo 'FAIL: temporary NFQUEUE never appeared';exit 1
}
assert_clean(){
 must test "$(field restored)" = true
 if ip netns exec "$router" nft list tables | grep -q nadkop_tune_;then echo 'FAIL: temporary table remains';exit 1;fi
 if ip netns exec "$router" cat /proc/net/netfilter/nfnetlink_queue | awk '$1==201{found=1} END{exit !found}';then echo 'FAIL: temporary queue remains';exit 1;fi
}
must ip netns add "$router"
must ip netns add "$server"
must ip link add nktune-wan type veth peer name nktune-peer
must ip link set nktune-wan netns "$router"
must ip link set nktune-peer netns "$server"
must ip -n "$router" link set lo up
must ip -n "$server" link set lo up
must ip -n "$router" addr add 1.1.1.2/24 dev nktune-wan
must ip -n "$server" addr add 1.1.1.1/24 dev nktune-peer
must ip -n "$router" link set nktune-wan up
must ip -n "$server" link set nktune-peer up
mkdir -p "/etc/netns/$router"
printf '1.1.1.1 target.tuner.test\n127.0.0.1 private.tuner.test\n' > "/etc/netns/$router/hosts"
ip netns exec "$server" /tmp/nadkop-tuner-http > "$work/http.log" 2>&1 & fixture=$!
sleep 1
revision="$(printf '{}\n' | /usr/bin/nadkop rpc call get | jsonfilter -e '@.revision')"
start target.tuner.test http
wait_terminal
must test "$(field status)" = unneeded
assert_clean
ok 'working direct access never becomes a found strategy'
start private.tuner.test http
wait_terminal
must test "$(field status)" = failed
assert_clean
ok 'private DNS answer rejected before network changes'
start target.tuner.test https
wait_terminal
must test "$(field status)" = inconclusive
must test "$(field 'results[0].code')" = tls_certificate
assert_clean
ok 'untrusted TLS certificate cannot produce a strategy'
# Reject only baseline socket marks. Candidate traffic traverses real nfqws and
# its isolated queue. This fixture proves plumbing, not ISP DPI effectiveness.
ip netns exec "$router" nft -f - <<'NFT'
table inet tuner_fixture {
 chain output { type filter hook output priority -160; policy accept;
 meta mark == 0x28000001 tcp dport 80 reject with tcp reset
 }
}
NFT
start target.tuner.test http
wait_terminal
must test "$(field status)" = completed
must test "$(field winner)" = multisplit
must test "$(field 'results[1].queued')" = true
assert_clean
selection="$(rpc "{\"action\":\"select\",\"wan\":\"nktune-wan\",\"revision\":\"$revision\",\"job\":\"$job\",\"strategy\":\"multisplit\"}")"
must test "$(printf '%s' "$selection" | jsonfilter -e '@.strategy')" = multisplit
ok 'two real candidate probes + alternating failed baseline + explicit selection'
# Keep candidate connections pending so process failures are reproducible.
ip netns exec "$server" nft -f - <<'NFT'
table inet tuner_server_fixture {
 chain input {
  type filter hook input priority -160; policy accept;
  tcp dport 80 drop
 }
}
NFT
start target.tuner.test http
wait_queue
must rpc "{\"action\":\"cancel\",\"job\":\"$job\"}" > "$work/cancel"
wait_terminal
must test "$(field status)" = cancelled
assert_clean
ok 'cancel cleans queue/process/table while a request is pending'
start target.tuner.test http
wait_queue
worker="$(field workerPID)"
must kill -KILL "$worker"
wait_terminal
must test "$(field status)" = failed
assert_clean
ok 'guard restores after SIGKILL of worker without status polling'
start target.tuner.test http
wait_queue
guard="$(cut -d: -f1 "$runtime/$job/guard-ready")"
must kill -KILL "$guard"
wait_terminal
must test "$(field status)" = failed
assert_clean
ok 'worker detects dead guard, stops and schedules independent cleanup'
start target.tuner.test http
wait_queue
must kill -KILL "$(field enginePID)"
wait_terminal
must test "$(field status)" != completed
assert_clean
ok 'dead nfqws cannot yield a confirmed strategy'
# Deadline is read independently by the guard; shorten only this private journal.
start target.tuner.test http
wait_queue
must kill -STOP "$(field workerPID)"
sed 's/"expiresAt":[0-9]*/"expiresAt":1/' "$runtime/$job/job.json" > "$work/expired"
mv "$work/expired" "$runtime/$job/job.json"
wait_terminal
must test "$(field status)" = failed
assert_clean
ok 'guard deadline restores resources without browser/RPC'
echo "Passed $passes isolated tuner scenarios"
