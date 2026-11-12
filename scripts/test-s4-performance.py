#!/usr/bin/env python3
"""S4 sequential, local-only performance runner. Standard library only.

Compiles production core files plus the opt-in performance test. Every scenario runs in a fresh process. Never talks to VMs,
changes system settings, invokes SDKs or uses actual subscriptions/credentials.
"""
from __future__ import annotations
import argparse
import csv
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
def require_geo(directory):
    missing = [name for name in ('geosite.dat', 'geoip.dat')
               if not (Path(directory) / name).is_file()]
    if missing:
        raise SystemExit('Missing geodata fixtures: ' + ', '.join(missing))


def source_hashes():
    files = list((ROOT / 'internal/core').glob('*.go')) + [Path(__file__).resolve()]
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in files}


SOURCE_HASHES = source_hashes()
BASE = ROOT / 'build/agent-audit/S4'
DEFAULT_CASES = [
    'scale-16', 'scale-256', 'scale-1024', 'scale-2048', 'scale-4096',
    'profiles-4096', 'links-4096', 'base64-4096', 'maximum',
    'https-byte-budget', 'boundaries', 'catalogue-limit', 'catalogue-byte-budget', 'rules-dense', 'geo-entries', 'geo-bytes',
    'geo-real', 'geo-install', 'monitor-loopback',
    'geo-fanout-1', 'geo-fanout-4', 'geo-fanout-16', 'geo-fanout-64',
    'geo-fanout-256', 'geo-fanout-1024',
]

def atomic_json(path: Path, value):
    tmp = path.with_suffix(path.suffix + '.s4-tmp')
    tmp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    tmp.replace(path)


def verify(path: Path):
    manifest = {'files': SOURCE_HASHES}
    entries = []
    for name, expected in manifest['files'].items():
        p = ROOT / name
        actual = hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None
        entries.append(dict(path=name, expected=expected, actual=actual, match=actual == expected))
    result = dict(at=dt.datetime.now(dt.timezone.utc).isoformat(),
                  manifest_sha256=hashlib.sha256(json.dumps(SOURCE_HASHES, sort_keys=True).encode()).hexdigest(),
                  files=len(entries), matching=sum(x['match'] for x in entries),
                  mismatches=[x for x in entries if not x['match']], entries=entries)
    atomic_json(path, result)
    return not result['mismatches']


def proc_info(pid):
    try:
        fields = {}
        for line in Path(f'/proc/{pid}/status').read_text().splitlines():
            key, _, val = line.partition(':')
            if key in ('VmRSS', 'VmHWM', 'PPid'):
                fields[key] = int(val.split()[0])
        return fields
    except (OSError, ValueError):
        return {}


def descendants(pid):
    pending = [pid]
    result = []
    while pending:
        current = pending.pop()
        if current in result:
            continue
        result.append(current)
        try:
            pending.extend(int(x) for x in Path(f'/proc/{current}/task/{current}/children').read_text().split())
        except OSError:
            pass
    return result


