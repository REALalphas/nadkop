#!/usr/bin/env python3
"""Identify compatible SDK caches without invalidating on package version bumps."""
import hashlib
import os
from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parent.parent


def cache_key(sdk_url, host, root=ROOT, policy_root=None):
    policy_root = policy_root or root
    parts = ['nadkop-sdk-v1', sdk_url, host]
    for name in ('.github/workflows/openwrt.yml', 'scripts/build-openwrt.sh',
                 'scripts/openwrt-cache-key.py'):
        parts.extend((name, (policy_root / name).read_text()))
    # Recipe changes can alter dependencies or build settings: require a new
    # baseline. Only version bumps and application sources reuse the old one.
    for recipe in sorted((root / 'openwrt').glob('*/Makefile')):
        text = re.sub(r'^PKG_(?:VERSION|RELEASE):=.*$', '', recipe.read_text(), flags=re.M)
        parts.extend((str(recipe.relative_to(root)), text))
    digest = hashlib.sha256('\0'.join(parts).encode()).hexdigest()
    return 'nadkop-sdk-v1-' + digest


if __name__ == '__main__':
    source = Path(os.environ.get('NADKOP_SOURCE_ROOT', ROOT))
    print(cache_key(sys.argv[1], os.environ.get('ImageOS', 'ubuntu24') + '-x86_64',
                    root=source, policy_root=ROOT))
