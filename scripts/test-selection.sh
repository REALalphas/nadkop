#!/bin/sh
# Isolated OpenWrt test: real proxy probes, priorities, latency, URL and fail-closed.
set -eu
umask 077
binary="${1:-/usr/bin/nadkop}"
work="$(mktemp -d /tmp/nadkop-selection.XXXXXX)"
cp /etc/hosts "$work/hosts"
a_pid= b_pid= xray_pid= monitor_pid= http_pid=
cleanup() {
 for pid in "$monitor_pid" "$xray_pid" "$a_pid" "$b_pid" "$http_pid"; do [ -z "$pid" ] || kill "$pid" 2>/dev/null || true; done
 cp "$work/hosts" /etc/hosts
 rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mkdir "$work/cgi-bin"
cat > "$work/cgi-bin/probe" <<'EOF'
#!/bin/sh
case "$QUERY_STRING:$REMOTE_ADDR" in :127.0.0.2|reverse=1:127.0.0.3) sleep 1 ;; esac
printf 'Status: 204 No Content\r\nContent-Type: text/plain\r\n\r\n'
EOF
cat > "$work/cgi-bin/who" <<'EOF'
#!/bin/sh
printf 'Content-Type: text/plain\r\n\r\n%s\n' "$REMOTE_ADDR"
EOF
chmod 755 "$work/cgi-bin/probe" "$work/cgi-bin/who"
uhttpd -f -p 127.0.0.1:18081 -h "$work" -x /cgi-bin > "$work/http.log" 2>&1 &
http_pid=$!
for pair in 'A 2081 127.0.0.2' 'B 2082 127.0.0.3'; do
 set -- $pair
 cat > "$work/$1.json" <<EOF
{"inbounds":[{"listen":"127.0.0.1","port":$2,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"freedom","sendThrough":"$3"}]}
EOF
 xray run -config "$work/$1.json" > "$work/$1.log" 2>&1 &
 if [ "$1" = A ]; then a_pid=$!; else b_pid=$!; fi
 done
cat > "$work/nodes.json" <<'EOF'
[{"tag":"A","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2081}]}},{"tag":"B","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2082}]}}]
EOF
cat > "$work/config.json" <<EOF
{"sources":[{"name":"lab","file":"$work/nodes.json"}],"rules":[],"socksPort":11080}
EOF
"$binary" nodes -config "$work/config.json" >/dev/null
ref_a="$(jsonfilter -i "$work/servers.json" -e '@.servers[0].id')"
ref_b="$(jsonfilter -i "$work/servers.json" -e '@.servers[1].id')"
cat > "$work/config.json" <<EOF
{"sources":[{"name":"lab","file":"$work/nodes.json"}],"rules":[
{"name":"Priority","action":"proxy","domains":["priority.nadkop.test"],"nodeRefs":["$ref_a","$ref_b"],"selection":{"mode":"priority"}},
{"name":"Ping","action":"proxy","domains":["ping.nadkop.test"],"nodeRefs":["$ref_a","$ref_b"],"selection":{"mode":"ping"}},
{"name":"URL","action":"proxy","domains":["url.nadkop.test"],"nodeRefs":["$ref_a","$ref_b"],"selection":{"mode":"url","url":"http://127.0.0.1:18081/cgi-bin/probe?reverse=1"}}
],"socksPort":11080}
EOF
"$binary" validate -config "$work/config.json"
"$binary" generate -config "$work/config.json" -output "$work/generated"
xray run -test -config "$work/generated/xray.json"
# Use the same deterministic local URL for default checks in this isolated fixture.
sed -i 's#https://connectivitycheck.gstatic.com/generate_204#http://127.0.0.1:18081/cgi-bin/probe#g' "$work/generated/health.json"
printf '\n127.0.0.1 priority.nadkop.test ping.nadkop.test url.nadkop.test\n' >> /etc/hosts
xray run -config "$work/generated/xray.json" > "$work/xray.log" 2>&1 &
xray_pid=$!
sleep 1
kill -0 "$xray_pid" || { cat "$work/xray.log"; exit 1; }
fetch() { curl --fail --silent --show-error --noproxy '' --max-time 3 --socks5-hostname 127.0.0.1:11080 "http://$1.nadkop.test:18081/cgi-bin/who"; }
if fetch priority > "$work/not-ready" 2>/dev/null; then echo 'FAIL: group allowed traffic before health checks'; exit 1; fi
"$binary" monitor -manifest "$work/generated/health.json" > "$work/monitor.log" 2>&1 &
monitor_pid=$!
selected() {
 xray api bi --server=127.0.0.1:61080 "__nadkop:rule-$1" 2>/dev/null | grep -q "lab/$2"
}
wait_selected() {
 n=0
 until selected "$1" "$2"; do
  n=$((n+1)); [ "$n" -lt 35 ] || { echo "FAIL: rule $1 did not choose $2"; cat "$work/monitor.log"; exit 1; }
  sleep 1
 done
}
wait_selected 0 A
wait_selected 1 B
wait_selected 2 A
[ "$(fetch priority)" = 127.0.0.2 ]
[ "$(fetch ping)" = 127.0.0.3 ]
[ "$(fetch url)" = 127.0.0.2 ]
echo 'PASS: ordered priority chooses A; minimum HTTP latency chooses B; custom URL chooses A'
kill "$a_pid"; wait "$a_pid" 2>/dev/null || true; a_pid=
wait_selected 0 B
[ "$(fetch priority)" = 127.0.0.3 ]
kill -0 "$xray_pid"
echo 'PASS: loss of first server switches to B without restarting Xray'
kill "$b_pid"; wait "$b_pid" 2>/dev/null || true; b_pid=
n=0
until xray api bi --server=127.0.0.1:61080 __nadkop:rule-0 2>/dev/null | grep -q 'block'; do
 n=$((n+1)); [ "$n" -lt 35 ] || { echo 'FAIL: total failure did not select block'; exit 1; }; sleep 1
 done
if fetch priority > "$work/fallback" 2>/dev/null; then echo 'FAIL: total proxy failure fell back to direct'; exit 1; fi
kill -0 "$xray_pid"
echo 'PASS: complete failure blocks traffic, Xray stays running'
