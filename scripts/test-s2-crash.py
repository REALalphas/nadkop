#!/usr/bin/env python3
"""S2-only bounded syscall fault tests, synthetic fixtures, no host RPC/network."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "build/agent-audit/S2"
FIX = OUT / "fixtures"
REF = "srv_22222222222222222222222222222222"
REF_A = "srv_11111111111111111111111111111111"
ENV = dict(os.environ, GOMAXPROCS="1", GODEBUG="asyncpreemptoff=1")
TRACE = "openat,close,write,fsync,renameat,rename,renameat2,symlinkat,unlinkat,mkdirat,flock"


def dump(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    path.chmod(0o600)


def mkcase(name):
    path = FIX / name
    path.mkdir(parents=True, exist_ok=False)
    return path


def node(tag, port):
    return {"tag": tag, "protocol": "socks", "settings": {"servers": [{"address": "127.0.0.1", "port": port}]}}


def catalog_record(ref, tag, available=True):
    return {"id": ref, "source": "provider", "name": tag, "protocol": "socks", "tag": "provider/" + tag,
            "position": 0, "available": available, "fingerprint": "s2-seeded-" + tag, "endpoint": "s2-seeded-" + tag}


def save_fixture(path, race=False):
    (path / "run").mkdir()
    (path / "run/generation.working").mkdir()
    dump(path / "run/generation.working/xray.json", {"synthetic": "previous-runtime"})
    os.symlink("generation.working", path / "run/current")
    old_nodes = [node("old", 19001)]
    new_nodes = [node("new", 19002)]
    cfg = {"sources": [{"name": "local", "file": str(path / "nodes.json")}],
           "rules": [{"domains": ["crash.s2.example"], "action": "proxy", "node": "local/old"}],
           "default": "block", "socksPort": 1080}
    new_cfg = json.loads(json.dumps(cfg))
    new_cfg["rules"][0]["node"] = "local/new"
    records = [catalog_record(REF, "unused", available=False)]
    if race:
        cfg["sources"] = [{"name": "provider", "file": str(path / "provider.json")}]
        cfg["rules"] = [{"domains": ["race.s2.example"], "action": "proxy", "nodeRef": REF_A}]
        new_cfg = json.loads(json.dumps(cfg))
        new_cfg["rules"][0]["nodeRef"] = REF
        records = [catalog_record(REF_A, "a"), catalog_record(REF, "b")]
        dump(path / "provider.json", [node("a", 19101)])
        dump(path / "refresh-nodes.json", [node("a", 19101)])
        new_nodes = old_nodes
    dump(path / "config.json", cfg)
    dump(path / "old-config.json", cfg)
    dump(path / "nodes.json", old_nodes)
    dump(path / "servers.json", {"version": 1, "servers": records})
    c, n = (path / "config.json").read_bytes(), (path / "nodes.json").read_bytes()
    rev = hashlib.sha256(c + b"\0" + n).hexdigest()
    dump(path / "request.json", {"config": new_cfg, "nodes": new_nodes, "revision": rev})
    dump(path / "atomic.json", {"value": "old"})
    (path / "payload.json").write_text('{"value":"new"}')


def command(path, mode, core=False):
    binary = OUT / ("core.test" if core else "router.test")
    test = "TestS2CoreChild" if core else "TestS2Child"
    return [str(binary), "-test.run=^" + test + "$", "-test.v=false"]


def parse_output(output):
    for line in output.splitlines():
        if line.startswith("S2_RESULT "):
            return json.loads(line[len("S2_RESULT "):])
    return None


def run(path, mode, core=False, inject=None, traced=False, suffix=""):
    cmd = command(path, mode, core)
    env = dict(ENV, S2_FIXTURE=str(path), S2_MODE=mode)
    stem = path / (mode + suffix)
    if traced or inject:
        cmd = ["strace", "-f", "-qq", "-y", "-s", "320", "-e", "trace=" + TRACE,
               "-o", str(stem) + ".strace"] + (["-e", "inject=" + inject] if inject else []) + cmd
    started = time.monotonic()
    proc = subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    stem.with_suffix(".stdout").write_text(proc.stdout)
    return {"command": cmd, "exit": proc.returncode, "seconds": round(time.monotonic()-started, 4),
            "response": parse_output(proc.stdout), "stdout": str(stem.with_suffix(".stdout")),
            "trace": str(stem) + ".strace" if traced or inject else None}


def hashes(path):
    files = {}
    for item in sorted(path.rglob("*")):
        if item.is_symlink():
            files[str(item.relative_to(path))] = {"symlink": os.readlink(item)}
        elif item.is_file() and item.suffix not in (".strace", ".stdout") and item.name != "evidence.json":
            data = item.read_bytes()
            files[str(item.relative_to(path))] = {"sha256": hashlib.sha256(data).hexdigest(), "bytes": len(data),
                                                "mode": oct(item.stat().st_mode & 0o777)}
    return files


def save_case(name, inject=None, race=False):
    path = mkcase(name)
    save_fixture(path, race=race)
    record = {"name": name, "fixture": str(path), "before": hashes(path)}
    record["action"] = run(path, "save", inject=inject, traced=True)
    record["afterFault"] = hashes(path)
    record["inspection"] = run(path, "inspect")["response"]
    record["restart"] = run(path, "restart")
    record["afterRestart"] = hashes(path)
    record["inspectionAfterRestart"] = run(path, "inspect", suffix="-after-restart")["response"]
    dump(path / "evidence.json", record)
    return record


def atomic_case(name, inject):
    path=mkcase(name)
    save_fixture(path)
    before=hashes(path)
    action=run(path,"atomic",inject=inject,traced=True)
    after=hashes(path)
    assert action["response"]["error"], (name, "injection failed")
    assert json.loads((path/"atomic.json").read_text()) in ({"value":"old"},{"value":"new"})
    record={"name":name,"fixture":str(path),"before":before,"action":action,"afterFault":after,
            "unchanged":before["atomic.json"]==after["atomic.json"],"jsonValid":True}
    dump(path/"evidence.json",record)
    return record


def geo_case(name, inject=None):
    path=mkcase(name)
    seed=run(path,"geoseed",core=True)
    assert seed["response"]["error"]=="", seed
    before=hashes(path)
    action=run(path,"geoinstall",core=True,inject=inject,traced=True)
    after=hashes(path)
    restart=run(path,"geoinspect",core=True)
    record={"name":name,"fixture":str(path),"before":before,"action":action,"afterFault":after,"restart":restart}
    dump(path/"evidence.json",record)
    return record


def catalogue_case(name, inject=None):
    path=mkcase(name)
    save_fixture(path,race=True)
    before=hashes(path)
    action=run(path,"refresh-old",inject=inject,traced=True)
    record={"name":name,"fixture":str(path),"before":before,"action":action,"afterFault":hashes(path),
            "inspection":run(path,"inspect")["response"],"restart":run(path,"restart")}
    record["afterRestart"]=hashes(path)
    dump(path/"evidence.json",record)
    return record


def device_case(name, inject=None, existing=False):
    path=mkcase(name)
    # This is a synthetic local ID; no real host HWID is ever read.
    (path/"hwid").write_text("S2-synthetic-fixed-device-0001\n" if existing else "")
    before=hashes(path)
    action=run(path,"device",core=True,inject=inject,traced=True)
    after=hashes(path)
    restart=run(path,"device",core=True,suffix="-restart")
    record={"name":name,"fixture":str(path),"before":before,"action":action,"afterFault":after,
            "restart":restart,"afterRestart":hashes(path)}
    dump(path/"evidence.json",record)
    return record


def generation_case(name, inject=None, existing=False):
    path=mkcase(name)
    save_fixture(path)
    # An empty source set avoids external I/O and catalogue writes before the
    # target generation, making the target rename/write counters unambiguous.
    dump(path/"config.json",{"sources":[],"rules":[],"default":"block","socksPort":1080})
    dump(path/"servers.json",{"version":1,"servers":[]})
    (path/"run/generation.old").mkdir()
    dump(path/"run/generation.old/xray.json",{"synthetic":"old"})
    (path/"run/current").unlink()
    os.symlink("generation.old",path/"run/current")
    output=path/"run/generation.new"
    if existing:
        output.mkdir()
        (output/"keep").write_text("S2 existing generation must not change\n")
    before=hashes(path)
    cmd=[str(OUT/"nadkop"),"generate","-config",str(path/"config.json"),"-output",str(output)]
    cmd=["strace","-f","-qq","-y","-s","320","-e","trace="+TRACE,"-o",str(path/"generate.strace")]+(["-e","inject="+inject] if inject else [])+cmd
    p=subprocess.run(cmd,env=ENV,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (path/"generate.stdout").write_text(p.stdout)
    after=hashes(path)
    retry=path/"run/generation.retry"
    retry_cmd=[str(OUT/"nadkop"),"generate","-config",str(path/"config.json"),"-output",str(retry)]
    r=subprocess.run(retry_cmd,env=ENV,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (path/"retry.stdout").write_text(r.stdout)
    files=sorted(x.name for x in output.iterdir()) if output.is_dir() else []
    record={"name":name,"fixture":str(path),"before":before,"action":{"command":cmd,"exit":p.returncode,"stdout":p.stdout},
            "afterFault":after,"published":output.is_dir(),"publishedFiles":files,
            "retry":{"command":retry_cmd,"exit":r.returncode,"files":sorted(x.name for x in retry.iterdir()) if retry.is_dir() else []}}
    dump(path/"evidence.json",record)
    return record


def race_case():
    path=mkcase("save-vs-stale-generator")
    save_fixture(path,race=True)
    before=hashes(path)
    cmd=["strace","-f","-qq","-y","-s","320","-e","trace="+TRACE,"-o",str(path/"generator.strace"),
         "-e","inject=flock:delay_enter=2s:when=1",str(OUT/"nadkop"),"generate","-config",str(path/"config.json"),
         "-output",str(path/"run/generation.stale")]
    proc=subprocess.Popen(cmd,env=ENV,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    try:
        deadline=time.monotonic()+6
        while time.monotonic()<deadline:
            trace=(path/"generator.strace").read_text() if (path/"generator.strace").exists() else ""
            if 'servers.json.lock' in trace:
                break
            if proc.poll() is not None: raise RuntimeError("generator ended before observation")
            time.sleep(0.005)
        else: raise RuntimeError("did not reach catalogue lock")
        at_lock=hashes(path)
        save=run(path,"save",traced=True)
        after_save=hashes(path)
        stdout,_=proc.communicate(timeout=10)
    finally:
        if proc.poll() is None:
            proc.kill(); proc.wait(timeout=5)
    (path/"generator.stdout").write_text(stdout)
    record={"name":"save-vs-stale-generator","fixture":str(path),"before":before,"generatorPausedAtLock":at_lock,
            "save":save,"afterSave":after_save,"action":{"command":cmd,"exit":proc.returncode,"stdout":stdout},
            "afterFault":hashes(path),"inspection":run(path,"inspect")["response"],"restart":run(path,"restart")}
    record["afterRestart"]=hashes(path)
    dump(path/"provider.json",[node("a",19101),node("b",19102)])
    record["afterProviderReturns"]=run(path,"restart",suffix="-source-returned")
    record["inspectionAfterProviderReturns"]=run(path,"inspect",suffix="-source-returned")["response"]
    record["hashesAfterProviderReturns"]=hashes(path)
    dump(path/"evidence.json",record)
    return record


def main():
    global FIX
    parser=argparse.ArgumentParser()
    parser.add_argument("--suite",choices=["baseline","faults","race","all"],default="all")
    parser.add_argument("--run-id",default="")
    args=parser.parse_args()
    if args.run_id:
        if not re.fullmatch(r"[A-Za-z0-9_-]+",args.run_id): raise ValueError("invalid run id")
        FIX=FIX/args.run_id
    FIX.mkdir(parents=True,exist_ok=True)
    records=[]
    if args.suite in ("baseline","all"):
        records += [save_case("save-baseline"),geo_case("geo-baseline"),device_case("device-baseline"),generation_case("generation-baseline")]
    if args.suite in ("faults","all"):
        for suffix,inj in [
            ("write-eio","write:error=EIO:when=1"),
            ("file-fsync-eio","fsync:error=EIO:when=1"),
            ("rename-eperm","renameat:error=EPERM:when=1"),
            ("directory-fsync-eio","fsync:error=EIO:when=2")]:
            records.append(atomic_case("atomic-"+suffix,inj))
        for name,inj in [
            ("save-nodes-write-eio","write:error=EIO:when=1"),
            ("save-nodes-fsync-eio","fsync:error=EIO:when=1"),
            ("save-nodes-dir-fsync-eio","fsync:error=EIO:when=2"),
            ("save-config-write-eio","write:error=EIO:when=2"),
            ("save-config-fsync-eio","fsync:error=EIO:when=3"),
            ("save-rollback-fsync-eio","fsync:error=EIO:when=3+"),
            ("save-config-rename-eperm","renameat:error=EPERM:when=2"),
            ("save-config-dir-fsync-eio","fsync:error=EIO:when=4"),
            ("save-catalogue-write-eio","write:error=EIO:when=3"),
            ("save-catalogue-fsync-eio","fsync:error=EIO:when=5"),
            ("save-catalogue-rename-eperm","renameat:error=EPERM:when=3"),
            ("save-catalogue-dir-fsync-eio","fsync:error=EIO:when=6"),
            ("save-kill-before-config-rename","renameat:signal=SIGKILL:when=2"),
            ("save-kill-before-catalogue-rename","renameat:signal=SIGKILL:when=3")]:
            records.append(save_case(name,inj))
        for name,inj in [
            ("catalogue-write-eio","write:error=EIO:when=1"),
            ("catalogue-file-fsync-eio","fsync:error=EIO:when=1"),
            ("catalogue-rename-eperm","renameat:error=EPERM:when=1"),
            ("catalogue-dir-fsync-eio","fsync:error=EIO:when=2"),
            ("catalogue-kill-before-rename","renameat:signal=SIGKILL:when=1")]:
            records.append(catalogue_case(name,inj))
        for name,inj in [
            ("geo-first-write-eio","write:error=EIO:when=1"),
            ("geo-second-write-eio","write:error=EIO:when=2"),
            ("geo-kill-between-files","write:signal=SIGKILL:when=2"),
            ("geo-current-rename-eperm","renameat:error=EPERM:when=1"),
            ("geo-kill-before-current","renameat:signal=SIGKILL:when=1"),
            ("geo-kill-after-current","write:signal=SIGKILL:when=3"),
            ("geo-fsync-observation","fsync:error=EIO:when=1")]:
            records.append(geo_case(name,inj))
        for name,inj in [
            ("device-write-eio","write:error=EIO:when=1"),
            ("device-file-fsync-eio","fsync:error=EIO:when=1"),
            ("device-rename-eperm","renameat:error=EPERM:when=1"),
            ("device-dir-fsync-eio","fsync:error=EIO:when=2"),
            ("device-kill-before-rename","renameat:signal=SIGKILL:when=1")]:
            records.append(device_case(name,inj))
        records.append(device_case("device-existing-no-storage-sync","fsync:error=EIO:when=1",existing=True))
        for name,inj in [
            ("generation-first-write-eio","write:error=EIO:when=1"),
            ("generation-fifth-write-eio","write:error=EIO:when=5"),
            ("generation-kill-between-files","write:signal=SIGKILL:when=5"),
            ("generation-publish-rename-eperm","renameat:error=EPERM:when=1"),
            ("generation-kill-before-publish","renameat:signal=SIGKILL:when=1"),
            ("generation-fsync-observation","fsync:error=EIO:when=1")]:
            records.append(generation_case(name,inj))
        records.append(generation_case("generation-existing-output",existing=True))
    if args.suite in ("race","all"):
        records.append(race_case())
    summary=OUT/("syscall-"+args.suite+("-"+args.run_id if args.run_id else "")+".json")
    dump(summary,records)
    for r in records:
        a=r.get("action",{})
        inspect=(r.get("inspection") or {}).get("result") or {}
        print(r["name"],"exit",a.get("exit"),"error",(a.get("response") or {}).get("error"),
              "missingLocalTags",inspect.get("missingLocalTags"),"missingNodeRefs",inspect.get("missingNodeRefs"),
              "retry",r.get("restart",r.get("retry",{})).get("exit"))
    print("Evidence:",summary)


if __name__=="__main__":
    main()
