#!/usr/bin/env python3
"""Bounded syscall failures in a private fixture; no host service/network calls."""
from pathlib import Path
import hashlib
import json
import os
import subprocess

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'build/improvements-X/x2-implementation/save-faults'
OUT.mkdir(parents=True, exist_ok=True)
BINARY = OUT / 'router.test'
subprocess.run(['go', 'test', '-c', '-o', str(BINARY), './internal/router'], cwd=ROOT, check=True)
cases = [('baseline', None, False)]
for n in range(1, 4):
    cases.append((f'kill-before-rename-{n}', f'renameat:signal=SIGKILL:when={n}', n == 3))
for n in range(1, 7):
    cases.append((f'fsync-eio-{n}', f'fsync:error=EIO:when={n}', n == 4))
cases += [('kill-after-config-rename', 'fsync:signal=SIGKILL:when=6', False),
          ('config-rename-eio', 'renameat:error=EIO:when=3', False)]
results = []
for name, injection, blocked in cases:
    root = OUT / name
    root.mkdir(exist_ok=True)
    # Only this runner's previous fixture files may be reset.
    for p in root.iterdir():
        if p.is_file() or p.is_symlink():
            p.unlink()
    (root / 'run').mkdir(exist_ok=True)
    (root / 'run/current').write_text('runtime-sentinel')
    nodes = [{'tag': 'old', 'protocol': 'socks', 'settings': {'servers': [{'address': '127.0.0.1', 'port': 12345}]}}]
    config = {'sources': [{'name': 'local', 'file': str(root / 'nodes.json')}],
              'rules': [{'action': 'proxy', 'domains': ['test.invalid'], 'node': 'local/old'}]}
    for file, value in [('config.json', config), ('nodes.json', nodes)]:
        (root / file).write_text(json.dumps(value) + '\n')
    env = {**os.environ, 'X2_SAVE_FIXTURE': str(root), 'X2_SAVE_MODE': 'save'}
    trace = root / 'trace.log'
    command = ['strace', '-f', '-qq', '-o', str(trace), '-e', 'trace=fsync,renameat,rename,renameat2']
    if injection:
        command += ['-e', 'inject=' + injection]
    command += [str(BINARY), '-test.run=^TestX2SaveFaultChild$', '-test.v']
    action = subprocess.run(command, env=env, capture_output=True, text=True, timeout=20)
    (root / 'action.log').write_text(action.stdout + action.stderr)
    inspect = subprocess.run([str(BINARY), '-test.run=^TestX2SaveFaultChild$', '-test.v'],
                             env={**env, 'X2_SAVE_MODE': 'inspect'}, capture_output=True, text=True, timeout=5)
    actual = 'X2_INSPECT blocked=true' in inspect.stdout
    injected = injection is None or '(INJECTED)' in trace.read_text() or 'SIGKILL' in trace.read_text()
    new_c = json.loads((root / 'config.json').read_text())
    new_n = json.loads((root / 'nodes.json').read_text())
    coherent = new_c['rules'][0]['node'] == 'local/' + new_n[0]['tag']
    record = {'name': name, 'injection': injection, 'exit': action.returncode, 'blocked': actual,
              'expectedBlocked': blocked, 'coherent': coherent, 'injectionReached': injected,
              'runtimeIntact': (root / 'run/current').read_text() == 'runtime-sentinel', 'inspection': inspect.stdout}
    record['pass'] = actual == blocked and (coherent or actual) and injected and record['runtimeIntact']
    results.append(record)
    print(('PASS ' if record['pass'] else 'FAIL ') + name)
(OUT / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
raise SystemExit(0 if all(r['pass'] for r in results) else 1)
