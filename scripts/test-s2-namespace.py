#!/usr/bin/env python3
"""S2 init/current tests with real Go network code and mocked external tools.

All absolute OpenWrt paths exist only in an unshared bwrap mount/user/net/pid
namespace. External tools record synthetic state, never mutate real networking.
"""
import hashlib
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/"build/agent-audit/S2"
FIX=OUT/"namespace-fixtures"
MOCK=r'''#!PYTHON
import json,os,re,sys
from pathlib import Path
name=Path(sys.argv[0]).name
a=sys.argv[1:]
statepath=Path('/case/state.json')
state=json.loads(statepath.read_text())
with open('/case/commands.jsonl','a') as out: out.write(json.dumps({'name':name,'args':a})+'\n')
def save(): statepath.write_text(json.dumps(state))
def fail(): sys.exit(1)
if name=='xray':
 try: json.loads(Path(a[a.index('-config')+1]).read_text())
 except Exception: fail()
elif name=='nfqws': pass
elif name=='ubus': print('{}')
elif name=='uci': print('1' if a[-1].endswith('auto_includes') else '0')
elif name=='ip':
 if a[:2]==['link','show']: sys.exit(0)
 if a[0]=='-j':
  family,kind=a[1],a[2]
  present=state['policies'].get(family+'/'+kind,False)
  rule={'priority':10000,'table':100,'src':'all','fwmark':'0x10000','fwmask':'0x10000'}
  route={'table':100,'type':'local','dst':'default','dev':'lo'}
  print(json.dumps([rule if kind=='rule' else route] if present else []))
 elif len(a)>2 and a[2] in ('add','del'):
  if a[2]=='del' and os.environ.get('S2_FAIL_CLEANUP')=='1': fail()
  state['policies'][a[0]+'/'+a[1]]=a[2]=='add'; save()
 else: fail()
elif name=='nft':
 if a[:2]==['list','table']:
  if not state['table']: fail()
  print('table inet nadkop {}')
 elif a[:3]==['-a','list','chain']: print('chain input {}')
 elif a[:2]==['-c','-f']:
  if not Path(a[2]).is_file(): fail()
 elif a[0]=='-f':
  if os.environ.get('S2_FAIL_NFT')=='1': fail()
  parent=Path(a[1]).parent.name
  state['networkGeneration']=parent
  state['table']=bool(re.search(r'(?m)^(?:add )?table inet nadkop\b',Path(a[1]).read_text())); save()
 else: fail()
else: fail()
'''

RUNNER=r'''#!/bin/sh
export PATH=/tools/bin:/bin
action=
. /case/nadkop.init
procd_open_instance() { printf '%s\n' "$1" >> /case/procd.log; }
procd_set_param() { :; }
procd_close_instance() { :; }
logger() { printf '%s\n' "$*" >> /case/logger.log; }
jsonfilter() { printf '0\n'; }
ln() {
 if [ "$S2_SCENARIO" = ln-fail ] || [ "$S2_SCENARIO" = first-ln-fail ]; then return 1; fi
 /tools/bin/ln "$@"
}
mv() {
 if [ "$S2_SCENARIO" = mv-fail ]; then return 1; fi
 if [ "$S2_SCENARIO" = kill-after-apply ]; then kill -KILL "$$"; fi
 /tools/bin/mv "$@"
}
if [ "$S2_SCENARIO" = stop-corrupt-current ]; then
 /usr/bin/nadkop network stop
 rc=$?
else
 NADKOP_PREPARED=/var/run/nadkop/generation.new
 start_service
 rc=$?
fi
printf 'S2_INIT_RC=%s\n' "$rc"
exit "$rc"
'''


def dump(path,value):
    path.write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n')


def base(path):
    return ['bwrap','--die-with-parent','--unshare-all','--ro-bind','/nix/store','/nix/store',
            '--ro-bind','/run/current-system/sw','/tools','--dev','/dev','--proc','/proc','--tmpfs','/tmp',
            '--dir','/bin','--ro-bind','/bin/sh','/bin/sh','--dir','/usr/bin','--dir','/usr/sbin','--dir','/sbin',
            '--dir','/etc','--dir','/var','--dir','/run','--symlink','../run','/var/run',
            '--bind',str(path),'/case','--dir','/run/nadkop','--bind',str(path/'run'),'/run/nadkop',
            '--ro-bind',str(OUT/'nadkop'),'/usr/bin/nadkop','--setenv','PATH','/tools/bin:/bin',
            '--setenv','GOMAXPROCS','1','--setenv','GODEBUG','asyncpreemptoff=1']


def snapshot(path):
    files={}
    for item in sorted((path/'run').rglob('*')):
        if item.is_symlink(): files[str(item.relative_to(path/'run'))]={'symlink':os.readlink(item)}
        elif item.is_file():
            data=item.read_bytes(); files[str(item.relative_to(path/'run'))]={'sha256':hashlib.sha256(data).hexdigest(),'bytes':len(data)}
    return {'files':files,'state':json.loads((path/'state.json').read_text()),
            'procdInstances':(path/'procd.log').read_text().splitlines() if (path/'procd.log').exists() else []}