def measure_process(command, env, logpath, cpus, timeout, stop_kib):
    def child_limits():
        os.sched_setaffinity(0, cpus)
        resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
        resource.setrlimit(resource.RLIMIT_CPU, (max(60, int(timeout)), max(61, int(timeout) + 1)))
        resource.setrlimit(resource.RLIMIT_FSIZE, (256 << 20, 256 << 20))
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    start = time.monotonic_ns()
    start_epoch = time.time_ns()
    samples = []
    peaks = {}
    reason = None
    with logpath.open('wb') as log:
        process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT,
                                   start_new_session=True, preexec_fn=child_limits)
        child_usage = None
        while True:
            waited, status, usage = os.wait4(process.pid, os.WNOHANG)
            if waited:
                process.returncode = os.waitstatus_to_exitcode(status)
                child_usage = usage
                break
            now = time.monotonic_ns()
            root_rss = 0
            total_rss = 0
            for pid in descendants(process.pid):
                info = proc_info(pid)
                rss = info.get('VmRSS', 0)
                peak = max(rss, info.get('VmHWM', 0))
                peaks[pid] = max(peaks.get(pid, 0), peak)
                total_rss += rss
                if pid == process.pid:
                    root_rss = rss
                if peak >= stop_kib and reason is None:
                    reason = f'rss_limit: owned pid {pid} reached {peak} KiB >= {stop_kib} KiB'
            samples.append(dict(unix_ns=time.time_ns(), wall_ns=now-start,
                                root_rss_kib=root_rss, owned_tree_rss_kib=total_rss))
            if now-start > timeout * 1_000_000_000 and reason is None:
                reason = f'time_limit: {timeout}s'
            if reason is not None:
                # This group was created only for this owned child command.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                break
            time.sleep(0.01)
        if process.returncode is None:
            _, status, child_usage = os.wait4(process.pid, 0)
            process.returncode = os.waitstatus_to_exitcode(status)
        exitcode = process.returncode
    end = time.monotonic_ns()
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    samplepath = logpath.with_suffix('.samples.json')
    atomic_json(samplepath, samples)
    result = dict(command=command, cpus=cpus, exitcode=exitcode,
                  stop_reason=reason, start_unix_ns=start_epoch,
                  wall_ns=end-start, cpu_user_ns=round((after.ru_utime-before.ru_utime)*1e9),
                  cpu_system_ns=round((after.ru_stime-before.ru_stime)*1e9),
                  per_pid_peak_rss_kib={str(k): v for k, v in peaks.items()},
                  sampled_peak_rss_kib=max(peaks.values(), default=0),
                  kernel_peak_rss_kib=child_usage.ru_maxrss if child_usage else 0,
                  log=str(logpath.relative_to(ROOT)), samples=str(samplepath.relative_to(ROOT)))
    metrics = []
    markers = []
    for line in logpath.read_text(errors='replace').splitlines():
        if line.startswith('S4_METRIC '):
            metric = json.loads(line.removeprefix('S4_METRIC '))
            relevant = [x['root_rss_kib'] for x in samples
                        if metric['start_unix_ns'] <= x['unix_ns'] <= metric['end_unix_ns']]
            metric['sampled_phase_peak_rss_kib'] = max(relevant, default=0)
            metrics.append(metric)
        elif line.startswith('S4_BEGIN '):
            markers.append(json.loads(line.removeprefix('S4_BEGIN ')))
    result['metrics'] = metrics
    result['last_started'] = markers[-1] if markers else None
    return result


