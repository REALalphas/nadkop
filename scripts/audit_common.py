"""Shared audit source selection and prerequisite diagnostics (no test policy)."""
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CONFIG = json.loads((ROOT / 'scripts/audit-config.json').read_text())
REPORT = ROOT / os.environ.get('NADKOP_AUDIT_REPORT', CONFIG['report'])
MANIFEST = REPORT / CONFIG['manifest']


def load_manifest():
    try:
        manifest = json.loads(MANIFEST.read_text())
        if not isinstance(manifest['files'], dict) or not manifest['files']:
            raise ValueError('files must be a non-empty object')
        return manifest
    except (OSError, ValueError, KeyError) as error:
        raise SystemExit(f'ERROR: cannot load audit manifest {MANIFEST}: {error}') from None


def require_geo(directory):
    missing = [str(Path(directory) / name) for name in ('geosite.dat', 'geoip.dat')
               if not (Path(directory) / name).is_file()]
    if missing:
        raise SystemExit('ERROR: NADKOP_GEO_TEST_FILES requires geosite.dat and geoip.dat; missing: '
                         + ', '.join(missing) + '. Set it to a prepared local dataset directory.')


def require_ui(env, emoji=False):
    module = env.setdefault('NADKOP_PLAYWRIGHT_MODULE', CONFIG['playwright'])
    if not Path(module).is_file():
        raise SystemExit(f'ERROR: Playwright module missing: {module}. Prepare /path/to/playwright-env '
                         'or set NADKOP_PLAYWRIGHT_MODULE to its index.mjs file.')
    if emoji:
        directory = Path(env['NADKOP_EMOJI_TEST_CACHE'])
        if not directory.is_dir() or not any(directory.glob('*.png')):
            raise SystemExit(f'ERROR: NADKOP_EMOJI_TEST_CACHE has no PNG fixtures: {directory}. '
                             'Set it to a populated local emoji cache.')
