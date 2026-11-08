#!/bin/sh
# Bump the unified nadkop release version across all versioned files.
# Usage:
#   scripts/bump-version.sh 0.4.0
#   scripts/bump-version.sh 0.4.0-r2
#   scripts/bump-version.sh --core 0.3.0-r7 --ui 0.3.0-r12 [--nfqws 2026.11.01-r1]
#   scripts/bump-version.sh --check
# Never touches the network; safe to run in CI. Idempotent.
set -eu

project="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)"
cd "$project"

CORE_MK=openwrt/nadkop/Makefile
UI_MK=openwrt/luci-app-nadkop/Makefile
NFQWS_MK=openwrt/nadkop-nfqws/Makefile
OVERVIEW=openwrt/luci-app-nadkop/htdocs/luci-static/resources/view/nadkop/overview.js
GETTER=scripts/get-nadkop.sh
LOG=CHANGELOG.md

usage() {
  sed -n '2,10p' "$0" >&2
  exit "${1:-1}"
}

split_ver() {
  # split_ver 0.4.0-r2 -> "0.4.0 2"; split_ver 0.4.0 -> "0.4.0 1"
  case "$1" in
    *-r[0-9]*)
      printf '%s %s\n' "${1%%-r*}" "${1##*-r}" ;;
    [0-9]*.[0-9]*.[0-9]*)
      printf '%s 1\n' "$1" ;;
    *)
      echo "Bad version: $1 (want X.Y.Z or X.Y.Z-rN)" >&2
      exit 1 ;;
  esac
}

mk_get() {
  # mk_get Makefile -> "PKG_VERSION PKG_RELEASE"
  awk -F':=' '/^PKG_VERSION:=/{v=$2} /^PKG_RELEASE:=/{r=$2} END{printf "%s %s", v, r}' "$1"
}

getter_get() {
  # getter_get CORE -> pinned value from get-nadkop.sh
  grep -E "^$1=" "$GETTER" | sed -E 's/.*:-(.*)\}".*/\1/'
}

if [ "${1:-}" = "--check" ]; then
  if [ "$#" -ne 1 ]; then usage 2; fi
  set -- $(mk_get "$CORE_MK"); core="$1-r$2"
  set -- $(mk_get "$UI_MK"); ui="$1-r$2"
  fail=0
  [ "$(getter_get CORE)" = "$core" ] || { echo "CHECK FAIL: get-nadkop.sh CORE=$(getter_get CORE), want $core" >&2; fail=1; }
  [ "$(getter_get UI)" = "$ui" ] || { echo "CHECK FAIL: get-nadkop.sh UI=$(getter_get UI), want $ui" >&2; fail=1; }
  grep -q "?v=$ui" "$OVERVIEW" || { echo "CHECK FAIL: overview.js cache-buster is not ?v=$ui" >&2; fail=1; }
  if [ "$fail" -eq 0 ]; then echo "versions consistent: core=$core ui=$ui"; fi
  exit "$fail"
fi

core_full=""; ui_full=""; nfqws_full=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --core) core_full="$2"; shift 2 ;;
    --ui) ui_full="$2"; shift 2 ;;
    --nfqws) nfqws_full="$2"; shift 2 ;;
    --help|-h) usage 0 ;;
    -*) usage ;;
    *) # positional unified version
      if [ -n "$core_full" ]; then usage; fi
      core_full="$1"; ui_full="$1"; shift ;;
  esac
done
[ -n "$core_full" ] || usage

set -- $(split_ver "$core_full"); core_v="$1"; core_r="$2"
set -- $(split_ver "$ui_full"); ui_v="$1"; ui_r="$2"

set_kv() {
  # set_kv FILE KEY VALUE — replace `KEY:=...` line (Makefile style).
  python3 - "$1" "$2" "$3" <<'PY'
import re, sys
path, key, val = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path).read()
new, n = re.subn(rf'^{key}:=.*$', f'{key}:={val}', text, count=1, flags=re.M)
assert n == 1, f'{key} not found in {path}'
open(path, 'w').write(new)
PY
}

set_kv "$CORE_MK" PKG_VERSION "$core_v"
set_kv "$CORE_MK" PKG_RELEASE "$core_r"
set_kv "$UI_MK" PKG_VERSION "$ui_v"
set_kv "$UI_MK" PKG_RELEASE "$ui_r"
if [ -n "$nfqws_full" ]; then
  set -- $(split_ver "$nfqws_full")
  set_kv "$NFQWS_MK" PKG_VERSION "$1"
  set_kv "$NFQWS_MK" PKG_RELEASE "$2"
fi
set -- $(mk_get "$NFQWS_MK"); nfqws="$1-r$2"

# LuCI cache-buster tracks the UI package version.
python3 - "$OVERVIEW" "$ui_v-r$ui_r" <<'PY'
import re, sys
path, want = sys.argv[1], sys.argv[2]
text = open(path).read()
new, n = re.subn(r'\?v=[0-9][0-9.\-r]*', f'?v={want}', text)
assert n >= 1, 'cache-buster ?v= not found'
open(path, 'w').write(text and new)
print(f'cache-buster -> ?v={want} ({n} occurrence(s))')
PY

# get-nadkop.sh pinned defaults track the Makefiles.
python3 - "$GETTER" "$core_v-r$core_r" "$ui_v-r$ui_r" "$nfqws" <<'PY'
import re, sys
path, core, ui, nfqws = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
text = open(path).read()
for key, val in (('CORE', core), ('UI', ui), ('NFQWS', nfqws)):
    new, n = re.subn(rf'^{key}="\$\{{NADKOP_{key}:-[^}}]*\}}"',
                     f'{key}="${{NADKOP_{key}:-{val}}}"', text, count=1, flags=re.M)
    assert n == 1, f'{key} pin not found'
    text = new
open(path, 'w').write(text)
print(f'pins -> CORE={core} UI={ui} NFQWS={nfqws}')
PY

# CHANGELOG skeleton (date-led headers, as in this repo).
today="$(date +%F)"
if ! grep -qF -- "$core_v-r$core_r" "$LOG"; then
  python3 - "$LOG" "$today" "$core_v-r$core_r" "$ui_v-r$ui_r" <<'PY'
import sys
path, today, core, ui = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
lines = open(path, encoding='utf-8').read().splitlines()
anchor = next(i for i, l in enumerate(lines) if l.startswith('# '))
lines.insert(anchor + 1, '')
lines.insert(anchor + 2, f'## {today} — core {core}, LuCI {ui}')
lines.insert(anchor + 3, '')
lines.insert(anchor + 4, '- TODO: describe the release changes.')
open(path, 'w', encoding='utf-8').write('\n'.join(lines) + '\n')
PY
  echo "CHANGELOG: new section for $today added (fill it in)."
fi

echo "bumped: core=$core_v-r$core_r ui=$ui_v-r$ui_r nfqws=$nfqws"
echo 'next: fill CHANGELOG, then: git add -A && git commit && git tag v<ui-full> && git push --follow-tags'
