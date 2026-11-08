#!/usr/bin/env python3
"""Recheck the frozen merged version and restore the current VM after every suite."""
import argparse,hashlib,json,os,pathlib,subprocess,sys,time
from audit_common import REPORT, load_manifest, require_geo, require_ui
root=pathlib.Path(__file__).resolve().parent.parent
report=REPORT
manifest=load_manifest()
# Key/known_hosts paths match what prepare-openwrt-vm.py provisions into build/
# (gitignored lab stand). Management IP is the TEST-NET placeholder from docs.
ssh=['ssh','-i',str(root/'build/openwrt-vm/id_ed25519'),'-o','UserKnownHostsFile='+str(root/'build/openwrt-vm/known_hosts'),'-o','StrictHostKeyChecking=yes','-o','ConnectTimeout=5','root@192.0.2.10']
suites={
 'vm-core':'NADKOP_GEO_TEST_FILES=/etc/nadkop/geodata/current /tmp/core.test -test.v -test.timeout=120s',
 'vm-main':'/tmp/main.test -test.v -test.timeout=60s',
 'vm-router':'NADKOP_GEO_TEST_FILES=/etc/nadkop/geodata/current /tmp/router.test -test.v -test.timeout=120s',
 'vm-smoke':'sh /tmp/test-openwrt.sh',
 'vm-rpc':'sh /tmp/test-openwrt-rpc.sh',
 'vm-selection':'sh /tmp/test-selection.sh',
 'vm-procd':'sh /tmp/test-procd-selection.sh',
 'vm-transparent':'sh /tmp/test-transparent-openwrt.sh lan',
 'vm-transparent-zapret':'sh /tmp/test-transparent-openwrt.sh zapret',
 'vm-lifecycle':'sh /tmp/test-lifecycle-openwrt.sh',
 'vm-default-dns':'sh /tmp/test-default-dns-openwrt.sh',
 'vm-default-dns-configured':'sh /tmp/test-default-dns-configured-openwrt.sh',
 'vm-zapret-isolation':'sh /tmp/test-zapret-isolation-openwrt.sh',
 'vm-zapret-badseq':'sh /tmp/test-zapret-badseq-openwrt.sh',
}
parser=argparse.ArgumentParser();parser.add_argument('--suite',choices=list(suites));args=parser.parse_args()
if args.suite:suites={args.suite:suites[args.suite]}
(report/'logs').mkdir(parents=True,exist_ok=True)
results=[]
for name,command in suites.items():
    changed=[p for p,h in manifest['files'].items() if not (root/p).is_file() or hashlib.sha256((root/p).read_bytes()).hexdigest()!=h]
    if changed:raise SystemExit('Frozen source changed: '+', '.join(changed))
    start=time.time();print('START '+name,flush=True)
    wrapped=(
      'test -f /tmp/nadkop-resume-backup/before.sha256 || exit 91; '
      '/etc/init.d/nadkop stop >/dev/null 2>&1; '+command+'; result=$?; '
      '/etc/init.d/nadkop stop >/dev/null 2>&1; '
      'for file in config.json nodes.json servers.json hwid; do cp /tmp/nadkop-resume-backup/$file /etc/nadkop/$file; done; '
      'cp /tmp/nadkop-resume-backup/hosts /etc/hosts; killall -HUP dnsmasq 2>/dev/null || true; '
      'if [ -f /tmp/nadkop-resume-backup/was-running ]; then /etc/init.d/nadkop start; restore=$?; else restore=0; fi; '
      'cp /tmp/nadkop-resume-backup/servers.json /etc/nadkop/servers.json; '
      'if [ "$restore" != 0 ]; then echo "RESTORE FAILED"; exit 91; fi; exit "$result"')
    with (report/'logs'/f'{name}.log').open('w') as out:
        try:run=subprocess.run(ssh+[wrapped],stdout=out,stderr=subprocess.STDOUT,timeout=240);code=run.returncode
        except subprocess.TimeoutExpired:code=124;out.write('\nHOST TIMEOUT; inspect guest cleanup\n')
    changed=[p for p,h in manifest['files'].items() if hashlib.sha256((root/p).read_bytes()).hexdigest()!=h]
    record={'suite':name,'command':command,'exitCode':code,'seconds':round(time.time()-start,2),'sourceUnchanged':not changed,'changed':changed}
    (report/'logs'/f'{name}.json').write_text(json.dumps(record,indent=2)+'\n')
    results.append(record);print(json.dumps(record),flush=True)
    if code in (91,124):break
(report/'logs/vm-results.json').write_text(json.dumps(results,indent=2)+'\n')
sys.exit(1 if any(r['exitCode'] for r in results) else 0)
