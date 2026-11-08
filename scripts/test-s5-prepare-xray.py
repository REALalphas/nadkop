#!/usr/bin/env python3
"""Copy the audited local Xray and loader into S5; never build or modify an SDK."""
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess


def s5_main():
    root = Path(__file__).resolve().parents[1]
    output = root / "build/agent-audit/S5"
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    output.chmod(0o700)
    sdk = root / "build/openwrt-vm/sdk/staging_dir"
    files = [
        (sdk / "target-x86_64_musl/root-x86/usr/bin/xray", "xray", "4f92ba5e12339213108495ce92478cf3460e450a03495e8cd1459b7f7317ca48", 0o700),
        (sdk / "toolchain-x86_64_gcc-14.3.0_musl/lib/libc.so", "ld-musl-x86_64.so.1", "d3c2f0f67861c4e029fe24f5c12b95ec4f6ac0cc1bf20983da4de6bbcb3bb10a", 0o700),
        (sdk / "toolchain-x86_64_gcc-14.3.0_musl/lib/libc.so", "libc.so", "d3c2f0f67861c4e029fe24f5c12b95ec4f6ac0cc1bf20983da4de6bbcb3bb10a", 0o600),
        (sdk / "toolchain-x86_64_gcc-14.3.0_musl/lib/libgcc_s.so.1", "libgcc_s.so.1", "635f40844a04a9147a7ec4aee4ea7b4139819661ab23c5fdde6f0e7d946c0291", 0o600),
    ]
    prepared = []
    for source, name, expected, mode in files:
        data = source.read_bytes()
        actual = hashlib.sha256(data).hexdigest()
        if actual != expected:
            raise SystemExit("Refuse unexpected SDK input checksum: " + str(source))
        temporary = output / (name + ".partial")
        temporary.write_bytes(data)
        temporary.chmod(mode)
        os.replace(temporary, output / name)
        prepared.append({"source": str(source.relative_to(root)), "output": name, "sha256": actual, "bytes": len(data)})
    wrapper = output / "xray-local"
    args = [str(output / "ld-musl-x86_64.so.1"), "--library-path", str(output), str(output / "xray")]
    temporary = output / "xray-local.partial"
    temporary.write_text("#!/bin/sh\nexec " + " ".join(shlex.quote(x) for x in args) + ' "$@"\n')
    temporary.chmod(0o700)
    os.replace(temporary, wrapper)
    result = subprocess.run([str(wrapper), "version"], capture_output=True, text=True, timeout=10, check=True)
    evidence = {"files": prepared, "version": result.stdout, "exit": result.returncode}
    (output / "engine-preparation.json").write_text(json.dumps(evidence, indent=2) + "\n")
    print(result.stdout, end="")


if __name__ == "__main__":
    s5_main()
