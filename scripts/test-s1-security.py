#!/usr/bin/env python3
"""Run only TestS1* on frozen sources, with synthetic loopback/temp fixtures."""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
ART = ROOT / "build/agent-audit/S1"
from audit_common import MANIFEST, load_manifest

def atomic(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".s1-publish-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as f:
            f.write(data)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)

def verify(label):
    manifest = load_manifest()
    hashes, mismatches = {}, []
    for rel, expected in manifest["files"].items():
        p = ROOT / rel
        actual = hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None
        hashes[rel] = actual
        if actual != expected:
            mismatches.append({"path": rel, "expected": expected, "actual": actual})
    result = {"time": dt.datetime.now(dt.timezone.utc).isoformat(),
              "manifest_sha256": hashlib.sha256(MANIFEST.read_bytes()).hexdigest(),
              "files": hashes, "mismatches": mismatches}
    atomic(ART / f"sha256-{label}.json", json.dumps(result, indent=2) + "\n")
    return result

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--race", action="store_true")
    args = parser.parse_args()
    ART.mkdir(parents=True, exist_ok=True)
    tmp = ART / "tmp"
    tmp.mkdir(exist_ok=True)
    tag = "race" if args.race else "normal"
    before = verify(f"{tag}-before")
    if before["mismatches"]:
        print("Frozen source differs; refusing to start tests", file=sys.stderr)
        return 2
    env = os.environ.copy()
    env.update(GOMAXPROCS="2", GOMEMLIMIT="384MiB", GOFLAGS="-mod=readonly",
               GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local",
               TMPDIR=str(tmp), S1_ARTIFACT_DIR=str(ART))
    cmd = ["go", "test", "-p", "1", "-parallel", "1", "-count", "1", "-timeout", "60s", "-json"]
    if args.race:
        cmd.append("-race")
    cmd += ["-run", "^TestS1", "./internal/core", "./internal/router"]
    log = ART / f"go-test-{tag}.jsonl"
    stderr = ART / f"go-test-{tag}.stderr.log"
    start = time.monotonic()
    with log.open("w") as out, stderr.open("w") as err:
        proc = subprocess.Popen(cmd, cwd=ROOT, env=env, stdout=out, stderr=err, start_new_session=True)
        try:
            code = proc.wait(timeout=240)
        except subprocess.TimeoutExpired:
            import signal
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait()
            code = 124
    after = verify(f"{tag}-after")
    counts = {"pass": 0, "fail": 0, "skip": 0}
    top = {"pass": 0, "fail": 0, "skip": 0}
    packages = []
    failures = []
    for line in log.read_text().splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        action, test = event.get("Action"), event.get("Test")
        if action in counts and test:
            counts[action] += 1
            if "/" not in test:
                top[action] += 1
            if action == "fail":
                failures.append({"package": event.get("Package"), "test": test})
        elif action in ("pass", "fail") and not test:
            packages.append({"package": event.get("Package"), "result": action,
                             "elapsed": event.get("Elapsed")})
    summary = {"time": dt.datetime.now(dt.timezone.utc).isoformat(), "command": cmd,
               "exitCode": code, "wallSeconds": round(time.monotonic()-start, 3),
               "topLevelTests": top, "testEventsIncludingSubtests": counts,
               "packages": packages, "failures": failures,
               "sha256CheckedBefore": len(before["files"]),
               "sha256CheckedAfter": len(after["files"]),
               "sha256MismatchesBefore": len(before["mismatches"]),
               "sha256MismatchesAfter": len(after["mismatches"]),
               "log": str(log.relative_to(ROOT)), "stderr": str(stderr.relative_to(ROOT))}
    atomic(ART / f"summary-{tag}.json", json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if stderr.stat().st_size:
        print(stderr.read_text()[-4000:], file=sys.stderr)
    return code if code else (2 if after["mismatches"] else 0)

if __name__ == "__main__":
    raise SystemExit(main())
