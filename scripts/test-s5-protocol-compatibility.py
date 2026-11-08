#!/usr/bin/env python3
"""Run only S5 tests, retain private evidence and bound our child processes."""
import argparse
import datetime
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import time


def s5_limits():
    os.nice(10)
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_NOFILE, (512, 512))
    resource.setrlimit(resource.RLIMIT_CPU, (420, 420))
    resource.setrlimit(resource.RLIMIT_FSIZE, (32 << 20, 32 << 20))


def s5_main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--run", default="TestS5Compatibility")
    parser.add_argument("--label", default="matrix")
    parser.add_argument("--timeout", type=int, default=330)
    args = parser.parse_args()
    if not args.run.startswith("TestS5"):
        raise SystemExit("Only TestS5* is allowed")
    root = Path(__file__).resolve().parents[1]
    private = root / "build/agent-audit/S5"
    private.mkdir(mode=0o700, parents=True, exist_ok=True)
    private.chmod(0o700)
    run_dir = private / (args.label + "-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ"))
    run_dir.mkdir(mode=0o700)
    env = os.environ.copy()
    env.update(S5_XRAY=str(private / "xray-local"), S5_RUN_DIR=str(run_dir), GOMAXPROCS="2", GOMEMLIMIT="256MiB")
    cmd = ["go", "test", "-p", "1", "./internal/core", "-run", "^" + args.run + "$", "-count=1", "-timeout", str(args.timeout) + "s", "-json"]
    start = time.monotonic()
    timeout = False
    with (run_dir / "go-test.jsonl").open("w") as output:
        proc = subprocess.Popen(cmd, cwd=root, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True, preexec_fn=s5_limits)
        try:
            code = proc.wait(timeout=args.timeout + 15)
        except subprocess.TimeoutExpired:
            timeout = True
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                code = proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                code = proc.wait()
        finally:
            # Only our session: also clean up descendants after a Go test panic.
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
    meta = {"command": cmd, "cwd": str(root), "run_dir": str(run_dir), "exit": code, "timeout": timeout, "elapsed_seconds": round(time.monotonic() - start, 3)}
    (run_dir / "runner.json").write_text(json.dumps(meta, indent=2) + "\n")
    print(json.dumps(meta, ensure_ascii=False))
    raise SystemExit(code if code >= 0 else 1)


if __name__ == "__main__":
    s5_main()
