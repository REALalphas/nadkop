#!/usr/bin/env python3
"""Record the result of one frozen-source verification suite; never edit sources."""
import argparse, hashlib, json, os, pathlib, shutil, subprocess, sys, time
from audit_common import REPORT, load_manifest, require_geo, require_ui
root=pathlib.Path(__file__).resolve().parent.parent
parser=argparse.ArgumentParser();parser.add_argument('suite');args=parser.parse_args()
os.chdir(root)
report=REPORT
logs=report/'logs';logs.mkdir(parents=True,exist_ok=True)
manifest=load_manifest()
changed=[name for name,digest in manifest['files'].items() if not (root/name).is_file() or hashlib.sha256((root/name).read_bytes()).hexdigest()!=digest]
if changed:
    print('Frozen source changed:', ', '.join(changed));sys.exit(90)
env=os.environ.copy()
env.setdefault('NADKOP_GEO_TEST_FILES',str(root/'build/geodata'))
env.setdefault('NADKOP_EMOJI_TEST_CACHE',str(root/'build/assets-r24/emoji'))
suites={
 'unit':['go','test','-race','-count=1','-coverprofile='+str(logs/'coverage.out'),'-json','./...'],
 'vet':['go','vet','./...'],
 'picker':['node','scripts/test-picker.mjs'],
 'subscriptions':['node','scripts/test-subscriptions.mjs'],
 'ui':['node','scripts/test-product-ui.mjs'],
 'fuzz-config':['go','test','./internal/core','-run=^$','-fuzz=^FuzzProductConfigBoundary$','-fuzztime=12s','-parallel=2'],
 'fuzz-subscription':['go','test','./internal/core','-run=^$','-fuzz=^FuzzProductSubscriptionBoundary$','-fuzztime=12s','-parallel=2'],
 'shell-syntax':['sh','-c','for script in scripts/*.sh openwrt/nadkop/files/nadkop.init openwrt/nadkop/files/usr/libexec/rpcd/nadkop; do sh -n "$script" || exit; done'],
 'shellcheck':['nix-shell','-p','shellcheck','--run','shellcheck -S warning scripts/build-openwrt.sh scripts/install-openwrt.sh openwrt/nadkop/files/nadkop.init'],
}
if args.suite not in suites:parser.error('unknown suite')
if args.suite in ('picker', 'subscriptions', 'ui'):
    require_ui(env, emoji=args.suite == 'picker')
if args.suite == 'unit':
    require_geo(env['NADKOP_GEO_TEST_FILES'])
command=suites[args.suite];start=time.time()
skip = args.suite == 'shellcheck' and shutil.which('nix-shell') is None
with (logs/(args.suite+'.log')).open('w') as out:
    if skip:
        code=0;out.write('SKIP: nix-shell is unavailable; shellcheck suite was not run.\n')
    else:
        try:result=subprocess.run(command,env=env,stdout=out,stderr=subprocess.STDOUT,timeout=300);code=result.returncode
        except subprocess.TimeoutExpired:code=124;out.write('\nSUITE TIMEOUT\n')
changed=[name for name,digest in manifest['files'].items() if not (root/name).is_file() or hashlib.sha256((root/name).read_bytes()).hexdigest()!=digest]
record={'status':'SKIP' if skip else ('PASS' if code == 0 else 'FAIL'),'reason':'nix-shell unavailable' if skip else None,'suite':args.suite,'command':command,'exitCode':code,'seconds':round(time.time()-start,2),'sourceUnchanged':not changed,'changed':changed}
(logs/(args.suite+'.json')).write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps(record));sys.exit(code if code>=0 else 128-code)
