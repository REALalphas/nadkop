#!/bin/sh
# Run on a disposable OpenWrt VM with nadkop and luci-app-nadkop installed.
# jshn uses unset variables internally and does not support set -u.
set -e
[ -r /etc/openwrt_release ] && [ "$(id -u)" = 0 ] || exit 1
. /usr/share/libubox/jshn.sh
umask 077
work="$(mktemp -d /tmp/nadkop-rpc-test.XXXXXX)"
was_running=0
/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
cp /etc/nadkop/config.json "$work/config"
cp /etc/nadkop/nodes.json "$work/nodes"
had_catalog=0
if [ -f /etc/nadkop/servers.json ]; then
	cp /etc/nadkop/servers.json "$work/servers"
	had_catalog=1
fi
cleanup() {
	/etc/init.d/nadkop stop >/dev/null 2>&1 || true
	cp "$work/config" /etc/nadkop/config.json
	cp "$work/nodes" /etc/nadkop/nodes.json
	if [ "$had_catalog" = 1 ]; then cp "$work/servers" /etc/nadkop/servers.json; else rm -f /etc/nadkop/servers.json; fi
	[ "$was_running" = 0 ] || /etc/init.d/nadkop start
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
ok() {
	if [ -n "$(jsonfilter -i "$1" -e '@.error')" ]; then
		echo 'FAIL: RPC returned an error' >&2
		cat "$1" >&2
		exit 1
	fi
}
ubus call nadkop get > "$work/get"
ok "$work/get"
revision="$(jsonfilter -i "$work/get" -e '@.revision')"
[ "${#revision}" = 64 ]
ubus call nadkop status > "$work/status"
ok "$work/status"
ubus call session access '{"ubus_rpc_session":"00000000000000000000000000000000","scope":"ubus","object":"nadkop","function":"get"}' > "$work/acl"
[ "$(jsonfilter -i "$work/acl" -e '@.access')" = false ]
echo 'PASS: get/status; anonymous sessions cannot read credentials'

ubus call session access '{"ubus_rpc_session":"00000000000000000000000000000000","scope":"ubus","object":"nadkop","function":"addsource"}' > "$work/acl-source"
[ "$(jsonfilter -i "$work/acl-source" -e '@.access')" = false ]
cat > "$work/provider.json" <<'JSON'
{"title":"Lab VPN + EU!","outbounds":[{"tag":"main","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]}
JSON
json_init
json_add_string content "$work/provider.json"
json_add_string revision "$revision"
json_add_object options
json_close_object
ubus call nadkop addsource "$(json_dump)" > "$work/source"
ok "$work/source"
[ "$(jsonfilter -i "$work/source" -e '@.source.name')" = 'Lab_VPN_-_EU-' ]
revision="$(jsonfilter -i "$work/source" -e '@.revision')"
[ "${#revision}" = 64 ]
json_init
json_add_string content "$work/provider.json"
json_add_string revision "$revision"
json_add_object options
json_close_object
ubus call nadkop addsource "$(json_dump)" > "$work/source-duplicate"
ok "$work/source-duplicate"
[ "$(jsonfilter -i "$work/source-duplicate" -e '@.existing')" = true ]
echo 'PASS: immediate local JSON subscription import, title normalization, duplicate handling and admin ACL'

ubus call nadkop import '{"content":"[{\"tag\":\"lab\",\"protocol\":\"socks\",\"settings\":{\"servers\":[{\"address\":\"127.0.0.1\",\"port\":2080}]}}]"}' > "$work/import"
ok "$work/import"
[ "$(jsonfilter -i "$work/import" -e '@.nodes[0].tag')" = lab ]
json_init
json_add_object config
json_add_array sources
json_add_object ''
json_add_string name local
json_add_string file /etc/nadkop/nodes.json
json_close_object
json_close_array
json_add_array rules
json_close_array
json_add_string default direct
json_add_int socksPort 1080
json_close_object
json_add_array nodes
json_close_array
json_add_string revision "$revision"
request="$(json_dump)"
ubus call nadkop save "$request" > "$work/save"
ok "$work/save"
ubus call nadkop save "$request" > "$work/stale"
[ -n "$(jsonfilter -i "$work/stale" -e '@.error')" ]
ubus call nadkop nodes > "$work/nodes-result"
ok "$work/nodes-result"
# No rules or pins remain: removed connections must be cleaned immediately.
[ -z "$(jsonfilter -i "$work/save" -e '@.servers[*].id')" ]
[ -z "$(jsonfilter -i "$work/nodes-result" -e '@.nodes[*].id')" ]
# Persist a named rule and read it back through the same RPC used by LuCI.
revision="$(jsonfilter -i "$work/save" -e '@.revision')"
json_init
json_add_object config
json_add_array sources
json_add_object ''
json_add_string name local
json_add_string file /etc/nadkop/nodes.json
json_close_object
json_close_array
json_add_array rules
json_add_object ''
json_add_string name 'Video'
json_add_string action direct
json_add_array domains
json_add_string '' named-rule.test
json_close_array
json_close_object
json_close_array
json_close_object
json_add_array nodes
json_close_array
json_add_string revision "$revision"
ubus call nadkop save "$(json_dump)" > "$work/named-save"
ok "$work/named-save"
ubus call nadkop get > "$work/named-get"
ok "$work/named-get"
[ "$(jsonfilter -i "$work/named-get" -e '@.config.rules[0].name')" = 'Video' ]
ubus call nadkop check > "$work/check"
ok "$work/check"
[ "$(jsonfilter -i "$work/check" -e '@.ok')" = true ]
[ "$(jsonfilter -i "$work/check" -e '@.steps[*].id' | wc -l)" -eq 6 ]
[ "$(jsonfilter -i "$work/check" -e '@.steps[0].state')" = ok ]
[ "$(jsonfilter -i "$work/check" -e '@.steps[3].state')" = ok ]
[ "$(jsonfilter -i "$work/check" -e '@.steps[4].state')" = skipped ]
[ "$(jsonfilter -i "$work/check" -e '@.steps[5].state')" = skipped ]
echo 'PASS: rule name save/reload; six diagnostic stages with direct-only Zapret skips'
echo 'PASS: import, save empty local source, revision conflict, unbound catalogue cleanup, nodes, Xray preflight'

ubus call nadkop service '{"action":"restart"}' > "$work/restart"
ok "$work/restart"
sleep 1
ubus call nadkop status > "$work/status"
[ "$(jsonfilter -i "$work/status" -e '@.instances.xray.running')" = true ]
ubus call nadkop service '{"action":"stop"}' > "$work/stop"
ok "$work/stop"
sleep 1
if /etc/init.d/nadkop running >/dev/null 2>&1; then
	echo 'FAIL: service survived RPC stop' >&2; exit 1
fi
ubus call nadkop service '{"action":"start;false"}' > "$work/action"
[ -n "$(jsonfilter -i "$work/action" -e '@.error')" ]
echo 'PASS: RPC restart, running Xray, stop, rejected unsupported action'

[ -s /www/luci-static/resources/view/nadkop/overview.js ]
[ -s /www/luci-static/resources/nadkop/picker.js ]
[ -s /www/luci-static/resources/nadkop/picker.css ]
[ -s /usr/share/luci/menu.d/luci-app-nadkop.json ]
[ -s /usr/share/rpcd/acl.d/luci-app-nadkop.json ]
echo 'LuCI/RPC smoke tests passed'
