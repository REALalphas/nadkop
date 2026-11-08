#!/usr/bin/env python3
"""Prepare a disposable, expanded OpenWrt ext4 VM image without root/mounts."""
import argparse
import json
import pathlib
import struct
import subprocess


def run(*args):
    return subprocess.run(args, check=True)


def fsck(tool, image):
    result = subprocess.run([str(tool), '-fy', str(image)])
    if result.returncode not in (0, 1):
        raise RuntimeError(f'e2fsck failed: {result.returncode}')


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('source', type=pathlib.Path)
parser.add_argument('destination', type=pathlib.Path)
parser.add_argument('--tools', type=pathlib.Path, required=True, help='directory containing e2fsck/resize2fs/debugfs')
parser.add_argument('--work', type=pathlib.Path, required=True)
parser.add_argument('--size-gib', type=int, default=4)
args = parser.parse_args()
if args.destination.exists():
    parser.error('destination already exists; refusing to overwrite')
p = args.work.resolve()
p.mkdir(parents=True, exist_ok=True)
root = p / 'root.ext4'
table = json.loads(subprocess.check_output(['sfdisk', '--json', str(args.source)]))['partitiontable']
assert table['label'] == 'dos' and table['sectorsize'] == 512
part = table['partitions'][1]
offset = part['start'] * 512
size = args.size_gib * 1024**3
assert size > offset + part['size'] * 512
with args.source.open('rb') as src, root.open('wb') as dst:
    src.seek(offset)
    dst.write(src.read(part['size'] * 512))
    dst.truncate(size - offset)
fsck(args.tools / 'e2fsck', root)
run(str(args.tools / 'resize2fs'), str(root))
key = p / 'id_ed25519'
if not key.exists():
    run('ssh-keygen', '-t', 'ed25519', '-N', '', '-C', 'nadkop-openwrt-vm', '-f', str(key))
configs = {
    '/etc/config/network': """config interface 'loopback'
 option device 'lo'
 option proto 'static'
 option ipaddr '127.0.0.1'
 option netmask '255.0.0.0'
config interface 'lan'
 option device 'eth0'
 option proto 'dhcp'
 option hostname 'nadkop-openwrt'
config interface 'mgmt'
 option device 'eth1'
 option proto 'static'
 option ipaddr '192.0.2.10'
 option netmask '255.255.255.0'
""",
    '/etc/config/dhcp': """config dnsmasq
 option domainneeded '1'
 option boguspriv '1'
 option localservice '1'
config dhcp 'lan'
 option interface 'lan'
 option ignore '1'
config dhcp 'mgmt'
 option interface 'mgmt'
 option ignore '1'
""",
    '/etc/config/firewall': """config defaults
 option input 'REJECT'
 option output 'ACCEPT'
 option forward 'REJECT'
 option synflood_protect '1'
 option flow_offloading '0'
 option flow_offloading_hw '0'
config zone
 option name 'lan'
 list network 'lan'
 list network 'mgmt'
 option input 'ACCEPT'
 option output 'ACCEPT'
 option forward 'REJECT'
""",
    '/etc/config/dropbear': """config dropbear
 option PasswordAuth 'off'
 option RootPasswordAuth 'off'
 option Port '22'
""",
    '/etc/dropbear/authorized_keys': key.with_suffix('.pub').read_text(),
}
shadow = subprocess.check_output([str(args.tools / 'debugfs'), '-R', 'cat /etc/shadow', str(root)], stderr=subprocess.DEVNULL).decode()
configs['/etc/shadow'] = '\n'.join('root:!:' + line.split(':', 2)[2] if line.startswith('root:') else line for line in shadow.splitlines()) + '\n'
commands = []
for i, (dest, data) in enumerate(configs.items()):
    src = p / f'config-{i}'
    src.write_text(data)
    commands += [f'rm {dest}', f'write {src} {dest}', f'set_inode_field {dest} mode 0100600']
cmds = p / 'debugfs.commands'
cmds.write_text('\n'.join(commands) + '\n')
run(str(args.tools / 'debugfs'), '-w', '-f', str(cmds), str(root))
fsck(args.tools / 'e2fsck', root)
args.destination.parent.mkdir(parents=True, exist_ok=True)
with args.source.open('rb') as src, args.destination.open('xb') as dst:
    dst.write(src.read(offset))
    dst.truncate(size)
run('dd', f'if={root}', f'of={args.destination}', 'bs=512', f'seek={part["start"]}', 'conv=notrunc,sparse', 'status=none')
with args.destination.open('r+b') as dst:
    dst.seek(446 + 16 + 12)
    dst.write(struct.pack('<I', (size - offset) // 512))
print(f'Prepared {args.destination} ({args.size_gib} GiB)')
