#!/bin/sh
# Fetch nadkop packages on the router itself and install them.
# Runs inside OpenWrt as root. Usage:
#   sh get-nadkop.sh BASE_URL [ARCH]
# BASE_URL is a directory containing get-nadkop.sh, install-openwrt.sh,
# sha256sums and the package files (see docs/install.md for the layout).
# ARCH defaults to the output of `uname -m` (e.g. x86_64, aarch64).
set -eu

BASE_URL="${1:-}"
ARCH="${2:-$(uname -m)}"
# Pinned release; override with NADKOP_CORE / NADKOP_UI / NADKOP_NFQWS env.
CORE="${NADKOP_CORE:-0.3.0-r6}"
UI="${NADKOP_UI:-0.3.0-r11}"
NFQWS="${NADKOP_NFQWS:-2026.11.01-r1}"

if [ ! -r /etc/openwrt_release ] || [ "$(id -u)" != 0 ]; then
	echo 'Run this script as root inside OpenWrt.' >&2
	exit 1
fi
if [ -z "$BASE_URL" ]; then
	echo "Usage: $0 BASE_URL [ARCH]" >&2
	exit 1
fi

if command -v apk >/dev/null 2>&1; then
	# Order: installer expects nfqws, core, LuCI.
	FILES="nadkop-nfqws-${NFQWS}-${ARCH}.apk nadkop-${CORE}-${ARCH}.apk luci-app-nadkop-${UI}.apk"
elif command -v opkg >/dev/null 2>&1; then
	FILES="nadkop-nfqws_${NFQWS}_${ARCH}.ipk nadkop_${CORE}_${ARCH}.ipk luci-app-nadkop_${UI}_all.ipk"
else
	echo 'Neither apk nor opkg is available.' >&2
	exit 1
fi

fetch() {
	# fetch URL DEST
	if command -v uclient-fetch >/dev/null 2>&1; then
		uclient-fetch -O "$2" "$1"
	elif command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -O "$2" "$1"
	else
		echo 'No downloader found (uclient-fetch, curl or wget).' >&2
		exit 1
	fi
}

DL=/tmp/nadkop-dl
mkdir -p "$DL"
cd "$DL"
fetch "$BASE_URL/install-openwrt.sh" "$DL/install-openwrt.sh"
fetch "$BASE_URL/sha256sums" "$DL/sha256sums"
ARGS=''
for f in $FILES; do
	fetch "$BASE_URL/$f" "$DL/$f"
	ARGS="$ARGS $DL/$f"
done
# Verify only the files we are about to install.
: > "$DL/nk.sums"
for f in $FILES; do
	grep -F "  $f" "$DL/sha256sums" >> "$DL/nk.sums" || {
		echo "Checksum entry missing for $f." >&2
		exit 1
	}
done
sha256sum -c "$DL/nk.sums"
# shellcheck disable=SC2086
sh "$DL/install-openwrt.sh" $ARGS
echo "Packages and checksums kept in $DL."
