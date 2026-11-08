#!/usr/bin/env python3
"""Check S2 evidence assertions; success means reproductions agree, not acceptance."""
import csv
import datetime
import hashlib
import json
from pathlib import Path
from audit_common import MANIFEST, load_manifest

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'build/agent-audit/S2'
rows=json.loads((OUT/'syscall-all-accepted.json').read_text())
byname={r['name']:r for r in rows}
namespace=json.loads((OUT/'namespace-results-final2.json').read_text())
ns={r['name']:r for r in namespace}
checks=[]


def check(label,condition):
    checks.append({'label':label,'pass':bool(condition)})
    if not condition: raise AssertionError(label)


def response(record,key='action'):
    return record.get(key,{}).get('response') or {}


def inspection(record):
    return (record.get('inspection') or {}).get('result') or {}


def unchanged(r,path):
    return r.get('before',{}).get(path)==r.get('afterFault',{}).get(path)


for r in rows:
    action=r.get('action',{})
    tracepath=Path(action.get('trace') or Path(r['fixture'])/'generate.strace')
    if not tracepath.exists(): tracepath=Path(r['fixture'])/'generator.strace'
    trace=tracepath.read_text() if tracepath.exists() else ''
    command=' '.join(action.get('command',[]))
    if 'error=' in command and 'fsync-observation' not in r['name'] and 'no-storage-sync' not in r['name']:
        check(r['name']+': target syscall injected', '(INJECTED)' in trace)
    if 'signal=SIGKILL' in command:
        check(r['name']+': own child terminated by SIGKILL',action['exit']==-9 and 'killed by SIGKILL' in trace)
    if 'fsync-observation' in r['name']:
        check(r['name']+': no fsync reached, not a simulated fsync failure',' fsync(' not in trace and '(INJECTED)' not in trace)
    if 'inspection' in r:
        i=inspection(r)
        check(r['name']+': all three published JSON/structures parse',all(i.get(k) for k in ['configJSON','nodesJSON','configValid','nodesValid','catalogueValid']))
    if 'run/current' in r['before']:
        check(r['name']+': current sentinel unchanged',unchanged(r,'run/current'))
        for path in r['before']:
            if path.startswith('run/generation.working/'):
                check(r['name']+': previous runtime bytes unchanged',unchanged(r,path))

for name in ['atomic-write-eio','atomic-file-fsync-eio','atomic-rename-eperm']:
    check(name+': single-file old bytes preserved',byname[name]['unchanged'])
check('atomic directory fsync error occurs after new file visibility',not byname['atomic-directory-fsync-eio']['unchanged'])
for name,tag in [('save-nodes-dir-fsync-eio','local/old'),('save-config-dir-fsync-eio','local/new'),
                 ('save-kill-before-config-rename','local/old'),('save-rollback-fsync-eio','local/old')]:
    r=byname[name]
    check(name+': cross-file mismatch observed',inspection(r)['missingLocalTags']==[tag])
    check(name+': subsequent compile rejected',bool(response(r,'restart').get('error')))

for r in rows:
    if r['name'].startswith('catalogue-'):
        check(r['name']+': bound ID retained',not inspection(r)['missingNodeRefs'])
        check(r['name']+': next load/compile succeeds',response(r,'restart').get('error')=='')
    if r['name'].startswith('geo-'):
        state=response(r,'restart')
        check(r['name']+': reopened geo pair parses',state.get('error')=='')
        result=state.get('result',{})
        new=r['name'] in ['geo-baseline','geo-kill-after-current','geo-fsync-observation']
        check(r['name']+': coherent old/new pair',result.get('domains')==['domain:'+('new' if new else 'old')+'.s2.example'] and result.get('ips')==['192.0.2.'+('2' if new else '1')+'/32'])
    if r['name'].startswith('device-'):
        check(r['name']+': later DeviceID succeeds',response(r,'restart').get('error')=='')
    if r['name'].startswith('generation-'):
        check(r['name']+': subsequent generation builds ten files',r['retry']['exit']==0 and len(r['retry']['files'])==10)
        check(r['name']+': current retains old target',unchanged(r,'run/current'))
        if r['name'] in ['generation-baseline','generation-fsync-observation']:
            check(r['name']+': complete generation published',len(r['publishedFiles'])==10)
        elif r['name']!='generation-existing-output':
            check(r['name']+': no partial output published',not r['published'])
existing=byname['device-existing-no-storage-sync']
check('existing synthetic DeviceID keeps identical bytes',existing['before']['hwid']['sha256']==existing['afterFault']['hwid']['sha256']==existing['afterRestart']['hwid']['sha256'])

race=byname['save-vs-stale-generator']
check('D-S2-01: concurrent save succeeded',race['save']['response']['error']=='')
check('D-S2-01: stale generator succeeded',race['action']['exit']==0)
check('D-S2-01: saved nodeRef record lost',inspection(race)['missingNodeRefs']==['srv_22222222222222222222222222222222'])
check('D-S2-01: provider return does not recover old ID',race['inspectionAfterProviderReturns']['result']['missingNodeRefs']==['srv_22222222222222222222222222222222'])

