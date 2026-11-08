#!/bin/sh
# B2: run ONLY on the dedicated OpenWrt VM. This takes just over two hours.
# All subscription data and routes below are synthetic. Original data and the
# original prepared generation are kept on the VM until restoration is verified.
set -eu
umask 077

[ "$(id -u)" = 0 ] && [ -r /etc/openwrt_release ] || { echo 'FAIL: this is not a root OpenWrt session'; exit 2; }
ip -4 -o addr show | awk '$4 ~ /^192\.168\.122\.240\// {found=1} END {exit !found}' || { echo 'FAIL: unexpected VM address'; exit 2; }
for cmd in nadkop xray ubus jsonfilter curl uhttpd nft ip cmp timeout; do
 command -v "$cmd" >/dev/null || { echo "FAIL: missing test dependency $cmd"; exit 2; }
done
for port in 11880 18881 18882 21881 21882; do
 hex=$(printf '%04X' "$port")
 if awk -v p=":$hex" '$4 == "0A" && index($2,p) == length($2)-length(p)+1 {found=1} END {exit !found}' /proc/net/tcp /proc/net/tcp6; then
  echo "FAIL: fixture TCP port $port already in use"; exit 2
 fi
done
lock=/tmp/nadkop-b2-hourly.lock
mkdir "$lock" 2>/dev/null || { echo 'FAIL: B2 lock already exists'; exit 2; }
work=$(mktemp -d /tmp/nadkop-b2-hourly.XXXXXX) || { rmdir "$lock"; exit 2; }
chmod 711 "$work"
backup=$work/private
mkdir -m 700 "$backup"
changed=0
backup_ready=0
http_a='' http_b='' proxy_a='' proxy_b=''
original_running=0
original_enabled=0
original_gen=

note() { printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }
fail() { note "FAIL: $*"; exit 1; }
uptime_now() { awk '{print $1}' /proc/uptime; }
elapsed() { awk -v a="$1" -v b="$2" 'BEGIN {printf "%.2f", b-a}'; }
at_least() { awk -v a="$1" -v b="$2" -v min="$3" 'BEGIN {exit !(b-a >= min)}'; }
instance() { jsonfilter -i "$1" -e "@.nadkop.instances.$2.$3" 2>/dev/null || :; }
capture_service() { ubus call service list '{"name":"nadkop"}' > "$1"; }
save_file() {
 name=$1 path=$2
 if [ -e "$path" ] || [ -L "$path" ]; then
  [ -f "$path" ] && [ ! -L "$path" ] || fail "unexpected type for $name"
  cp -a "$path" "$backup/$name"
  printf 'present\n' > "$backup/$name.presence"
  ls -ln "$path" | awk '{print $1, $2, $3, $4}' > "$backup/$name.metadata"
 else
  printf 'absent\n' > "$backup/$name.presence"
 fi
}
restore_file() {
 name=$1 path=$2
 if [ "$(cat "$backup/$name.presence")" = present ]; then
  rm -f "$path" && cp -a "$backup/$name" "$path" || return 1
 else
  rm -f "$path" || return 1
 fi
}
check_file() {
 name=$1 path=$2
 if [ "$(cat "$backup/$name.presence")" = present ]; then
  [ -f "$path" ] && [ ! -L "$path" ] && cmp -s "$backup/$name" "$path" || return 1
  ls -ln "$path" | awk '{print $1, $2, $3, $4}' > "$backup/metadata.after"
  cmp -s "$backup/$name.metadata" "$backup/metadata.after"
 else
  [ ! -e "$path" ] && [ ! -L "$path" ]
 fi
}
network_snapshot() {
 dest=$1
 nft -s list table inet nadkop > "$dest.table" 2>/dev/null || :
 nft -s list chain inet fw4 input > "$dest.fw4" 2>/dev/null || :
 ip -4 rule show > "$dest.rule4" 2>/dev/null || :
 ip -6 rule show > "$dest.rule6" 2>/dev/null || :
 ip -4 route show table 100 > "$dest.route4" 2>/dev/null || :
 ip -6 route show table 100 > "$dest.route6" 2>/dev/null || :
}
network_equal() {
 for item in table fw4 rule4 rule6 route4 route6; do
  cmp -s "$backup/net-before.$item" "$backup/net-after.$item" || return 1
 done
}
kill_fixture() {
 for pid in "$http_a" "$http_b" "$proxy_a" "$proxy_b"; do
  [ -z "$pid" ] || kill "$pid" 2>/dev/null || :
 done
 for pid in "$http_a" "$http_b" "$proxy_a" "$proxy_b"; do
  [ -z "$pid" ] || wait "$pid" 2>/dev/null || :
 done
}
cleanup() {
 rc=$?
 trap - EXIT HUP INT TERM
 set +e
 restore_failed=0
 kill_fixture
 if [ "$changed" = 1 ] && [ "$backup_ready" = 1 ]; then
  note 'CLEANUP: stopping test generation and restoring original state'
  timeout 30 /etc/init.d/nadkop stop > "$backup/stop-restore.log" 2>&1 || restore_failed=1
  for item in 'config /etc/nadkop/config.json' 'nodes /etc/nadkop/nodes.json' 'servers /etc/nadkop/servers.json' 'hwid /etc/nadkop/hwid' 'hosts /etc/hosts' 'firewall /etc/config/firewall'; do
   set -- $item
   restore_file "$1" "$2" || restore_failed=1
  done
  if [ "$original_running" = 1 ]; then
   rm -rf "$original_gen"
   cp -a "$backup/generation" "$original_gen" || restore_failed=1
   # nfqws dry-run's file_open_test needs the preflight files owned by root.
   # An already running generation has these 0600 files owned by nfqws; init
   # transfers them back after preflight. Do not alter the private backup.
   for f in zapret-hosts.txt zapret-ips.txt; do
    [ ! -f "$original_gen/$f" ] || chown 0:0 "$original_gen/$f" || restore_failed=1
   done
   NADKOP_PREPARED="$original_gen" timeout 30 /etc/init.d/nadkop start > "$backup/start-restore.log" 2>&1 || restore_failed=1
  fi
  # procd keeps only recent generations; preserve all pre-existing ones and
  # remove generations created by this test after returning to the original.
  for dir in /var/run/nadkop/generation.*; do
   [ -d "$dir" ] || continue
   if ! grep -Fxq "$dir" "$backup/generations-before"; then rm -rf "$dir" || restore_failed=1; fi
  done
  for saved in "$backup"/generations/generation.*; do
   [ -d "$saved" ] || continue
   dir=/var/run/nadkop/${saved##*/}
   [ -d "$dir" ] || cp -a "$saved" "$dir" || restore_failed=1
  done
  restore_file status /var/run/nadkop/update-status.json || restore_failed=1
  for item in 'ping /proc/sys/net/ipv4/ping_group_range' 'forward4 /proc/sys/net/ipv4/ip_forward' 'forward6 /proc/sys/net/ipv6/conf/all/forwarding'; do
   set -- $item
   if ! cmp -s "$backup/sysctl-$1" "$2"; then cat "$backup/sysctl-$1" > "$2" || restore_failed=1; fi
   cmp -s "$backup/sysctl-$1" "$2" || restore_failed=1
  done
  # Poll only for the original procd instances that were previously running.
  ready=0 n=0
  while [ "$n" -lt 30 ]; do
   capture_service "$backup/service-after" 2>/dev/null || :
   ready=1
   for inst in xray monitor nfqws updater; do
    before=$(instance "$backup/service-before" "$inst" running)
    after=$(instance "$backup/service-after" "$inst" running)
    [ "$before" = "$after" ] || ready=0
   done
   [ "$ready" = 1 ] && break
   n=$((n+1)); sleep 1
  done
  [ "$ready" = 1 ] || restore_failed=1
  for item in 'config /etc/nadkop/config.json' 'nodes /etc/nadkop/nodes.json' 'servers /etc/nadkop/servers.json' 'hwid /etc/nadkop/hwid' 'hosts /etc/hosts' 'firewall /etc/config/firewall' 'status /var/run/nadkop/update-status.json'; do
   set -- $item
   if check_file "$1" "$2"; then note "RESTORE: $1 bytes, presence and metadata match"; else note "RESTORE FAIL: $1 differs"; restore_failed=1; fi
  done
  if [ "$original_running" = 1 ]; then
   [ "$(readlink /var/run/nadkop/current)" = "$original_gen" ] || restore_failed=1
   cmp -s "$backup/generation/xray.json" "$original_gen/xray.json" || restore_failed=1
   /etc/init.d/nadkop running >/dev/null 2>&1 || restore_failed=1
  else
   if /etc/init.d/nadkop running >/dev/null 2>&1; then restore_failed=1; fi
  fi
  now_enabled=0
  /etc/init.d/nadkop enabled >/dev/null 2>&1 && now_enabled=1
  [ "$original_enabled" = "$now_enabled" ] || restore_failed=1
  network_snapshot "$backup/net-after"
  if network_equal; then note 'RESTORE: stateless Nadkop/firewall rules and policy routes match'; else note 'RESTORE FAIL: network rules or policy routes differ'; restore_failed=1; fi
  if [ "$restore_failed" = 0 ]; then
   note 'RESTORE: original generation, service instances and enabled/running state match'
  else
   note "RESTORE FAIL: inspect private VM backup at $backup; backup retained"
   rc=1
  fi
 fi
 if [ "$restore_failed" = 0 ]; then rm -rf "$work"; rmdir "$lock" 2>/dev/null || :; fi
 exit "$rc"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
# A lost SSH transport makes a progress write fail and enter EXIT cleanup;
# ignore SIGPIPE so that restoration can continue even without a log receiver.
trap '' PIPE

save_file config /etc/nadkop/config.json
save_file nodes /etc/nadkop/nodes.json
save_file servers /etc/nadkop/servers.json
save_file hwid /etc/nadkop/hwid
save_file hosts /etc/hosts
save_file firewall /etc/config/firewall
save_file status /var/run/nadkop/update-status.json
/etc/init.d/nadkop running >/dev/null 2>&1 && original_running=1
/etc/init.d/nadkop enabled >/dev/null 2>&1 && original_enabled=1
capture_service "$backup/service-before"
network_snapshot "$backup/net-before"
mkdir "$backup/generations"
: > "$backup/generations-before"
for dir in /var/run/nadkop/generation.*; do
 [ -d "$dir" ] || continue
 [ ! -L "$dir" ] || fail 'unexpected generation symlink'
 printf '%s\n' "$dir" >> "$backup/generations-before"
 cp -a "$dir" "$backup/generations/"
done
cat /proc/sys/net/ipv4/ping_group_range > "$backup/sysctl-ping"
cat /proc/sys/net/ipv4/ip_forward > "$backup/sysctl-forward4"
cat /proc/sys/net/ipv6/conf/all/forwarding > "$backup/sysctl-forward6"
if [ "$original_running" = 1 ]; then
 original_gen=$(readlink /var/run/nadkop/current)
 case "$original_gen" in /var/run/nadkop/generation.*) ;; *) fail 'unexpected original generation path';; esac
 [ -d "$original_gen" ] && [ ! -L "$original_gen" ] || fail 'original generation unavailable'
 cp -a "$original_gen" "$backup/generation"
