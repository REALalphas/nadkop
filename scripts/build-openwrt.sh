#!/bin/sh
# Use an unpacked OpenWrt SDK matching the router's release and target.
set -eu
usage() {
	echo "Usage: $0 SDK_DIRECTORY [OUTPUT_DIRECTORY] [--prepared]" >&2
	echo 'Install the pinned packages and luci feeds first. --prepared reuses built dependencies.' >&2
	exit 1
}
[ "$#" -ge 1 ] && [ "$#" -le 3 ] || usage
project_input="${NADKOP_SOURCE_ROOT:-$(dirname -- "$0")/..}"
# OpenWrt feed parsing and package recipes do not support these paths. Check
# both supplied and physical paths before creating output or changing the SDK.
python3 - "$project_input" "$1" <<'PY'
from pathlib import Path
import sys, unicodedata
paths = [('project', Path(sys.argv[1]).absolute()),
         ('SDK', Path(sys.argv[2]).absolute())]
for label, path in paths:
    for candidate in (path, path.resolve()):
        if any(c.isspace() or unicodedata.category(c) in ('Cc', 'Cf', 'Cs')
               for c in str(candidate)):
            sys.exit(f'Unsupported {label} path: whitespace and control characters are not supported by the OpenWrt build. Use a plain path.')
PY
project="$(CDPATH='' cd -- "$project_input" && pwd -P)"
sdk="$(CDPATH='' cd -- "$1" && pwd -P)"
shift
out="$project/build/packages"
if [ "$#" -gt 0 ] && [ "$1" != --prepared ]; then out="$1"; shift; fi
prepared=0
if [ "$#" -gt 0 ]; then [ "$1" = --prepared ] || usage; prepared=1; shift; fi
[ "$#" -eq 0 ] || usage
mkdir -p "$out"
out="$(CDPATH='' cd -- "$out" && pwd)"
for required in include/package.mk feeds/packages/lang/golang/golang-package.mk feeds/luci/luci.mk; do
	[ -f "$sdk/$required" ] || { echo "Missing SDK file: $required (prepare feeds first)" >&2; exit 1; }
done
cd "$sdk"
[ -f feeds.conf ] || cp feeds.conf.default feeds.conf
# Replace only this project's feed; preserve the SDK's pinned upstream feeds.
python3 - "$project/openwrt" <<'PY'
import pathlib, sys
p = pathlib.Path('feeds.conf')
lines = [line for line in p.read_text().splitlines()
         if not (len(line.split()) >= 2 and line.split()[1] == 'nadkop')]
p.write_text('\n'.join(lines + ['src-link nadkop ' + sys.argv[1]]) + '\n')
PY
./scripts/feeds update nadkop
./scripts/feeds install -p nadkop nadkop nadkop-nfqws luci-app-nadkop
if [ ! -f .config ]; then
	# SDK defaults may otherwise select every package and kernel module.
	printf '%s\n' '# CONFIG_ALL is not set' '# CONFIG_ALL_NONSHARED is not set' '# CONFIG_ALL_KMODS is not set' > .config
fi
python3 - <<'PY'
from pathlib import Path
p = Path('.config')
keys = ['CONFIG_PACKAGE_nadkop', 'CONFIG_PACKAGE_nadkop-nfqws', 'CONFIG_PACKAGE_luci-app-nadkop']
lines = [line for line in p.read_text().splitlines()
         if not any(line.startswith(k + '=') or line == '# ' + k + ' is not set' for k in keys)]
p.write_text('\n'.join(lines + [k + '=m' for k in keys]) + '\n')
PY
make defconfig
# A cached SDK contains old project build stamps. Always re-read this checkout's
# sources, even when a version was not bumped. Leave upstream dependencies built.
if [ "${NADKOP_CLEAN_PACKAGES:-0}" = 1 ]; then
	for package in nadkop-nfqws nadkop luci-app-nadkop; do
		make "package/$package/clean" V=s
	done
fi
if [ "$prepared" = 1 ]; then
	# Rebuild package contents without revisiting already built dependencies.
	for package in nadkop-nfqws nadkop luci-app-nadkop; do
		make -C "package/feeds/nadkop/$package" TOPDIR="$sdk" \
			HOST_OS=Linux HOST_ARCH="$(uname -m)" \
			BUILD_SUBDIR="package/feeds/nadkop/$package" compile V=s
	done
else
	for package in nadkop-nfqws nadkop luci-app-nadkop; do
		make -j"${JOBS:-2}" "package/$package/compile" V=s
	done
fi
# Copy only the packages from this build, with checksums and SDK metadata.
python3 - "$out" <<'PY'
from pathlib import Path
import hashlib, re, shutil, sys
out = Path(sys.argv[1])
selected = []
for name in ['nadkop', 'nadkop-nfqws', 'luci-app-nadkop']:
    recipe = Path('feeds/nadkop', name, 'Makefile').read_text()
    version = re.search(r'^PKG_VERSION:=(.+)$', recipe, re.M)[1].strip()
    release = re.search(r'^PKG_RELEASE:=(.+)$', recipe, re.M)[1].strip()
    files = list(Path('bin/packages').glob(f'*/nadkop/{name}-{version}-r{release}.apk'))
    for ipk_release in (release, 'r' + release):
        files += list(Path('bin/packages').glob(f'*/nadkop/{name}_{version}-{ipk_release}_*.ipk'))
    if len(files) != 1:
        sys.exit(f'Expected one {name} {version}-{release} package, found {len(files)}.')
    selected.append(files[0])
for p in selected:
    shutil.copy2(p, out / p.name)
(out / 'sha256sums').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in selected))
config = Path('.config').read_text().splitlines()
provenance = [line for line in config if line.startswith(('CONFIG_TARGET_', 'CONFIG_VERSION_', 'CONFIG_USE_APK='))]
import subprocess
for label, args in (('GO_VERSION', ['go', 'version']), ('FEEDS_NADKOP', ['git', 'rev-parse', '--short', 'HEAD'])):
    try:
        provenance.append(f'{label}=' + subprocess.run(args, capture_output=True, text=True, timeout=30, cwd=(Path("feeds/nadkop") if label.startswith("FEEDS") else None)).stdout.strip())
    except Exception as exc:
        provenance.append(f'{label}=unknown ({exc})')
xray = list(Path('staging_dir').glob('*/bin/xray'))
if len(xray) == 1:
    try:
        provenance.append('XRAY_VERSION=' + subprocess.run([str(xray[0]), 'version'], capture_output=True, text=True, timeout=30).stdout.splitlines()[0].strip())
    except Exception as exc:
        provenance.append(f'XRAY_VERSION=unknown ({exc})')
(out / 'build-info.txt').write_text('\n'.join(provenance) + '\n')
print('Packages: ' + str(out))
PY