for name in ['missing-generation','missing-runtime','missing-rules','missing-xray','nft-apply-fail']:
    r=ns[name]
    check(name+': current retained',r['before']['files']['current']==r['after']['files']['current'])
    check(name+': no newly owned policies survive',not any(r['after']['state']['policies'].values()))
    check(name+': procd not touched',not r['after']['procdInstances'])
for name in ['ln-fail','mv-fail']:
    check(name+': previous pointer restored',ns[name]['after']['files']['current']==ns[name]['before']['files']['current'])
    check(name+': new policies survive despite pointer rollback',sum(ns[name]['after']['state']['policies'].values())==2)
check('D-S2-02: successful stop leaves two owned policies',ns['stop-corrupt-current']['exit']==0 and sum(ns['stop-corrupt-current']['after']['state']['policies'].values())==2)
check('D-S2-02: stop clears current without policy cleanup','current' not in ns['stop-corrupt-current']['after']['files'])
first=ns['first-ln-fail']
check('D-S2-03: failed first start leaves active network without current',first['exit']!=0 and first['after']['state']['table'] and 'current' not in first['after']['files'] and not first['after']['procdInstances'])
check('D-S2-03: candidate removed and retry blocked',not any(p.startswith('generation.new/') for p in first['after']['files']) and first['retry']['exit']==1)
tiny=ns['atomic-real-enospc']
tinyresult=json.loads(next(x[len('S2_RESULT '):] for x in tiny['stdout'].splitlines() if x.startswith('S2_RESULT ')))['result']
check('64 KiB private tmpfs: true ENOSPC, old JSON retained, retry succeeds',all(tinyresult[k] for k in ['fillENOSPC','writeENOSPC','unchanged','jsonValid','retryOK']))

with (OUT/'file-state-matrix.tsv').open('w') as out:
    columns=['case','path','before_sha256','after_fault_sha256','before_link','after_fault_link','config_valid','nodes_valid','catalogue_valid','missing_nodeRefs','missing_local_tags','later_load_compile_error','evidence']
    writer=csv.DictWriter(out,columns,delimiter='\t');writer.writeheader()
    for r in rows:
        i=inspection(r)
        paths=set(r['before'])|set(r['afterFault'])
        for path in sorted(paths):
            if path not in ['config.json','nodes.json','servers.json','atomic.json','hwid','run/current','geodata/current'] and not path.startswith(('geodata/generation.','run/generation.','run/.nadkop-')): continue
            a,b=r['before'].get(path,{}),r['afterFault'].get(path,{})
            writer.writerow({'case':r['name'],'path':path,'before_sha256':a.get('sha256','ABSENT'),'after_fault_sha256':b.get('sha256','ABSENT'),
                             'before_link':a.get('symlink',''),'after_fault_link':b.get('symlink',''),'config_valid':i.get('configValid','n/a'),
                             'nodes_valid':i.get('nodesValid','n/a'),'catalogue_valid':i.get('catalogueValid','n/a'),
                             'missing_nodeRefs':','.join(i.get('missingNodeRefs',[])),'missing_local_tags':','.join(i.get('missingLocalTags',[])),
                             'later_load_compile_error':response(r,'restart').get('error','n/a'),'evidence':str(Path(r['fixture'])/'evidence.json')})

manifest=MANIFEST
expected=load_manifest()['files']
actual=[{'path':p,'expected':h,'actual':hashlib.sha256((ROOT/p).read_bytes()).hexdigest()} for p,h in expected.items()]
for item in actual: item['match']=item['expected']==item['actual']
check(f'protected production files: {len(actual)}/{len(expected)} frozen hashes',len(actual)==len(expected) and all(r['match'] for r in actual))
before=json.loads((OUT/'frozen-before.json').read_text())
before_map={r['path']:r['actual'] for r in before['files']}
check('protected files unchanged from S2 start',all(before_map.get(r['path'])==r['actual'] for r in actual))
after={'phase':'after','at':datetime.datetime.now(datetime.timezone.utc).isoformat(),
       'manifest':str(manifest.relative_to(ROOT)),'manifest_sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),
       'files':actual,'mismatches':[r for r in actual if not r['match']],
       'changedSinceBefore':[r['path'] for r in actual if before_map.get(r['path'])!=r['actual']]}
(OUT/'frozen-after.json').write_text(json.dumps(after,ensure_ascii=False,indent=2)+'\n')
summary={'checks':checks,'check_count':len(checks),'checks_passed':sum(c['pass'] for c in checks),'syscall_cases':len(rows),
         'namespace_cases':len(namespace),'confirmed_contract_defects':['D-S2-01','D-S2-02','D-S2-03'],
         'meaning':'Assertions verify recorded behavior, including defects; this is not a clean conformance result.'}
(OUT/'verified-evidence.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:v for k,v in summary.items() if k!='checks'},ensure_ascii=False,indent=2))
