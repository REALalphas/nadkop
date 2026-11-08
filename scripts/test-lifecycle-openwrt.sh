#!/bin/sh
# Dedicated VM: exercise init wrappers and updater instance without external subscriptions.
set -eu
umask 077
work="$(mktemp -d /tmp/nadkop-lifecycle.XXXXXX)"
for file in config.json nodes.json servers.json;do cp "/etc/nadkop/$file" "$work/$file";done
was_running=0
/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
cleanup(){
 /etc/init.d/nadkop stop >/dev/null 2>&1 || true
 for file in config.json nodes.json servers.json;do cp "$work/$file" "/etc/nadkop/$file";done
 [ "$was_running" = 0 ] || /etc/init.d/nadkop start
 cp "$work/servers.json" /etc/nadkop/servers.json
 rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
/etc/init.d/nadkop stop >/dev/null 2>&1 || true
printf '[]\n' > /etc/nadkop/nodes.json
printf '%s\n' '{"sources":[],"rules":[],"socksPort":11880,"default":"block","updateIntervalHours":1}' > /etc/nadkop/config.json
wait_generation(){
 expected="$(readlink -f /var/run/nadkop/current/xray.json)"
 n=0
 while [ "$n" -lt 12 ];do
  ubus call service list '{"name":"nadkop"}' > "$work/status"
  pid="$(jsonfilter -i "$work/status" -e '@.nadkop.instances.xray.pid')"
  actual=
  if [ -n "$pid" ] && [ -r "/proc/$pid/cmdline" ];then actual="$(tr '\000' '\n' < "/proc/$pid/cmdline" | sed -n '4p')";fi
  if [ -n "$actual" ] && [ "$(readlink -f "$actual")" = "$expected" ] && [ "$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.running')" = true ];then return 0;fi
  n=$((n+1));sleep 1
 done
 echo 'FAIL: expected Xray generation / updater did not start'
 return 1
}
/usr/bin/timeout 20 /etc/init.d/nadkop start
wait_generation
initial="$(readlink /var/run/nadkop/current)"
updater="$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.pid')"
[ -n "$updater" ]
echo 'PASS: start launches Xray and updater for a scheduled configuration'
/usr/bin/timeout 20 /etc/init.d/nadkop reload
wait_generation
[ "$initial" != "$(readlink /var/run/nadkop/current)" ]
[ "$updater" = "$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.pid')" ]
echo 'PASS: reload completes without deadlock; updater PID survives generation change'
previous="$(readlink /var/run/nadkop/current)"
/usr/bin/timeout 20 /etc/init.d/nadkop restart
wait_generation
[ "$previous" != "$(readlink /var/run/nadkop/current)" ]
[ "$updater" = "$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.pid')" ]
echo 'PASS: restart publishes a new generation without restarting the updater'
printf '%s\n' '{"sources":[],"rules":[],"socksPort":0,"transparent":{"enabled":true,"interfaces":["lo"]}}' > /etc/nadkop/config.json
previous="$(readlink /var/run/nadkop/current)"
if /usr/bin/timeout 20 /etc/init.d/nadkop reload > "$work/rejected" 2>&1;then echo 'FAIL: invalid reload accepted';exit 1;fi
[ "$previous" = "$(readlink /var/run/nadkop/current)" ]
kill -0 "$updater"
echo 'PASS: invalid reload preserves both generation and running updater'
printf '%s\n' '{"sources":[],"rules":[],"socksPort":11880,"default":"block","updateIntervalHours":0}' > /etc/nadkop/config.json
/usr/bin/timeout 20 /etc/init.d/nadkop restart
n=0
while [ "$n" -lt 12 ];do
 ubus call service list '{"name":"nadkop"}' > "$work/status"
 if [ -z "$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.pid')" ];then break;fi
 n=$((n+1));sleep 1
done
[ -z "$(jsonfilter -i "$work/status" -e '@.nadkop.instances.updater.pid')" ]
echo 'PASS: disabling scheduled updates removes the updater instance'
/etc/init.d/nadkop stop
[ ! -e /var/run/nadkop/current ]
echo 'PASS: stop removes the applied-generation pointer'
