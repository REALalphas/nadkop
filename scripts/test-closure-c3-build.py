#!/usr/bin/env python3
"""C3: require early, mutation-free path rejection with real SDK feed parsers.

Only parser copies in build/blocker-closure/C3 are executed. A make stub stops
at defconfig (73); this checks feed parsing, never package compilation.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ROOT / 'build/blocker-closure/C3'


def snapshot(root):
    result = {}
    for p in sorted(root.rglob('*')):
        name = str(p.relative_to(root))
        if p.is_symlink():
            result[name] = ['link', os.readlink(p)]
        elif p.is_file():
            result[name] = ['file', p.stat().st_mode, hashlib.sha256(p.read_bytes()).hexdigest()]
        else:
            result[name] = ['dir', p.stat().st_mode]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, default=ROOT / 'scripts/build-openwrt.sh')
    parser.add_argument('--sdk-root', action='append', type=Path)
    parser.add_argument('--shell', default='/bin/sh')
    args = parser.parse_args()
    sdks = args.sdk_root or [ROOT / 'build/openwrt-vm/sdk', ROOT / 'build/openwrt-vm/sdk-24.10.8']
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix='feed-regression-', dir=ARTIFACTS))
    print('fixtures:', run, flush=True)
    results = []
    chars = [('space', ' '), ('tab', '\t'), ('newline', '\n'), ('cr', '\r'),
             ('vertical-tab', '\v'), ('form-feed', '\f'), ('escape', '\x1b'),
             ('delete', '\x7f'), ('nbsp', '\u00a0'), ('format', '\u200b')]
    cases = [('plain', None, ''), ('plain-relative', None, ''),
             ('prepared', None, ''), ('output-space', 'output', ' ')]
    cases += [(f'{kind}-{name}', kind, char) for kind in ('project', 'sdk') for name, char in chars]
    cases += [('project-trailing-newline', 'project-tail', '\n'),
              ('sdk-trailing-newline', 'sdk-tail', '\n'),
              ('project-physical-space', 'project-alias', ' '),
              ('sdk-physical-space', 'sdk-alias', ' ')]
    for sdk_index, sdk_source in enumerate(sdks):
        sdk_source = sdk_source.resolve()
        parser_before = {name: hashlib.sha256((sdk_source / 'scripts' / name).read_bytes()).hexdigest()
                         for name in ('feeds', 'metadata.pm')}
        for case_index, (case, kind, char) in enumerate(cases):
            base = run / str(sdk_index) / case
            project_name = 'project'
            sdk_name = 'sdk'
            if kind and kind.startswith('project'):
                project_name += char if kind == 'project-tail' else char + 'path'
            if kind and kind.startswith('sdk'):
                sdk_name += char if kind == 'sdk-tail' else char + 'path'
            project, sdk = base / project_name, base / sdk_name
            (project / 'scripts').mkdir(parents=True)
            shutil.copyfile(args.candidate, project / 'scripts/build-openwrt.sh')
            for package in ('nadkop', 'nadkop-nfqws', 'luci-app-nadkop'):
                (project / 'openwrt' / package).mkdir(parents=True)
                (project / 'openwrt' / package / 'Makefile').write_text('# synthetic package\n')
            for name in ('include/package.mk', 'feeds/packages/lang/golang/golang-package.mk', 'feeds/luci/luci.mk'):
                p = sdk / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.touch()
            (sdk / 'scripts').mkdir()
            for name in ('feeds', 'metadata.pm'):
                shutil.copyfile(sdk_source / 'scripts' / name, sdk / 'scripts' / name)
            (sdk / 'scripts/feeds').chmod(0o755)
            (sdk / 'feeds.conf.default').write_text('src-git packages https://example.invalid/packages\n'
                                                  'src-git luci https://example.invalid/luci\n')
            # Keep pre-existing SDK state too, so rejection cannot rewrite it.
            (sdk / 'feeds.conf').write_text((sdk / 'feeds.conf.default').read_text())
            (sdk / '.config').write_text('# synthetic pre-existing config\n')
            stubdir = base / 'stubbin'
            stubdir.mkdir()
            stub = stubdir / 'gmake'
            stub.write_text('#!/bin/sh\n'
                            'if [ "$1" = -v ]; then echo "GNU Make 4.4"; exit 0; fi\n'
                            'if [ "$1" = defconfig ]; then echo "C3_STUB_MAKE_STOP" >&2; exit 73; fi\n'
                            'exit 0\n')
            stub.chmod(0o755)
            (stubdir / 'make').symlink_to('gmake')
            project_arg, sdk_arg = project, sdk
            if kind == 'project-alias':
                project_arg = base / 'project-alias'
                project_arg.symlink_to(project.name)
            if kind == 'sdk-alias':
                sdk_arg = base / 'sdk-alias'
                sdk_arg.symlink_to(sdk.name)
            out = base / ('output directory' if kind == 'output' else 'output')
            rejected = kind not in (None, 'output')
            output_existed = rejected and case_index % 2 == 0
            if output_existed:
                out.mkdir()
                (out / 'sentinel').write_text('preserve existing output\n')
            output_before = snapshot(out) if output_existed else None
            if rejected and case_index % 3 == 0:
                (sdk / 'feeds.conf').unlink()
                (sdk / '.config').unlink()
            before = snapshot(sdk)
            env = os.environ.copy()
            env['PATH'] = str(stubdir) + os.pathsep + env['PATH']
            env['TOPDIR'] = str(sdk)
            env.pop('CDPATH', None)
            command = [args.shell, str(project_arg / 'scripts/build-openwrt.sh'), str(sdk_arg), str(out)]
            if case == 'plain-relative':
                command = [args.shell, 'project/scripts/build-openwrt.sh', 'sdk', 'output']
            if case == 'prepared':
                command.append('--prepared')
            result = subprocess.run(command, cwd=base, env=env,
                                    text=True, capture_output=True, timeout=20)
            (base / 'stdout.log').write_text(result.stdout)
            (base / 'stderr.log').write_text(result.stderr)
            link = sdk / 'feeds/nadkop'
            unchanged = snapshot(sdk) == before
            output_unchanged = ((out.exists() and snapshot(out) == output_before)
                                if output_existed else not out.exists())
            if rejected:
                ok = (result.returncode == 1 and 'Unsupported ' in result.stderr
                      and 'whitespace and control characters' in result.stderr
                      and unchanged and output_unchanged)
            else:
                ok = (result.returncode == 73 and 'C3_STUB_MAKE_STOP' in result.stderr
                      and link.is_symlink() and link.resolve() == project / 'openwrt'
                      and all((link / p / 'Makefile').is_file()
                              for p in ('nadkop', 'nadkop-nfqws', 'luci-app-nadkop')))
            record = dict(sdk=str(sdk_source), case=case, exit=result.returncode,
                          rejected=rejected, sdk_unchanged=unchanged,
                          output_existed=output_existed, output_unchanged=output_unchanged,
                          output_exists=out.exists(), passed=ok)
            results.append(record)
            print(json.dumps(record), flush=True)
        parser_after = {name: hashlib.sha256((sdk_source / 'scripts' / name).read_bytes()).hexdigest()
                        for name in ('feeds', 'metadata.pm')}
        if parser_before != parser_after:
            raise AssertionError('source SDK parser changed')
        (run / f'sdk-{sdk_index}-parsers.json').write_text(json.dumps(parser_before, indent=2) + '\n')
    (run / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
    failed = sum(not r['passed'] for r in results)
    print(f'RESULT: {len(results) - failed}/{len(results)} passed; no package build performed')
    return bool(failed)


if __name__ == '__main__':
    raise SystemExit(main())
