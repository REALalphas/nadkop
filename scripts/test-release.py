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
        for name in ('openwrt', 'scripts/get-nadkop.sh', 'scripts/install-openwrt.sh',
                     'scripts/bump-version.sh', 'CHANGELOG.md', '.gitattributes'):
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
        result, output = self.derive('# Changelog\n\n## core 0.3.0-r6, LuCI VERSION\n\n- Fixed routing.\n')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('tag=v' + self.ui, output)
        self.assertIn('exists=false', output)

    def test_placeholder_blocks_tag(self):
        result, _ = self.derive('# Changelog\n\n## LuCI VERSION\n\n- TODO: describe the release changes.\n')
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

    def test_version_prefix_is_not_a_changelog_match(self):
        result, _ = self.derive('# Changelog\n\n## LuCI VERSION0\n\n- Fixed.\n')
        self.assertNotEqual(result.returncode, 0)

    def test_source_archive_excludes_internal_files_and_keeps_svg(self):
        import tarfile
        fixtures = ('AGENTS.md', 'reports/internal.md', '.github/workflows/ci.yml',
                    'openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/flags/test.svg')
        for name in fixtures:
            path = self.work / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('fixture')
        result = self.execute('git init -q && git add . && git archive --format=tar -o source.tar "$(git write-tree)"')
        self.assertEqual(result.returncode, 0, result.stderr)
        with tarfile.open(self.work / 'source.tar') as archive:
            names = archive.getnames()
        self.assertNotIn('AGENTS.md', names)
        self.assertFalse(any(name.startswith('reports/') for name in names))
        self.assertIn('.github/workflows/ci.yml', names)
        self.assertIn(fixtures[-1], names)
        self.assertIn('openwrt/luci-app-nadkop/htdocs/luci-static/resources/nadkop/flags/LICENSE', names)

    def test_ui_only_bump_creates_changelog(self):
        result = self.execute('sh scripts/bump-version.sh --core 0.3.0-r6 --ui 0.3.0-r999')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('LuCI 0.3.0-r999', (self.work / 'CHANGELOG.md').read_text())
        self.assertEqual(self.execute('sh scripts/bump-version.sh --check').returncode, 0)

    def test_invalid_bump_preserves_files(self):
        before = {p: p.read_bytes() for p in self.work.rglob('*') if p.is_file()}
        result = self.execute('sh scripts/bump-version.sh --core 0.3.0-r7 --ui 0.3.0-r13 --nfqws 1.2.3-rbad')
        self.assertNotEqual(result.returncode, 0)
        for path, data in before.items():
            self.assertEqual(path.read_bytes(), data, str(path))

    def test_nfqws_pin_checked(self):
        getter = self.work / 'scripts/get-nadkop.sh'
        getter.write_text(getter.read_text().replace(self.version('nadkop-nfqws'), '1.2.3-r99'))
        result = self.execute('sh scripts/bump-version.sh --check')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('NFQWS', result.stderr)

    def run_installer(self, corrupt=False, suffix=False, pkgtype='apk'):
        remote = self.work / 'remote'
        remote.mkdir()
        marker = self.work / 'installed'
        installer = remote / 'install-openwrt.sh'
        installer.write_text('#!/bin/sh\nprintf installed > "' + str(marker) + '"\n')
        versions = [self.version(p) for p in ('nadkop-nfqws', 'nadkop', 'luci-app-nadkop')]
        packages = [f'{p}-{v}{"" if p == "luci-app-nadkop" else "-x86_64"}.apk'
                    if pkgtype == 'apk' else f'{p}_{v}_{"all" if p == "luci-app-nadkop" else "x86_64"}.ipk'
                    for p, v in zip(('nadkop-nfqws', 'nadkop', 'luci-app-nadkop'), versions)]
        for name in packages:
            (remote / name).write_text('fixture')
        import hashlib
        (remote / 'sha256sums').write_text(''.join(
            hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + ('.extra' if suffix and p == installer else '') + '\n'
            for p in [installer] + [remote / name for name in packages]))
        if corrupt:
            installer.write_text(installer.read_text() + '# modified after checksum\n')
        bindir = self.work / 'fakebin'
        bindir.mkdir()
        for name, source in {
                pkgtype if pkgtype == 'apk' else 'opkg': '#!/bin/sh\nexit 0\n',
                'id': '#!/bin/sh\necho 0\n',
                'uclient-fetch': '#!/bin/sh\ncp "${3#fixture://}" "$2"\n'}.items():
            path = bindir / name
            path.write_text(source)
            path.chmod(0o755)
        release_file = self.work / 'openwrt_release'
        release_file.touch()
        source = (self.work / 'scripts/get-nadkop.sh').read_text()
        source = source.replace('/etc/openwrt_release', str(release_file))
        source = source.replace('/tmp/nadkop-dl.XXXXXX', str(self.work / 'downloads.XXXXXX'))
        path = self.work / 'getter.sh'
        path.write_text(source)
        result = self.execute('sh getter.sh "fixture://$REMOTE" x86_64',
                              REMOTE=str(remote), PATH=str(bindir) + os.pathsep + os.environ['PATH'])
        return result, marker

    def test_installer_verifies_and_runs(self):
        result, marker = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker.exists())

    def test_ipk_installer_verifies_and_runs(self):
        result, marker = self.run_installer(pkgtype='ipk')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker.exists())

    def test_corrupt_installer_is_not_run(self):
        result, marker = self.run_installer(corrupt=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(marker.exists())

    def test_checksum_filename_must_match_exactly(self):
        result, marker = self.run_installer(suffix=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Checksum entry missing', result.stderr)
        self.assertFalse(marker.exists())

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
                        # Stock opkg SDKs use -N, not APK's -rN.
                        name = f'{package}_{version.replace("-r", "-")}_{abi}.ipk'
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
