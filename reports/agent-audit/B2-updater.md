# B2 — full hourly updater cycle

**Status: closed for the tested scenario.** The installed updater on the
OpenWrt VM fired twice on its real schedule: it successfully applied a
local source change and, on the next firing, reported source corruption
while preserving the working generation and traffic flow. No new confirmed
application defects were found in this check.

## Stand and method

Dedicated OpenWrt 25.12.6 x86_64 VM, kernel 6.12.112, Xray 26.10.21; installed
`nadkop-0.3.0-r4` and `luci-app-nadkop-0.3.0-r8`. The installed
`/usr/bin/nadkop` was tested, SHA256
`853b3b98e59e5f7ad35991da4bb2d5bcce4ea653e169c13e3ef3abb55bddd12e`.
Versions and checksum are also recorded in `build/agent-audit/B2/preflight.log`.
Application code and host/VM system clocks were unchanged.

The new standalone test `scripts/test-hourly-updater-openwrt.sh` runs the
real procd service with `updateIntervalHours=1`. The source is a synthetic
local JSON file; two synthetic SOCKS outbounds and HTTP servers run on
VM loopback. The rule references a saved `nodeRef`. After the initial start,
the server port in the source is changed; before the hourly update the working route
stays unchanged. After the first firing the source is intentionally corrupted.
The test verifies the `update-status.json` content, generation pointer, real
Xray configuration, server ID in the catalogue and rule reference, updater PID and
run identifier, and the actual HTTP response via SOCKS. After the wait begins,
the test does not call `Apply`: the updater itself initiates the update.

Wait time is measured by VM monotonic time via `/proc/uptime`; firing
timestamps come from `update-status.json`. Results require no disclosure of
real subscriptions or HWID; backups remain private on the VM and are deleted
after successful restore verification.

## Results of two real cycles

| Event | Observation |
| --- | --- |
| Start | 2026-11-02 17:05:27 UTC; updater PID 5694; route `b2-alpha`; new source content prepared without manual apply. |
| Pre-update state | At 17:18:56 UTC the source already contained port 21882, while the active generation and `b2-alpha` response stayed unchanged; the status file was still missing (`build/agent-audit/B2/intermediate.log`). |
| First schedule | At 18:05:29 UTC, 3603.52 s after the start request by VM monotonic time, `ok=true` recorded. Generation changed from `generation.4215640238` to `generation.343731796`; `current/xray.json` points to it, and the running Xray uses its configuration. |
| Reference and route stability | Synthetic rule `nodeRef` and catalogue entry ID preserved. HTTP via the new SOCKS outbound returned `b2-beta`; updater PID and process run identifier unchanged. |
| Next schedule under corruption | At 19:06:27 UTC the status changed to `ok=false` with text `Automatic refresh failed; previous generation retained`. 3660 s elapsed between the `updatedAt` of the two statuses, 3657.88 s between their observations. |
| Behavior after failure | `generation.343731796`, the Xray configuration path and bytes, synthetic catalogue bytes, `nodeRef`, and updater PID preserved. HTTP via the same route still returned `b2-beta`. |

Main log: `build/agent-audit/B2/hourly-run.log`, SSH result — `0`. The second
firing was observed after 61 minutes, so the check confirms the real schedule
works with minute granularity, not exact to-the-second precision.

## Stand restore and completion verification

Before modifying the stand, configuration, nodes, servers, HWID,
hosts, firewall configuration, update status, the original generation and other
existing generations, service state, network rules, and policy routes were privately saved. At
completion the runner restored the original files and the original generation without
loading a user subscription. Bytes, presence, and metadata matched for
config/nodes/servers/hwid/hosts/firewall/update-status; static
Nadkop and firewall4 rules and IPv4/IPv6 policy routes matched. Before and after, the service was
enabled and running; on the final independent check Xray, monitor, and nfqws
were running, NFQUEUE 200 was attached, and the updater was absent as before the test.
Test listeners, lock, and temporary directories were removed.

The first TERM cleanup trial **failed** due to a bug in the test
runner itself: on re-precheck, the Zapret files from the saved generation already
belonged to unprivileged nfqws. User files were restored;
the service and rules were restored manually from the private copy. The failed run and
recovery are saved in `build/agent-audit/B2/signal-cleanup.log` and
`build/agent-audit/B2/signal-recovery.log`. Only the auxiliary
runner was fixed: before precheck it temporarily returns root ownership to copies of these
files, after which the stock init assigns the required nfqws owner. The repeated
TERM check finished with the expected code `143`, including recovery verification:
`build/agent-audit/B2/signal-cleanup-retry.log`. Independent final state:
`build/agent-audit/B2/final-vm-state.log`.

## Commands

From the workspace root, with the pinned SSH key and strict host key checking:

```sh
sh -n scripts/test-hourly-updater-openwrt.sh
ssh -i ~/.ssh/nadkop_vm_key \
  -o UserKnownHostsFile=build/openwrt-vm/known_hosts \
  -o StrictHostKeyChecking=yes -o BatchMode=yes \
  -o ConnectTimeout=8 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 \
  root@203.0.113.10 'sh -s -- --exercise-term' \
  < scripts/test-hourly-updater-openwrt.sh
# Expected code of this standalone signal check: 143.
ssh -i ~/.ssh/nadkop_vm_key \
  -o UserKnownHostsFile=build/openwrt-vm/known_hosts \
  -o StrictHostKeyChecking=yes -o BatchMode=yes \
  -o ConnectTimeout=8 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 \
  root@203.0.113.10 'sh -s' \
  < scripts/test-hourly-updater-openwrt.sh
# The main run takes a little over two hours.
```

Actual runs were additionally recorded via `tee`, preserving the SSH
code through `PIPESTATUS`; the first failed run was not deleted.

## Protected files and confirmation limits

Final reconciliation after both cycles: **327 product files, 0 mismatches**;
`build/agent-audit/B2/source-integrity.json`. SHA256 of the unchanged manifest
`frozen-source.json` —
`09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.
Changes are limited to the new test runner and the B2 report/logs themselves.

A local file and its corruption were tested on one designated VM. This is not a check of
an HTTPS source, provider network unavailability, real subscriptions, all configuration types,
or a physical router. HUP/INT/TERM signals are handled by the runner;
only TERM was actually verified separately. Unconditional termination such as SIGKILL or
VM power-off cannot run a shell trap and was not modeled in this run.
