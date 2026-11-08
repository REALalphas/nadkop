#!/bin/sh
# Install locally built nadkop packages and their repository dependencies.
set -eu

if [ ! -r /etc/openwrt_release ] || [ "$(id -u)" != 0 ]; then
	echo 'Run this script as root inside OpenWrt.' >&2
	exit 1
fi
if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	echo "Usage: $0 /path/to/nadkop-nfqws.{apk,ipk} /path/to/nadkop.{apk,ipk} [/path/to/luci-app-nadkop.{apk,ipk}]" >&2
	exit 1
fi
for package in "$@"; do
	case "$package" in
		/*) ;;
		*) echo 'Use absolute paths to the local packages.' >&2; exit 1 ;;
	esac
	[ -f "$package" ] || { echo "Package does not exist: $package" >&2; exit 1; }
done
was_running=0
was_enabled=0
if [ -x /etc/init.d/nadkop ]; then
	/etc/init.d/nadkop running >/dev/null 2>&1 && was_running=1
	/etc/init.d/nadkop enabled >/dev/null 2>&1 && was_enabled=1
fi

if command -v apk >/dev/null 2>&1; then
	for package in "$@"; do
		case "$package" in *.apk) ;; *) echo 'This OpenWrt requires .apk packages.' >&2; exit 1 ;; esac
	done
	apk update
	# SDK development packages have no repository signature.
	apk add --allow-untrusted "$@"
elif command -v opkg >/dev/null 2>&1; then
	for package in "$@"; do
		case "$package" in *.ipk) ;; *) echo 'This OpenWrt requires .ipk packages.' >&2; exit 1 ;; esac
	done
	opkg update
	opkg install "$@"
else
	echo 'Neither apk nor opkg is available.' >&2
	exit 1
fi

if [ "$was_running" = 0 ]; then
	# Restore the previous state after OpenWrt's package hooks start services.
	/etc/init.d/nadkop stop >/dev/null 2>&1 || true
fi
if [ "$was_enabled" = 0 ]; then
	/etc/init.d/nadkop disable
else
	/etc/init.d/nadkop enable
fi
chmod 600 /etc/nadkop/config.json /etc/nadkop/nodes.json
[ ! -f /etc/nadkop/hwid ] || chmod 600 /etc/nadkop/hwid
[ ! -f /etc/nadkop/servers.json ] || chmod 600 /etc/nadkop/servers.json
# The post-upgrade hook may still hold ui.lock here; retry validate the same
# way apply is retried below instead of aborting the whole installer.
validate_status=1
for delay in 2 5 10; do
	if validate_output=$(nadkop validate 2>&1); then
		[ -z "$validate_output" ] || printf '%s\n' "$validate_output"
		validate_status=0
		break
	else
		validate_status=$?
		printf '%s\n' "$validate_output" >&2
		case "$validate_output" in
			*'Another save or service operation is running'*)
				echo "nadkop validate: ui.lock contention; retrying in ${delay}s." >&2
				sleep "$delay"
				continue
				;;
		esac
		echo "Installation failed: nadkop validate failed (exit $validate_status)." >&2
		exit "$validate_status"
	fi
done
if [ "$validate_status" -ne 0 ]; then
	echo "Installation failed: nadkop validate failed after retries (exit $validate_status)." >&2
	exit "$validate_status"
fi
if [ "$was_running" = 1 ]; then
	# Load the upgraded manager only after its saved configuration validates.
	attempt=0
	apply_status=1
	# Allow package-hook service operations to release ui.lock before each attempt.
	for delay in 2 5 10; do
		attempt=$((attempt + 1))
		echo "nadkop apply: attempt $attempt/3 in ${delay}s." >&2
		sleep "$delay"
		if apply_output=$(nadkop apply 2>&1); then
			[ -z "$apply_output" ] || printf '%s\n' "$apply_output"
			apply_status=0
			break
		else
			apply_status=$?
			printf '%s\n' "$apply_output" >&2
			case "$apply_output" in
				*'Another save or service operation is running'*)
					echo 'nadkop apply: ui.lock contention; another save or service operation holds the lock.' >&2
					;;
			esac
			echo "nadkop apply: attempt $attempt/3 failed (exit $apply_status)." >&2
		fi
	done
	if [ "$apply_status" -ne 0 ]; then
		echo "Installation failed: nadkop apply failed after 3 attempts (exit $apply_status); aborting." >&2
		exit "$apply_status"
	fi
fi
if [ "$#" -eq 3 ]; then
	/etc/init.d/rpcd restart
	rm -f /tmp/luci-indexcache
	rm -rf /tmp/luci-modulecache
	echo 'LuCI: Services → Nadkop. Reload the page after installation.'
fi
echo 'Installed. Add a subscription and rules in LuCI; select LAN devices and enable transparent routing.'
echo 'Use a real Linux WAN device name; disable any separate Xray service using port 1080.'
echo 'Disable firewall flow offloading before enabling transparent routing.'
echo 'Then run: /etc/init.d/nadkop start'