def initcase(name):
    path=FIX/name; path.mkdir(parents=True,exist_ok=False); (path/'run').mkdir()
    shutil.copyfile(ROOT/'openwrt/nadkop/files/nadkop.init',path/'nadkop.init')
    python=str(Path(shutil.which('python3')).resolve())
    (path/'mock').write_text(MOCK.replace('PYTHON',python)); (path/'mock').chmod(0o700)
    (path/'runner.sh').write_text(RUNNER); (path/'runner.sh').chmod(0o700)
    old_enabled=name in ('cleanup-after-nft-fail','stop-corrupt-current')
    state={'networkGeneration':'generation.old','table':old_enabled,
           'policies':{'-4/route':old_enabled,'-4/rule':old_enabled,'-6/route':False,'-6/rule':False}}
    dump(path/'state.json',state)
    plan={'transparent':{'enabled':True,'interfaces':['br-s2'],'port':12345,'dnsPort':1053,'dnsUpstream':'127.0.0.1','ipv6':False},
          'socksPort':1080,'health':False,'zapret':False,'updateIntervalHours':0}
    for gen in ['generation.old','generation.new']:
        d=path/'run'/gen; d.mkdir()
        p=json.loads(json.dumps(plan))
        p['transparent']['enabled']=old_enabled if gen=='generation.old' else name!='cleanup-after-nft-fail'
        dump(d/'runtime.json',p); dump(d/'xray.json',{'synthetic':gen})
        (d/'rules.nft').write_text('table inet nadkop {}\n' if p['transparent']['enabled'] else '')
        (d/'input.nft').write_text('')
    first=name=='first-ln-fail'
    if not first: os.symlink('/var/run/nadkop/generation.old',path/'run/current')
    if name=='missing-generation': shutil.rmtree(path/'run/generation.new')
    if name=='missing-runtime': (path/'run/generation.new/runtime.json').unlink()
    if name=='missing-rules': (path/'run/generation.new/rules.nft').unlink()
    if name=='missing-xray': (path/'run/generation.new/xray.json').unlink()
    if name=='stop-corrupt-current': (path/'run/generation.old/runtime.json').write_text('{')
    before=snapshot(path)
    cmd=base(path)
    for binary in ['/usr/bin/xray','/usr/sbin/nfqws','/usr/sbin/nft','/sbin/ip','/sbin/uci','/bin/ubus']:
        cmd+=['--ro-bind',str(path/'mock'),binary]
    cmd+=['--setenv','S2_SCENARIO',name]
    if name=='nft-apply-fail': cmd+=['--setenv','S2_FAIL_NFT','1']
    if name=='cleanup-after-nft-fail': cmd+=['--setenv','S2_FAIL_CLEANUP','1']
    cmd+=['/bin/sh','/case/runner.sh']
    p=subprocess.run(cmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (path/'run.stdout').write_text(p.stdout)
    result={'name':name,'fixture':str(path),'command':cmd,'exit':p.returncode,'stdout':p.stdout,'before':before,'after':snapshot(path)}
    if (path/'commands.jsonl').exists(): result['calls']=[json.loads(x) for x in (path/'commands.jsonl').read_text().splitlines()]
    # A fresh valid candidate is a separate, explicitly recorded recovery step;
    # the failed state above is captured before creating any replacement files.
    new=path/'run/generation.new'
    new.mkdir(exist_ok=True)
    dump(new/'runtime.json',plan); dump(new/'xray.json',{'synthetic':'generation.new-retry'})
    (new/'rules.nft').write_text('table inet nadkop {}\n'); (new/'input.nft').write_text('')
    retrycmd=base(path)
    for binary in ['/usr/bin/xray','/usr/sbin/nfqws','/usr/sbin/nft','/sbin/ip','/sbin/uci','/bin/ubus']:
        retrycmd+=['--ro-bind',str(path/'mock'),binary]
    retrycmd+=['--setenv','S2_SCENARIO','baseline','/bin/sh','/case/runner.sh']
    retry=subprocess.run(retrycmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (path/'retry.stdout').write_text(retry.stdout)
    result['retry']={'command':retrycmd,'exit':retry.returncode,'stdout':retry.stdout,'after':snapshot(path)}
    dump(path/'evidence.json',result)
    return result


def tinycase():
    path=FIX/'atomic-real-enospc'; path.mkdir(parents=True,exist_ok=False); (path/'run').mkdir()
    dump(path/'state.json',{})
    cmd=base(path)+['--ro-bind',str(OUT/'router.test'),'/s2-router.test','--size','65536','--tmpfs','/tiny',
                    '--setenv','S2_FIXTURE','/tiny','--setenv','S2_MODE','atomic-enospc',
                    '/s2-router.test','-test.run=^TestS2Child$','-test.v=false']
    p=subprocess.run(cmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (path/'run.stdout').write_text(p.stdout)
    result={'name':'atomic-real-enospc','fixture':str(path),'command':cmd,'exit':p.returncode,'stdout':p.stdout}
    dump(path/'evidence.json',result)
    return result


def main():
    global FIX
    parser=argparse.ArgumentParser()
    parser.add_argument('--run-id',default='')
    args=parser.parse_args()
    if args.run_id:
        if not all(c.isalnum() or c in '_-' for c in args.run_id): raise ValueError('invalid run id')
        FIX=FIX/args.run_id
    FIX.mkdir(parents=True,exist_ok=True)
    results=[]
    for name in ['baseline','missing-generation','missing-runtime','missing-rules','missing-xray','nft-apply-fail',
                 'ln-fail','mv-fail','first-ln-fail','kill-after-apply','cleanup-after-nft-fail','stop-corrupt-current']:
        r=initcase(name); results.append(r)
        print(name,'exit',r['exit'],'network',r['after']['state'].get('networkGeneration'),'current',r['after']['files'].get('current'),
              'procd',r['after']['procdInstances'],flush=True)
    r=tinycase(); results.append(r); print(r['name'],r['exit'],r['stdout'],flush=True)
    dump(OUT/('namespace-results'+('-'+args.run_id if args.run_id else '')+'.json'),results)


if __name__=='__main__': main()
