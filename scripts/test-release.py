#!/usr/bin/env python3
"""Exercise workflow shell steps locally without publishing or using secrets."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


def run_block(workflow, name):
    lines = (ROOT / '.github/workflows' / workflow).read_text().splitlines()
    start = lines.index('      - name: ' + name)
    start = next(i for i in range(start + 1, len(lines))
                 if lines[i] == '        run: |') + 1
    body = []
    for line in lines[start:]:
        if line and not line.startswith('          '):
            break
        body.append(line[10:])
    return '\n'.join(body) + '\n'


class ReleaseWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='nadkop-release-')
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        for name in ('openwrt', 'scripts/get-nadkop.sh', 'scripts/install-openwrt.sh'):
            src, dst = ROOT / name, self.work / name
            if src.is_dir():
                shutil.copytree(src, dst)
            else:
                dst.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(src, dst)

    def execute(self, script, **env):
        return subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', script],
                              cwd=self.work, env=dict(os.environ, **env),
                              capture_output=True, text=True, timeout=30)

    def derive(self, changelog, existing=False):
        self.execute('git init -q && git config user.name Test && '
                     'git config user.email test@example.invalid && '
                     'git -c commit.gpgsign=false commit --allow-empty -qm test')
        self.ui = self.version('luci-app-nadkop')
        (self.work / 'CHANGELOG.md').write_text(changelog.replace('VERSION', self.ui))
        if existing:
            result = self.execute('git tag "v' + self.ui + '"')
            self.assertEqual(result.returncode, 0, result.stderr)
        result = self.execute(run_block('auto-release.yml', 'Derive tag, check changelog'),
                              GITHUB_OUTPUT=str(self.work / 'outputs'))
        return result, (self.work / 'outputs').read_text()

    def version(self, package):
        recipe = (self.work / 'openwrt' / package / 'Makefile').read_text()
        version = re.search(r'^PKG_VERSION:=(\S+)', recipe, re.M)[1]
        release = re.search(r'^PKG_RELEASE:=(\S+)', recipe, re.M)[1]
        return version + '-r' + release

    def test_filled_changelog(self):
        result, output = self.derive('# Changelog\n\n## VERSION\n\n- Fixed routing.\n')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('tag=v' + self.ui, output)
        self.assertIn('exists=false', output)

    def test_placeholder_blocks_tag(self):
        result, _ = self.derive('# Changelog\n\n## VERSION\n\n- TODO: describe the release changes.\n')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('TODO placeholder', result.stderr)

    def test_missing_changelog_blocks_tag(self):
        result, _ = self.derive('# Changelog\n\n## Unrelated release\n')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('no section', result.stderr)

    def test_existing_tag_skips_release(self):
        result, output = self.derive('# Changelog\n', existing=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('exists=true', output)

    def test_missing_pat_blocks_push(self):
        result = self.execute(run_block('auto-release.yml', 'Create and push tag'),
                              RELEASE_PAT='', TAG='vtest')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('RELEASE_PAT secret is not set', result.stdout)

    def test_release_layout_and_installer(self):
        ver = self.version('luci-app-nadkop')
        base = 'https://example.invalid/releases/download/v' + ver
        packages = ('nadkop-nfqws', 'nadkop', 'luci-app-nadkop')
        host = self.work / 'artifacts' / ('host-' + ver)
        host.mkdir(parents=True)
        for arch in ('x86_64', 'aarch64'):
            (host / ('nadkop-linux-' + arch)).write_text('host fixture ' + arch)
        expected = set()
        for pkgtype in ('apk', 'ipk'):
            for arch in ('x86_64', 'aarch64'):
                dest = self.work / 'artifacts' / f'owrt-{pkgtype}-{arch}-{ver}'
                dest.mkdir()
                for package in packages:
                    version = self.version(package)
                    if pkgtype == 'apk':
                        name = f'{package}-{version}.apk'
                        asset = name if package == 'luci-app-nadkop' else f'{package}-{version}-{arch}.apk'
                    else:
                        abi = 'all' if package == 'luci-app-nadkop' else ('aarch64_generic' if arch == 'aarch64' else arch)
                        name = f'{package}_{version}_{abi}.ipk'
                        asset = f'{package}_{version}_{"all" if package == "luci-app-nadkop" else arch}.ipk'
                    (dest / name).write_text('package fixture ' + name)
                    expected.add(asset)
        result = self.execute(run_block('release.yml', 'Assemble release layout'),
                              VER=ver, BASE_URL=base)
        self.assertEqual(result.returncode, 0, result.stderr)
        release = self.work / 'release'
        self.assertTrue(expected.issubset({p.name for p in release.iterdir()}),
                        expected - {p.name for p in release.iterdir()})
        installer = (release / 'get-nadkop.sh').read_text()
        for key, package in [('CORE', 'nadkop'), ('UI', 'luci-app-nadkop'), ('NFQWS', 'nadkop-nfqws')]:
            self.assertIn(f'{key}="${{NADKOP_{key}:-{self.version(package)}}}"', installer)
        self.assertIn('BASE_URL="${1:-' + base + '}"', installer)
        result = self.execute('cd release && sha256sum -c sha256sums && sh -n get-nadkop.sh')
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
