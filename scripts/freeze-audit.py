#!/usr/bin/env python3
"""Generate a draft SHA256 inventory; never claim suite acceptance."""
import datetime
import hashlib
import json
from audit_common import ROOT, REPORT, MANIFEST

BASELINE = ROOT / 'reports/runs/resume-0.3.0-r4-ui-r8/frozen-source.json'
baseline = json.loads(BASELINE.read_text())
paths = {'.gitignore', 'go.mod'}
for directory in ('cmd', 'internal', 'examples', 'openwrt'):
    paths.update(str(p.relative_to(ROOT)) for p in (ROOT / directory).rglob('*')
                 if p.is_file() and not p.name.endswith('_test.go'))
paths.update(p for p in baseline['files'] if p.startswith('scripts/'))
files = {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest() for p in sorted(paths)}
manifest = dict(frozenAt=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                root=str(ROOT), status='draft', fullSuitesRun=False,
                scope=baseline['scope'], algorithm='sha256 (raw file bytes)', files=files)
REPORT.mkdir(parents=True, exist_ok=True)
(REPORT / 'logs').mkdir(exist_ok=True)
MANIFEST.write_text(json.dumps(manifest, indent=2) + '\n')

# Attribution describes areas, not an assertion that every later edit was made by C1–C4.
def attribution(p):
    if p == 'internal/core/geodata.go' or p == 'scripts/build-openwrt.sh':
        return ['C3'], ['reports/blocker-closure/C3-boundaries-build.md']
    if p in ('internal/router/network.go', 'openwrt/nadkop/files/nadkop.init'):
        return ['C2'], ['reports/blocker-closure/C2-network.md']
    if p.startswith('openwrt/luci-app-nadkop/'):
        return ['C4', 'LuCI r9–r11'], ['reports/blocker-closure/C4-luci.md', 'reports/ui-backlog-2026-11-03.md', 'reports/zapret-tuner-2026-11-03.md']
    if p in ('internal/core/monitor.go', 'internal/router/probe.go', 'internal/router/runtime_diagnostics.go', 'internal/router/updater.go'):
        return ['backend r5–r6 (post-closure)'], ['reports/improvements-X/final.md', 'reports/zapret-tuner-2026-11-03.md']
    return ['C1', 'backend r5–r6'], ['reports/blocker-closure/C1-state.md', 'reports/blocker-closure/acceptance.md', 'reports/zapret-tuner-2026-11-03.md']

changes = []
for p in sorted(set(baseline['files']) | set(files)):
    old, new = baseline['files'].get(p), files.get(p)
    if old == new:
        continue
    areas, evidence = attribution(p)
    changes.append(dict(path=p, beforeSha256=old, afterSha256=new,
                        change='added' if old is None else 'removed' if new is None else 'modified',
                        areas=areas, evidence=evidence,
                        attribution='area-level; exact revision provenance unavailable without Git'))
modified = sum(c['change'] == 'modified' for c in changes)
(REPORT / 'changes-since-resume.json').write_text(json.dumps(dict(
    status='draft', baseline=str(BASELINE.relative_to(ROOT)),
    baselineManifestSha256=hashlib.sha256(BASELINE.read_bytes()).hexdigest(),
    currentManifestSha256=hashlib.sha256(MANIFEST.read_bytes()).hexdigest(),
    requestedModifiedCount=24, observedModifiedCount=modified,
    countNote='Current raw-byte comparison is authoritative; do not force the requested count of 24.',
    versions={'baseline': 'backend r4 / LuCI r8', 'current': 'backend r6 / LuCI r11',
              'intermediateEvidence': 'reports/ui-backlog-2026-11-03.md: backend r5 / LuCI r9; reports/zapret-tuner-2026-11-03.md: backend r6 / LuCI r10'},
    changes=changes), ensure_ascii=False, indent=2) + '\n')
print(f'DRAFT: {len(files)} files, {modified} modified, {len(changes)} total changes: {REPORT}')
