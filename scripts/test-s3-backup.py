#!/usr/bin/env python3
"""Run only S3 synthetic backup audits with bounded, private build resources.

No package build, host config, VM, HTTP request or global test runner is used.
The detailed go test JSON is evidence: failing safety assertions are intentional
findings and are never suppressed by this runner.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import resource
import subprocess
import sys
import tempfile
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--run", default="^TestS3")
    parser.add_argument("--label", default="s3-main")
    args = parser.parse_args()
    if not args.run.startswith("^TestS3"):
        parser.error("the run pattern must select only TestS3 tests")
    if not args.label.replace("-", "").replace("_", "").isalnum():
        parser.error("label must contain only letters, digits, hyphens or underscores")
    root = Path(__file__).resolve().parents[1]
    artifact = root / "build/agent-audit/S3"
    artifact.mkdir(parents=True, exist_ok=True)
    os.umask(0o077)
    env = os.environ.copy()
    env.update({"GOMAXPROCS": "2", "GOFLAGS": "-mod=readonly", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off", "S3_ARTIFACT_DIR": str(artifact)})
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (128 << 20, 128 << 20))
    started = time.monotonic()
    results = {"utc_started": datetime.datetime.now(datetime.timezone.utc).isoformat(), "label": args.label, "commands": [], "scope": "synthetic local filesystem, core APIs, validate CLI; not a sysupgrade or network exchange"}
    with tempfile.TemporaryDirectory(prefix="s3-go-", dir=artifact) as private:
        private = Path(private)
        env["GOCACHE"] = str(private / "cache")
        env["GOTMPDIR"] = str(private / "tmp")
        Path(env["GOTMPDIR"]).mkdir()
        binary = private / "nadkop-s3"
        env["S3_NADKOP_BIN"] = str(binary)
        build = ["go", "build", "-p", "1", "-o", str(binary), "./cmd/nadkop"]
        results["commands"].append(build)
        with (artifact / f"{args.label}-build.log").open("wb") as log:
            subprocess.run(build, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=180, check=True)
        command = ["go", "test", "-p", "1", "-vet=off", "./internal/core", "-run", args.run, "-count=1", "-parallel=1", "-timeout", "90s", "-json"]
        results["commands"].append(command)
        log_path = artifact / f"{args.label}-go-test.jsonl"
        with log_path.open("wb") as log:
            run = subprocess.run(command, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=240)
        results["exit_code"] = run.returncode
        tests = {}
        output = []
        for line in log_path.read_text().splitlines():
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                output.append(line)
                continue
            if "Output" in event:
                output.append(event["Output"].rstrip("\n"))
            if event.get("Test") and event["Action"] in ("pass", "fail", "skip"):
                tests[event["Test"]] = {"result": event["Action"], "elapsed_s": event.get("Elapsed")}
        (artifact / f"{args.label}-go-test.log").write_text("\n".join(output) + "\n")
        results["tests"] = tests
        results["counts_all_test_events"] = {status: sum(t["result"] == status for t in tests.values()) for status in ("pass", "fail", "skip")}
        results["counts_top_level"] = {status: sum("/" not in name and info["result"] == status for name, info in tests.items()) for status in ("pass", "fail", "skip")}
        results["test_source_sha256"] = hashlib.sha256((root / "internal/core/s3_backup_test.go").read_bytes()).hexdigest()
        results["binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
        results["elapsed_s"] = round(time.monotonic() - started, 3)
        results["rusage_children"] = {"max_rss_kib": resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss, "user_s": resource.getrusage(resource.RUSAGE_CHILDREN).ru_utime, "system_s": resource.getrusage(resource.RUSAGE_CHILDREN).ru_stime}
        summary = artifact / f"{args.label}-summary.json"
        pending = summary.with_suffix(".json.tmp")
        pending.write_text(json.dumps(results, indent=2, ensure_ascii=False) + "\n")
        pending.replace(summary)
        print(json.dumps({k: v for k, v in results.items() if k != "tests"}, ensure_ascii=False, indent=2))
    return run.returncode


if __name__ == "__main__":
    sys.exit(main())