fi
backup_ready=1
note "BASELINE: backup complete; running=$original_running enabled=$original_enabled"
changed=1
timeout 30 /etc/init.d/nadkop stop > "$work/stop.log" 2>&1 || fail 'cannot stop service for synthetic fixture'
rm -f /var/run/nadkop/update-status.json

mkdir "$work/a" "$work/b"
chmod 755 "$work/a" "$work/b"
printf 'b2-alpha\n' > "$work/a/index.html"
printf 'b2-beta\n' > "$work/b/index.html"
chmod 644 "$work/a/index.html" "$work/b/index.html"
uhttpd -f -p 127.0.0.1:18881 -h "$work/a" > "$work/http-a.log" 2>&1 & http_a=$!
uhttpd -f -p 127.0.0.1:18882 -h "$work/b" > "$work/http-b.log" 2>&1 & http_b=$!
cat > "$work/proxy-a.json" <<'JSON'
{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":21881,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom","settings":{"redirect":"127.0.0.1:18881"}}]}
JSON
cat > "$work/proxy-b.json" <<'JSON'
{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":21882,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom","settings":{"redirect":"127.0.0.1:18882"}}]}
JSON
xray run -config "$work/proxy-a.json" > "$work/proxy-a.log" 2>&1 & proxy_a=$!
xray run -config "$work/proxy-b.json" > "$work/proxy-b.log" 2>&1 & proxy_b=$!
n=0
while [ "$n" -lt 20 ]; do
 a=$(curl --fail --silent --noproxy '' --connect-timeout 1 --max-time 2 --socks5-hostname 127.0.0.1:21881 http://b2-proxy.test:18881/ 2>/dev/null) || a=
 b=$(curl --fail --silent --noproxy '' --connect-timeout 1 --max-time 2 --socks5-hostname 127.0.0.1:21882 http://b2-proxy.test:18881/ 2>/dev/null) || b=
 [ "$a" = b2-alpha ] && [ "$b" = b2-beta ] && break
 n=$((n+1)); sleep 1
done
[ "$a" = b2-alpha ] && [ "$b" = b2-beta ] || fail 'synthetic SOCKS and HTTP fixture did not become ready'
note 'FIXTURE: both synthetic SOCKS exits route to distinct local HTTP responses'
cat > "$work/source.json" <<'JSON'
[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":21881}]}}]
JSON
printf '[]\n' > /etc/nadkop/nodes.json
printf '{"version":1,"servers":[]}\n' > /etc/nadkop/servers.json
printf '{"sources":[{"name":"b2","file":"%s/source.json"}],"rules":[],"default":"block","socksPort":11880,"updateIntervalHours":1}\n' "$work" > /etc/nadkop/config.json
nadkop nodes > "$work/nodes.log" 2>&1 || fail 'cannot initialize synthetic catalogue'
ref=$(jsonfilter -i /etc/nadkop/servers.json -e '@.servers[0].id')
case "$ref" in srv_[0-9a-f]*) ;; *) fail 'synthetic catalogue reference missing';; esac
printf '{"sources":[{"name":"b2","file":"%s/source.json"}],"rules":[{"action":"proxy","domains":["b2-proxy.test"],"nodeRef":"%s"}],"default":"block","socksPort":11880,"updateIntervalHours":1}\n' "$work" "$ref" > /etc/nadkop/config.json
start_before=$(uptime_now)
start_epoch=$(date +%s)
timeout 30 /etc/init.d/nadkop start > "$work/start.log" 2>&1 || fail 'cannot start synthetic service'

fetch() { curl --fail --silent --show-error --noproxy '' --connect-timeout 2 --max-time 4 --socks5-hostname 127.0.0.1:11880 http://b2-proxy.test:18881/ 2> "$work/fetch-error"; }
wait_route() {
 expected=$1 n=0
 while [ "$n" -lt 30 ]; do
  result=$(fetch) || result=
  [ "$result" = "$expected" ] && return 0
  n=$((n+1)); sleep 1
 done
 return 1
}
check_pid() {
 capture_service "$work/service"
 [ "$(instance "$work/service" updater pid)" = "$updater" ] || return 1
 [ "$(instance "$work/service" updater running)" = true ] || return 1
 [ -r "/proc/$updater/stat" ] && [ "$(awk '{print $22}' "/proc/$updater/stat")" = "$start_ticks" ]
}
wait_route b2-alpha || fail 'initial synthetic proxy route did not become ready'
capture_service "$work/service"
updater=$(instance "$work/service" updater pid)
case "$updater" in ''|*[!0-9]*) fail 'updater PID missing';; esac
start_ticks=$(awk '{print $22}' "/proc/$updater/stat")
check_pid || fail 'updater instance not running'
initial_gen=$(readlink /var/run/nadkop/current)
initial_xray=$(readlink -f /var/run/nadkop/current/xray.json)
[ -f "$initial_xray" ] || fail 'initial current/xray.json does not resolve'
[ "$(jsonfilter -i /etc/nadkop/config.json -e '@.rules[0].nodeRef')" = "$ref" ] || fail 'initial rule reference mismatch'
note "START: updater_pid=$updater process_start_ticks=$start_ticks start_before_uptime=$start_before start_epoch=$start_epoch generation=$initial_gen ref=$ref route=b2-alpha"

cat > "$work/source.new" <<'JSON'
[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":21882}]}}]
JSON
mv "$work/source.new" "$work/source.json"
[ "$(fetch)" = b2-alpha ] || fail 'route changed before the scheduled refresh'
note 'SOURCE: changed synthetic SOCKS endpoint 21881 -> 21882; active route still b2-alpha'
if [ "${1:-}" = --exercise-term ]; then
 note 'SIGNAL TEST: sending TERM to runner; exit 143 is expected after cleanup'
 kill -TERM $$
 fail 'TERM did not terminate runner'
fi

wait_new_status() {
 old=$1 desired=$2 phase_start=$(uptime_now) last_report=$phase_start
 while :; do
  check_pid || fail 'updater PID or process identity changed while waiting'
  now=$(uptime_now)
  if [ -f /var/run/nadkop/update-status.json ]; then
   status_time=$(jsonfilter -i /var/run/nadkop/update-status.json -e '@.updatedAt')
   status_ok=$(jsonfilter -i /var/run/nadkop/update-status.json -e '@.ok')
   if [ -n "$status_time" ] && [ "$status_time" != "$old" ]; then
    [ "$status_ok" = "$desired" ] || fail "scheduled status was ok=$status_ok, expected=$desired"
    status_seen=$now
    return 0
   fi
  fi
  if at_least "$phase_start" "$now" 3780; then fail 'scheduled refresh timeout after 3780 seconds'; fi
  if at_least "$last_report" "$now" 45; then
   note "WAIT: expected_ok=$desired elapsed_seconds=$(elapsed "$phase_start" "$now") updater_pid=$updater"
   last_report=$now
  fi
  sleep 5
 done
}

wait_new_status '' true
first_updated=$status_time
first_seen=$status_seen
at_least "$start_before" "$first_seen" 3600 || fail 'success observed before one real hour from start request'
first_gen=$(readlink /var/run/nadkop/current)
first_xray=$(readlink -f /var/run/nadkop/current/xray.json)
[ "$first_gen" != "$initial_gen" ] && [ "$first_xray" != "$initial_xray" ] || fail 'successful schedule did not publish a new generation'
[ -f "$first_xray" ] || fail 'stable current/xray.json path does not resolve after success'
[ "$(jsonfilter -i /etc/nadkop/servers.json -e '@.servers[0].id')" = "$ref" ] || fail 'catalogue reference changed after source endpoint update'
[ "$(jsonfilter -i /etc/nadkop/config.json -e '@.rules[0].nodeRef')" = "$ref" ] || fail 'rule reference changed after source endpoint update'
[ "$(jsonfilter -i /etc/nadkop/servers.json -e '@.servers[0].available')" = true ] || fail 'updated server is unavailable in catalogue'
check_pid || fail 'updater PID changed after scheduled success'
wait_route b2-beta || fail 'new source endpoint is not carrying proxy traffic'
capture_service "$work/service"
xray_pid=$(instance "$work/service" xray pid)
[ -n "$xray_pid" ] && [ -r "/proc/$xray_pid/cmdline" ] || fail 'Xray process missing after refresh'
xray_arg=$(tr '\000' '\n' < "/proc/$xray_pid/cmdline" | sed -n '4p')
[ "$(readlink -f "$xray_arg")" = "$first_xray" ] || fail 'Xray is not running the new generation'
cp /var/run/nadkop/current/xray.json "$work/first-xray.json"
cp /etc/nadkop/servers.json "$work/first-servers.json"
note "PASS SUCCESS: status_ok=true updatedAt=$first_updated real_elapsed_seconds=$(elapsed "$start_before" "$first_seen") updater_pid=$updater generation=$first_gen ref=$ref route=b2-beta"

printf '{corrupt\n' > "$work/source.json"
[ "$(fetch)" = b2-beta ] || fail 'working proxy route failed immediately after source corruption'
note 'SOURCE: synthetic local subscription deliberately corrupted; waiting for next real hourly tick'
wait_new_status "$first_updated" false
second_updated=$status_time
second_seen=$status_seen
at_least "$first_seen" "$second_seen" 3570 || fail 'failure observed too soon for the next real hourly tick'
[ $((second_updated-first_updated)) -ge 3590 ] || fail 'status timestamps are too close for consecutive hourly ticks'
[ "$(jsonfilter -i /var/run/nadkop/update-status.json -e '@.error')" = 'Automatic refresh failed; inspect service status' ] || fail 'failure status is missing expected diagnostic'
[ "$(readlink /var/run/nadkop/current)" = "$first_gen" ] || fail 'failed refresh changed the working generation'
[ "$(readlink -f /var/run/nadkop/current/xray.json)" = "$first_xray" ] || fail 'failed refresh changed the stable Xray target'
cmp -s "$work/first-xray.json" /var/run/nadkop/current/xray.json || fail 'failed refresh changed the active Xray config'
cmp -s "$work/first-servers.json" /etc/nadkop/servers.json || fail 'failed refresh changed the synthetic catalogue'
[ "$(jsonfilter -i /etc/nadkop/config.json -e '@.rules[0].nodeRef')" = "$ref" ] || fail 'failed refresh changed the rule reference'
check_pid || fail 'updater PID changed after scheduled failure'
wait_route b2-beta || fail 'working route was lost after scheduled source failure'
note "PASS FAILURE: status_ok=false updatedAt=$second_updated status_delta_seconds=$((second_updated-first_updated)) observed_delta_seconds=$(elapsed "$first_seen" "$second_seen") updater_pid=$updater generation=$first_gen ref=$ref route=b2-beta error=previous-generation-retained"
note 'RESULT: both scheduled updater cycles passed; restoration follows'