def write_tables(out, results):
    metrics = []
    for result in results:
        for m in result.get('metrics', []):
            metrics.append(dict(m, process_exitcode=result['exitcode'],
                                process_peak_rss_kib=max(result.get('sampled_peak_rss_kib', 0), result.get('kernel_peak_rss_kib', 0))))
    atomic_json(out / 'metrics.json', metrics)
    fields = ['case', 'operation', 'iterations', 'wall_ms_per_op', 'cpu_ms_per_op',
              'alloc_bytes_per_op', 'allocs_per_op', 'write_syscalls_per_op',
              'write_chars_per_op', 'kernel_write_bytes_per_op',
              'process_high_water_rss_kib', 'sampled_phase_peak_rss_kib',
              'process_peak_rss_kib', 'gc_cycles', 'dimensions', 'error']
    tmp = out / 'metrics.csv.s4-tmp'
    with tmp.open('w', newline='') as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for m in metrics:
            n = max(1, m['iterations'])
            w.writerow(dict(case=m['case'], operation=m['operation'], iterations=n,
                            wall_ms_per_op=m['wall_ns']/n/1e6,
                            cpu_ms_per_op=(m['cpu_user_ns']+m['cpu_system_ns'])/n/1e6,
                            alloc_bytes_per_op=m['alloc_bytes']/n, allocs_per_op=m['allocs']/n,
                            write_syscalls_per_op=m['write_syscalls']/n,
                            write_chars_per_op=m['write_chars']/n,
                            kernel_write_bytes_per_op=m['kernel_write_bytes']/n,
                            process_high_water_rss_kib=m['process_high_water_rss_kib'],
                            sampled_phase_peak_rss_kib=m.get('sampled_phase_peak_rss_kib', 0),
                            process_peak_rss_kib=m['process_peak_rss_kib'], gc_cycles=m['gc_cycles'],
                            dimensions=json.dumps(m['dimensions'], ensure_ascii=False, sort_keys=True),
                            error=m.get('error', '')))
    tmp.replace(out / 'metrics.csv')


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--cases', default=','.join(DEFAULT_CASES))
    ap.add_argument('--output', type=Path)
    ap.add_argument('--reuse-binary', action='store_true')
    ap.add_argument('--strace', action='store_true', help='trace file/eventfd writes; timing includes tracer overhead')
    ap.add_argument('--strace-fs-only', action='store_true', help='trace chmod/sync/rename without network payload writes')
    ap.add_argument('--timeout', type=float, default=120)
    ap.add_argument('--stop-rss-mib', type=int, default=736)
    args = ap.parse_args()
    if not 256 <= args.stop_rss_mib <= 736:
        ap.error('--stop-rss-mib must be 256..736 (margin below 768 MiB)')
    stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    out = args.output or BASE / ('run-' + stamp)
    if not out.is_absolute():
        out = ROOT / out
    out.mkdir(parents=True, exist_ok=False)
    temp = out / 'tmp'
    temp.mkdir()
    cache = BASE / 'go-cache'
    cache.mkdir(exist_ok=True)
    env = os.environ.copy()
    env.update(GOMAXPROCS='2', GOMEMLIMIT='576MiB', CGO_ENABLED='0',
               GOFLAGS='', GOTOOLCHAIN='local', GOWORK='off',
               GOCACHE=str(cache), TMPDIR=str(temp),
               S4_GEO_DIR=os.environ.get('NADKOP_GEO_TEST_FILES', str(ROOT / 'build/geodata')))
    cpus = sorted(os.sched_getaffinity(0))[-2:]
    cases = [x.strip() for x in args.cases.split(',') if x.strip()]
    if any(case in ('geo-real', 'geo-install') for case in cases):
        require_geo(env['S4_GEO_DIR'])
    metadata = dict(at=dt.datetime.now(dt.timezone.utc).isoformat(), platform=platform.platform(),
                    machine=platform.machine(), cpu_model=next((x.split(':',1)[1].strip()
                    for x in Path('/proc/cpuinfo').read_text().splitlines() if x.startswith('model name')), ''),
                    cpus=cpus, GOMAXPROCS=2, GOMEMLIMIT='576MiB',
                    rss_stop_mib=args.stop_rss_mib, sample_interval_ms=10,
                    cases=cases, timeout_s=args.timeout,
                    argv=sys.argv, strace=args.strace, strace_fs_only=args.strace_fs_only, build_scope='frozen internal/core production files plus S4 test only')
    metadata['go_version'] = subprocess.check_output(['go','version'],env=env,text=True).strip()
    atomic_json(out / 'environment.json', metadata)
    (out/'s4-runner-source.txt').write_bytes(Path(__file__).read_bytes())
    (out/'s4-test-source.txt').write_bytes((ROOT/'internal/core/s4_performance_test.go').read_bytes())
    if not verify(out / 'frozen-before.json'):
        print('Protected source mismatch before S4; stopping.', flush=True)
        return 2
    source = {'files': SOURCE_HASHES}
    gofiles = sorted(p for p in source['files'] if p.startswith('internal/core/') and p.endswith('.go') and not p.endswith('_test.go'))
    gofiles.append('internal/core/s4_performance_test.go')
    fingerprint = hashlib.sha256(b''.join(p.encode()+b'\0'+(ROOT/p).read_bytes() for p in gofiles)).hexdigest()
    binary = BASE / 's4-core.test'
    fingerprint_file = BASE / 'binary-fingerprint.json'
    reusable = (binary.exists() and fingerprint_file.exists() and
                json.loads(fingerprint_file.read_text()).get('sources_sha256') == fingerprint)
    stop_kib = args.stop_rss_mib * 1024
    results = []
    try:
        if not (args.reuse_binary and reusable):
            cmd = ['go', 'test', '-c', '-p=2', '-vet=off', '-o', str(binary), *gofiles]
            print('S4 building isolated core test binary, max 2 CPU.', flush=True)
            result = measure_process(cmd, env, out/'build.log', cpus, args.timeout, stop_kib)
            atomic_json(out/'build.json', result)
            if result['exitcode'] != 0:
                print('S4 build failed; see', result['log'], flush=True)
                return 1
            atomic_json(fingerprint_file, dict(sources_sha256=fingerprint,
                        binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), gofiles=gofiles))
        else:
            atomic_json(out/'build.json', dict(reused=True, sources_sha256=fingerprint,
                                             binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest()))
        fanout_stopped = False
        for case in cases:
            if '/' in case or not all(x.isalnum() or x=='-' for x in case):
                raise ValueError('invalid case name')
            if case.startswith('geo-fanout-') and fanout_stopped:
                results.append(dict(case=case, exitcode=None, skipped='lower geo fanout hit resource guard', metrics=[]))
                atomic_json(out/'results.json', results)
                continue
            case_env = dict(env, S4_CASE=case)
            cmd = [str(binary), '-test.run=^TestS4Performance$', '-test.count=1',
                   '-test.parallel=1', f'-test.timeout={max(30,int(args.timeout))}s', '-test.v']
            if args.strace or args.strace_fs_only:
                trace = 'fchmod,chmod,fchmodat,fsync,fdatasync,rename,renameat,renameat2'
                if not args.strace_fs_only:
                    trace = 'write,writev,pwrite64,' + trace
                cmd = ['strace','-f','-qq','-ttt','-yy','-s','256','-e',
                       'trace='+trace,'-o',str(out/(case+'.strace.log')),*cmd]
            print('S4 running', case, flush=True)
            result = measure_process(cmd, case_env, out/(case+'.log'), cpus, args.timeout, stop_kib)
            result['case'] = case
            results.append(result)
            atomic_json(out/'results.json', results)
            write_tables(out, results)
            print(json.dumps({k:result.get(k) for k in ('case','exitcode','wall_ns','sampled_peak_rss_kib','stop_reason')}, ensure_ascii=False), flush=True)
            if case.startswith('geo-fanout-') and (result['stop_reason'] or result['exitcode'] != 0):
                fanout_stopped = True
    finally:
        verify(out/'frozen-after.json')
        # Only transient test directories owned by this invocation live here.
        import shutil
        shutil.rmtree(temp)
    summary = dict(cases_requested=len(cases), cases_executed=sum(r.get('exitcode') is not None for r in results),
                   cases_passed=sum(r.get('exitcode')==0 for r in results),
                   cases_resource_stopped=sum(bool(r.get('stop_reason')) for r in results),
                   cases_failed=sum(r.get('exitcode') not in (0,None) and not r.get('stop_reason') for r in results),
                   cases_skipped=sum(r.get('exitcode') is None for r in results),
                   completed_measurements=sum(len(r.get('metrics',[])) for r in results),
                   protected_source_unchanged=not json.loads((out/'frozen-after.json').read_text())['mismatches'])
    atomic_json(out/'summary.json', summary)
    print(json.dumps(dict(output=str(out),**summary),ensure_ascii=False), flush=True)
    return 0 if summary['cases_failed']==0 and summary['protected_source_unchanged'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
