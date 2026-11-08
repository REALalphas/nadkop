#!/bin/sh
# Run on the disposable VM: monitor lifecycle for multiple versus one server.
set -eu
umask 077
work="$(mktemp -d /tmp/nadkop-procd-selection.XXXXXX)"
was_running=0
/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
for name in config.json nodes.json servers.json; do cp "/etc/nadkop/$name" "$work/$name"; done
cleanup() {
 /etc/init.d/nadkop stop >/dev/null 2>&1 || true
 for name in config.json nodes.json servers.json; do cp "$work/$name" "/etc/nadkop/$name"; done
 [ "$was_running" = 0 ] || /etc/init.d/nadkop start
 rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
/etc/init.d/nadkop stop
sleep 1
cat > /etc/nadkop/nodes.json <<'EOF'
[{"tag":"A","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2081}]}},{"tag":"B","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2082}]}}]
EOF
cat > /etc/nadkop/config.json <<'EOF'
{"sources":[{"name":"lab","file":"/etc/nadkop/nodes.json"}],"rules":[],"socksPort":1080}
EOF
nadkop nodes >/dev/null
ref_a="$(jsonfilter -i /etc/nadkop/servers.json -e '@.servers[0].id')"
ref_b="$(jsonfilter -i /etc/nadkop/servers.json -e '@.servers[1].id')"
cat > /etc/nadkop/config.json <<EOF
{"sources":[{"name":"lab","file":"/etc/nadkop/nodes.json"}],"rules":[{"name":"Group","action":"proxy","domains":["test.nadkop.example"],"nodeRefs":["$ref_a","$ref_b"],"selection":{"mode":"priority"}}],"socksPort":1080}
EOF
/etc/init.d/nadkop start
sleep 1
ubus call nadkop status > "$work/status"
[ "$(jsonfilter -i "$work/status" -e '@.instances.xray.running')" = true ]
[ "$(jsonfilter -i "$work/status" -e '@.instances.monitor.running')" = true ]
case "$(ls -l /var/run/nadkop/current/health.json)" in '-rw------- '*) ;; *) echo 'FAIL: monitoring credentials are not private'; exit 1;; esac
echo 'PASS: procd starts Xray and monitor for a multi-server rule; credentials mode 0600'
/etc/init.d/nadkop stop
sleep 1
cat > /etc/nadkop/config.json <<EOF
{"sources":[{"name":"lab","file":"/etc/nadkop/nodes.json"}],"rules":[{"action":"proxy","domains":["test.nadkop.example"],"nodeRef":"$ref_a"}],"socksPort":1080}
EOF
/etc/init.d/nadkop start
sleep 1
ubus call nadkop status > "$work/status"
[ "$(jsonfilter -i "$work/status" -e '@.instances.xray.running')" = true ]
[ -z "$(jsonfilter -i "$work/status" -e '@.instances.monitor.running')" ]
[ ! -s /var/run/nadkop/current/health.json ]
echo 'PASS: single-server rule uses Xray without a monitor or probe listeners'
