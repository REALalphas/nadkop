#!/usr/bin/env python3
"""C2 command/state regression. Real init/Go; synthetic ip/nft/procd in bwrap.

No host network commands or sysctl writes. --variant baseline asserts the known
old defects separately; only --variant fixed asserts closure requirements.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'build/blocker-closure/C2'
SHELL = Path('/bin/sh')
NADKOP_WRAPPER = '''#!/bin/sh
if [ "$C2_FAULT" = commit ] && [ "$1" = network ] && [ "$2" = commit ]; then
 echo 'Injected network commit cleanup failure' >&2
 exit 1
fi
exec /usr/bin/nadkop-real "$@"
'''
MOCK = r'''#!PYTHON
import json,os,re,sys
from pathlib import Path
name=Path(sys.argv[0]).name
a=sys.argv[1:]
p=Path('/case/state.json'); s=json.loads(p.read_text())
with open('/case/commands.jsonl','a') as f: f.write(json.dumps({'name':name,'args':a})+'\n')
def save(): p.write_text(json.dumps(s))
def fail(): sys.exit(1)
def fault(kind):
 if os.environ.get('C2_FAULT')!=kind: return False
 if os.environ.get('C2_PERSIST')=='1': return True
 key='fault-'+kind
 if s.get(key): return False
 s[key]=True; save(); return True
if name=='xray':
 try: json.loads(Path(a[a.index('-config')+1]).read_text())
 except Exception: fail()
elif name=='nfqws': pass
elif name=='ubus': print('{}')
elif name=='uci': print('1' if a[-1].endswith('auto_includes') else '0')
elif name=='ip':
 if a[:2]==['link','show']: sys.exit(0)
 if '-6' in a and os.environ.get('C2_NO_IPV6')=='1': fail()
 if a[0]=='-j':
  family,kind=a[1:3]
  if fault('inspect-'+family): fail()
  rows=list(s['foreign'][family+'/'+kind])
  if s['policies'][family+'/'+kind]:
   rows.append({'priority':10000,'table':100,'src':'all','fwmark':'0x10000','fwmask':'0x10000'} if kind=='rule' else {'table':100,'type':'local','dst':'default','dev':'lo'})
  print(json.dumps(rows))
 elif len(a)>2 and a[2] in ('add','del'):
  family,kind,op=a[:3]
  if fault(op+'-'+family+'-'+kind): fail()
  if op=='del' and not s['policies'][family+'/'+kind]: fail()
  s['policies'][family+'/'+kind]=op=='add'; save()
 else: fail()
elif name=='nft':
 if a[:2]==['list','table']:
  if not s['table']: fail()
  print(s['table'])
 elif a[:3]==['-a','list','chain']:
  print('chain input {')
  print(' iifname "foreign0" accept # handle 77')
  if s['input']: print(' '+s['input'].strip()+' # handle 78')
  print('}')
 elif a[:2]==['-c','-f']:
  if not Path(a[2]).is_file(): fail()
 elif a[0]=='-f':
  script=Path(a[1]).read_text()
  if fault('nft') or ('network-rollback.' in a[1] and fault('nft-rollback')): fail()
  table=re.search(r'(?m)^table inet nadkop [^\n]*\n?',script)
  if 'delete table inet nadkop' in script: s['table']=''
  if table: s['table']=table.group(0).strip()
  if 'delete rule inet fw4 input handle 78' in script: s['input']=''
  insert=re.search(r'(?m)^insert rule inet fw4 input (.*)',script)
  if insert: s['input']=insert.group(1)
  if 'handle 77' in script: raise AssertionError('foreign firewall rule deletion')
  save()
 else: fail()
else: fail()
'''
RUNNER = r'''#!/bin/sh
export PATH=/tools/bin:/bin
action=
. /case/nadkop.init
procd_open_instance() { printf '%s\n' "$1" >> /case/procd.log; }
procd_set_param() { :; }
procd_close_instance() { :; }
logger() { printf '%s\n' "$*" >> /case/logger.log; }
jsonfilter() { printf '0\n'; }
ln() { [ "$C2_PUBLICATION" != ln ] || return 1; /tools/bin/ln "$@"; }
mv() {
 [ "$C2_PUBLICATION" != mv ] || return 1
 [ "$C2_PUBLICATION" != kill ] || kill -KILL "$$"
 /tools/bin/mv "$@"
}
if [ "$C2_ACTION" = stop ]; then
 /usr/bin/nadkop network stop
 rc=$?
else
 NADKOP_PREPARED=/var/run/nadkop/generation.new
 start_service
 rc=$?
fi
printf 'C2_INIT_RC=%s\n' "$rc"
exit "$rc"
'''


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def network(state):
    return {k: state[k] for k in ('table', 'input', 'policies', 'foreign')}


def snapshot(path):
    files = {}
    for p in sorted((path/'run').rglob('*')):
        key = str(p.relative_to(path/'run'))
        if p.is_symlink():
            files[key] = {'symlink': os.readlink(p)}
        elif p.is_file():
            data = p.read_bytes()
            files[key] = {'sha256': hashlib.sha256(data).hexdigest(), 'bytes': len(data)}
    return {'files': files, 'state': json.loads((path/'state.json').read_text()),
            'procd': (path/'procd.log').read_text().splitlines() if (path/'procd.log').exists() else []}


def base(path, binary, ipv6=True):
    cmd = ['bwrap', '--die-with-parent', '--unshare-all', '--ro-bind', '/nix/store', '/nix/store',
           '--ro-bind', '/run/current-system/sw', '/tools', '--dev', '/dev', '--proc', '/proc',
           '--tmpfs', '/tmp', '--dir', '/bin', '--ro-bind', str(SHELL), '/bin/sh', '--dir', '/usr/bin',
           '--dir', '/usr/sbin', '--dir', '/sbin', '--dir', '/etc', '--dir', '/var', '--dir', '/run',
           '--symlink', '../run', '/var/run', '--bind', str(path), '/case', '--dir', '/run/nadkop',
           '--bind', str(path/'run'), '/run/nadkop', '--ro-bind', str(binary), '/usr/bin/nadkop-real',
           '--ro-bind', str(path/'nadkop-wrapper'), '/usr/bin/nadkop',
           '--setenv', 'PATH', '/tools/bin:/bin', '--setenv', 'GOMAXPROCS', '1']
    if not ipv6:
        # Hide only this namespace's IPv6 kernel capability indicator; ip is
        # also mocked as unavailable. No write to real /proc or real sysctl.
        cmd += ['--tmpfs', '/proc/sys/net']
    for name in ['/usr/bin/xray', '/usr/sbin/nfqws', '/usr/sbin/nft', '/sbin/ip', '/sbin/uci', '/bin/ubus']:
        cmd += ['--ro-bind', str(path/'mock'), name]
    return cmd


def plan(enabled, ipv6=False):
    return {'transparent': {'enabled': enabled, 'interfaces': ['br-c2'], 'port': 12345, 'dnsPort': 1053,
                            'dnsUpstream': '127.0.0.1', 'ipv6': ipv6},
            'socksPort': 1080, 'health': False, 'zapret': False, 'updateIntervalHours': 0}


def generation(path, name, enabled, ipv6=False):
    d = path/'run'/name
    d.mkdir(exist_ok=True)
    dump(d/'runtime.json', plan(enabled, ipv6))
    dump(d/'xray.json', {'synthetic': name})
    (d/'rules.nft').write_text('table inet nadkop { comment "'+name+'"; }\n' if enabled else '')
    (d/'input.nft').write_text('meta mark & 0x10000 == 0x10000 accept comment "nadkop-tproxy"\n' if enabled else '')


def execute(path, binary, tag, env, ipv6=True):
    cmd = base(path, binary, ipv6)
    for key, value in env.items():
        cmd += ['--setenv', key, value]
    cmd += ['/bin/sh', '/case/runner.sh']
    r = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=25)
    (path/(tag+'.stdout')).write_text(r.stdout)
    return {'exit': r.returncode, 'stdout': r.stdout, 'command': cmd, 'after': snapshot(path)}


def check(result, condition, message):
    result['assertions'].append({'pass': bool(condition), 'message': message})


def case(root, binary, variant, name, old=None, new=True, old6=False, new6=False, publication='',
         fault='', persist=False, action='start', corruption='', foreign='', ipv6=True):
    path = root/name
    path.mkdir()
    (path/'run').mkdir()
    source = ROOT/('build/blocker-closure/baseline' if variant == 'baseline' else '.')
    shutil.copyfile(source/'openwrt/nadkop/files/nadkop.init', path/'nadkop.init')
    python = str(Path(shutil.which('python3')).resolve())
    (path/'mock').write_text(MOCK.replace('PYTHON', python))
    (path/'mock').chmod(0o700)
    (path/'runner.sh').write_text(RUNNER)
    (path/'runner.sh').chmod(0o700)
    (path/'nadkop-wrapper').write_text(NADKOP_WRAPPER)
    (path/'nadkop-wrapper').chmod(0o700)
    state = {'table': '', 'input': '', 'policies': {}, 'foreign': {}}
    for family in ['-4', '-6']:
        for kind in ['route', 'rule']:
            key = family+'/'+kind
            state['policies'][key] = bool(old and (family == '-4' or old6))
            state['foreign'][key] = []
    # Ordinary unrelated routing policies always exist and must survive.
    state['foreign']['-4/rule'] = [{'priority': 32766, 'table': 'main', 'src': 'all'}]
    state['foreign']['-4/route'] = [{'table': 'main', 'dst': 'default', 'dev': 'wan0', 'gateway': '192.0.2.1'}]
    if old is not None:
        generation(path, 'generation.old', old, old6)
        os.symlink('/var/run/nadkop/generation.old', path/'run/current')
        if old:
            state['table'] = (path/'run/generation.old/rules.nft').read_text().strip()
            state['input'] = (path/'run/generation.old/input.nft').read_text().strip()
    generation(path, 'generation.new', new, new6)
    if corruption == 'corrupt':
        (path/'run/generation.old/runtime.json').write_text('{')
    elif corruption == 'missing':
        (path/'run/generation.old/runtime.json').unlink()
    elif corruption == 'dangling':
        shutil.rmtree(path/'run/generation.old')
        state['table'] = ''; state['input'] = ''
    if foreign:
        if foreign == 'table':
            state['table'] = 'table inet nadkop { comment "foreign"; }'
        elif foreign == 'route':
            state['foreign']['-4/route'].append({'table': 100, 'dst': 'default', 'dev': 'wan0'})
        else:
            row = {'priority': 10000, 'table': 100, 'src': 'all', 'fwmark': '0x10000', 'fwmask': '0x10000', foreign: 6}
            state['foreign']['-4/rule'].append(row)
    dump(path/'state.json', state)
    before = snapshot(path)
    env = {'C2_PUBLICATION': publication, 'C2_FAULT': fault, 'C2_PERSIST': '1' if persist else '0',
           'C2_ACTION': action, 'C2_NO_IPV6': '0' if ipv6 else '1'}
    run = execute(path, binary, 'run', env, ipv6)
    after = run['after']; result = {'name': name, 'variant': variant, 'before': before, 'run': run, 'assertions': []}
    check(result, before['state']['foreign'] == after['state']['foreign'], 'foreign policy data retained')
    if variant == 'baseline':
        if action == 'stop':
            check(result, run['exit'] == 0 and after['state']['policies']['-4/rule'], 'reproduced old false success / retained policies')
        elif old is None:
            check(result, run['exit'] != 0 and bool(after['state']['table']) and after['state']['policies']['-4/rule'], 'reproduced old first-start leak')
        elif fault:
            check(result, run['exit'] != 0 and not after['state']['table'], 'reproduced old nft rollback omission after cleanup error')
        else:
            check(result, run['exit'] != 0 and after['state']['policies']['-4/rule'], 'reproduced old added-policy leak')
    elif foreign:
        check(result, run['exit'] != 0, 'foreign conflict reported')
        check(result, before['state']['policies'] == after['state']['policies'], 'no policy mutation on ownership conflict')
        if foreign == 'table':
            check(result, network(before['state']) == network(after['state']), 'unowned table retained')
    elif action == 'stop':
        if fault:
            check(result, run['exit'] != 0 and 'current' in after['files'], 'failed cleanup reported; current retained for retry')
            check(result, not after['state']['policies']['-4/route'] and
                  (fault == 'inspect--6' or not after['state']['policies']['-6/route']), 'cleanup continued in inspectable families after error')
        else:
            check(result, run['exit'] == 0, 'complete cleanup succeeds independently of runtime')
            check(result, not any(after['state']['policies'].values()), 'all exact owned IPv4/IPv6 policies removed')
            check(result, not after['state']['table'] and not after['state']['input'] and 'current' not in after['files'], 'firewall and current removed')
    elif fault == 'commit':
        check(result, run['exit'] == 0 and after['procd'] == ['xray'], 'published generation registers Xray despite journal cleanup failure')
        check(result, after['files'].get('current', {}).get('symlink') == '/var/run/nadkop/generation.new', 'published current retained')
        check(result, 'network-transaction.json' in after['files'], 'failed journal cleanup retained for recovery')
        check(result, 'journal cleanup failed' in (path/'logger.log').read_text() if (path/'logger.log').exists() else False, 'journal cleanup failure is reported')
        expected = {family+'/'+kind: new and (family == '-4' or new6) for family in ['-4', '-6'] for kind in ['rule', 'route']}
        check(result, after['state']['policies'] == expected, 'published network and process configuration agree')
    elif publication or fault:
        check(result, run['exit'] != 0 and not after['procd'], 'failure returned before procd registration')
        check(result, before['files'].get('current') == after['files'].get('current'), 'previous current retained')
        if publication == 'kill' or (persist and not name.startswith('cleanup-')):
            check(result, 'network-transaction.json' in after['files'], 'incomplete rollback retains recovery journal')
        else:
            check(result, network(before['state']) == network(after['state']), 'network fully restored to pre-apply state')
            check(result, 'network-transaction.json' not in after['files'], 'completed rollback removes journal')
    else:
        check(result, run['exit'] == 0 and after['procd'] == ['xray'], 'positive start registers Xray')
        check(result, after['files'].get('current', {}).get('symlink') == '/var/run/nadkop/generation.new', 'new current published')
        check(result, 'network-transaction.json' not in after['files'], 'successful start commits journal')
        expected = {family+'/'+kind: new and (family == '-4' or new6) for family in ['-4', '-6'] for kind in ['rule', 'route']}
        check(result, after['state']['policies'] == expected, 'successful transitions reconcile both policy families')
    # First retry after removing the injected fault. Do not repair current,
    # policies, table or journal. Only provide the candidate init may have removed.
    if not foreign:
        generation(path, 'generation.new', new if action != 'stop' else True, new6)
        retry = execute(path, binary, 'retry', {'C2_PUBLICATION': '', 'C2_FAULT': '', 'C2_ACTION': action,
                                              'C2_NO_IPV6': '0' if ipv6 else '1'}, ipv6)
        result['retry'] = retry
        if variant == 'fixed':
            if new6 and not ipv6:
                check(result, retry['exit'] != 0 and network(before['state']) == network(retry['after']['state']),
                      'IPv6-required start stays rejected until capability is available')
            else:
                check(result, retry['exit'] == 0, 'first retry succeeds after fault removal')
            check(result, 'network-transaction.json' not in retry['after']['files'], 'retry leaves no pending journal')
        elif old is None:
            check(result, retry['exit'] != 0 and 'without an owned generation' in retry['stdout'], 'reproduced old first retry blocker')
    result['calls'] = [json.loads(line) for line in (path/'commands.jsonl').read_text().splitlines()]
    dump(path/'evidence.json', result)
    failed = [a['message'] for a in result['assertions'] if not a['pass']]
    print(('FAIL' if failed else 'PASS'), variant, name, 'exit='+str(run['exit']), '; '.join(failed), flush=True)
    return result


def main():
    global SHELL
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--variant', choices=['baseline', 'fixed'], default='fixed')
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--shell', type=Path, default=Path('/bin/sh'))
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--case', default='.')
    args = parser.parse_args()
    SHELL = args.shell.absolute()
    if not re.fullmatch(r'[A-Za-z0-9_-]+', args.run_id): raise ValueError('invalid run id')
    root = OUT/(args.variant+'-'+args.run_id)
    root.mkdir(parents=True, exist_ok=False)
    binary = args.binary.resolve() if args.binary else OUT/('nadkop-baseline' if args.variant == 'baseline' else 'nadkop')
    jobs = [dict(name='first-ln', publication='ln'), dict(name='first-mv', publication='mv'),
            dict(name='disabled-ln', old=False, publication='ln'), dict(name='disabled-mv', old=False, publication='mv'),
            dict(name='cleanup-after-nft', old=True, new=False, fault='del--4-rule'),
            dict(name='stop-corrupt', old=True, action='stop', corruption='corrupt')]
    if args.variant == 'fixed':
        jobs += [dict(name='enabled-ln', old=True, publication='ln'), dict(name='enabled-mv', old=True, publication='mv'),
                 dict(name='v4-to-dual-ln', old=True, new6=True, publication='ln'),
                 dict(name='dual-to-v4-mv', old=True, old6=True, publication='mv'),
                 dict(name='dual-to-off-ln', old=True, old6=True, new=False, publication='ln'),
                 dict(name='cleanup-v6-after-nft', old=True, old6=True, new=False, fault='del--6-route'),
                 dict(name='cleanup-persistent', old=True, old6=True, new=False, fault='del--4-rule', persist=True),
                 dict(name='rollback-nft-failure', publication='ln', fault='nft-rollback', persist=True),
                 dict(name='rollback-policy-failure', publication='mv', fault='del--4-rule', persist=True),
                 dict(name='kill-first-before-current', publication='kill'),
                 dict(name='kill-disabled-before-current', old=False, publication='kill'),
                 dict(name='apply-nft-failure', fault='nft'),
                 dict(name='apply-v4-rule-failure', fault='add--4-rule'),
                 dict(name='apply-v6-route-failure', new6=True, fault='add--6-route'),
                 dict(name='first-success'), dict(name='off-to-on', old=False),
                 dict(name='dual-to-off', old=True, old6=True, new=False), dict(name='dual-to-v4', old=True, old6=True),
                 dict(name='off-to-off', old=False, new=False),
                 dict(name='stop-missing', old=True, old6=True, action='stop', corruption='missing'),
                 dict(name='stop-dangling', old=True, old6=True, action='stop', corruption='dangling'),
                 dict(name='stop-no-current', action='stop'),
                 dict(name='stop-no-ipv6', old=True, action='stop', corruption='corrupt', ipv6=False),
                 dict(name='start-no-ipv6', ipv6=False),
                 dict(name='start-ipv6-unavailable', new6=True, fault='inspect--6', ipv6=False),
                 dict(name='stop-ipv6-inspection-failure', old=True, old6=True, action='stop', fault='inspect--6'),
                 dict(name='stop-delete-error', old=True, old6=True, action='stop', fault='del--4-rule'),
                 dict(name='foreign-rule-apply', foreign='ipproto'),
                 dict(name='foreign-route-apply', foreign='route'),
                 dict(name='foreign-rule-stop', old=True, action='stop', foreign='ipproto'),
                 dict(name='foreign-route-stop', old=True, action='stop', foreign='route'),
                 dict(name='foreign-table-apply', foreign='table')]
        jobs += [dict(name='commit-first-cleanup-failure', fault='commit'),
                 dict(name='commit-replace-cleanup-failure', old=True, old6=True, new6=True, fault='commit')]
    jobs = [job for job in jobs if re.search(args.case, job['name'])]
    if not jobs: raise ValueError('no matching cases')
    results = [case(root, binary, args.variant, **job) for job in jobs]
    total = sum(len(r['assertions']) for r in results)
    failed = sum(not a['pass'] for r in results for a in r['assertions'])
    dump(root/'results.json', {'variant': args.variant, 'cases': results, 'assertions': total, 'failed': failed,
                              'limits': 'real init/Go only; mocked ip/nft/Xray/procd, no real kernel/LAN validation'})
    print(f'{args.variant}: {len(results)} cases, {total-failed}/{total} assertions passed')
    return 1 if failed else 0


if __name__ == '__main__':
    raise SystemExit(main())
