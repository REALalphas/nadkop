# Current outcome: core 0.3.0-r4 / LuCI 0.3.0-r8

**Full readiness is not confirmed: 10 application defects are open (4 high
and 6 medium severity), E05 in the builder, and a separate accessibility note S6-A01.**
All nine delegated checks are complete and accepted on evidence.
B2 confirmed the real hourly updater: refresh of the local source and
failure on a corrupted source while keeping the working route. SHA256 of all
327 application files and six packages matched. The proposed patches were not applied.
[Current blockers](blockers.md), [errors](errors.md),
[agent summary](agent-audit/summary.md).

The results below refer to the main rerun before the additional
S1–S6. New checks were run in a targeted manner and were not merged into a single
all-green run; old logs are preserved without rewriting.

- Local Go/race: 207 PASS, 0 FAIL, 4 integrations skipped without Xray on the host;
  in the VM all of them ran. Go on the VM: core 134 PASS, router 69 PASS, CLI 12 PASS.
- Chromium, go vet, fuzz, APK/IPK builds, RPC/ACL and server switching passed.
- LAN without Zapret: 35/35; LAN with Zapret: 36/36; stock local DNS: 35/35;
  real-NFQUEUE check with fake+badseq: 39/39; updater lifecycle: 6/6.
- ShellCheck still reports nine warnings. B1 closed their analysis: the
  BusyBox/rc.common profile passes with seven targeted exclusions; semantics checks
  54/54 and profile checks 8/8. The first stand failures and bare fake are preserved.
- VM settings, nodes, catalogue, HWID, hosts and firewall were restored by SHA256.
  Core r4, LuCI r8, Xray, nfqws and monitor are running, autostart preserved.

Full machine-readable run outcomes (`runs/`, `results.json`) are local
stand artifacts and are not published; the summary above is the canonical outcome.

The verified packages are built from sources (`scripts/build-openwrt.sh`).
APK was verified on OpenWrt 25.12.6 x86_64;
IPK was additionally installed and verified on a clean OpenWrt 24.10.8 VM: install,
upgrade and cold boot are closed by check B3. [Report](agent-audit/B3-install-boot.md). A physical router, all external
VPNs and DPI effectiveness at a specific provider were not verified. Real hourly
cycles of the local updater were additionally confirmed by B2; the schedule under an
HTTPS-provider outage was not tested.


B3 addendum: on 24.10.8 and 25.12.6, cold boot with active TPROXY yielded
19/19 on each release; the additional NFQUEUE check — 39/39. The upgrade
preserved four user files byte-for-byte and the original service states.
API/FakeDNS/TPROXY compatibility was confirmed including on Xray 25.1.30 from feed 24.10.
Historical run logs are local stand artifacts;
new evidence does not rewrite them.

B1 addendum: the lead agent read the report and logs, reran the
ShellCheck profile and reproduced E05 with both SDK parsers. The original strict
ShellCheck command did not become successful. The proposed patch for a clear early rejection
on paths with spaces was not applied; full support for such paths is unconfirmed.
Previously built packages did not change. [Report B1](agent-audit/B1-shellcheck.md),
[open criteria and E05](blockers.md).

S5 addendum: after resumption the final report was published, accepted by the lead
agent on tests and original logs. On local Xray **26.3.27**,
81 scenarios ran: 44 rows with HTTP and UDP, additional probes/rotations give
50 successful HTTP exchanges and 44 UDP responses in total. The remaining rows check parameters
and expected rejections; 81 rows do not mean 81 successful connections. No
Nadkop defects were found in this volume. The original failed REALITY setup and the provider
limit are preserved in history. S5 did not test other Xray versions or external VPNs.
[Report S5](agent-audit/S5-protocol-compatibility.md),
`lead-agent acceptance` (local stand artifact).

S3 addendum: a full synthetic backup restored 3/3 server IDs,
references/pins, HWID, disabled fields and geodata generations. The new checks
yielded **10 PASS / 6 FAIL / 0 SKIP at the top level, exit 1**. The two confirmed
defects S3-F01/S3-F02 remain open; six FAILs do not equal six separate
bugs. Mode/UID on a deliberately unsafe restore and the missing restore marker
for HWID are tracked separately. S3 did not test real sysupgrade, boot or network.
None of the proposed patches were applied.
[Report S3](agent-audit/S3-backup-restore.md), [error registry](errors.md),
`lead-agent acceptance` (local stand artifact).

S2 addendum: evidence accepted for 48 syscall scenarios, 12 init/network
scenarios and a real ENOSPC in a private 64 KiB tmpfs. The narrow race tests
passed, but the 227 passing assertions include defect confirmation and do not yield
a clean acceptance result. D-S2-01 (bound-ID loss on CLI/RPC,
high), D-S2-02 (stop with corrupted runtime leaves policies, medium) and
D-S2-03 (incomplete network rollback after apply/current failure, high) are open.
Network/procd/Xray used mocks; real traffic and power loss were not tested.
Multi-file inconsistency after fsync/SIGKILL is tracked separately as
the F-S2-01 guarantee boundary. Fix proposals were not applied.
[Report S2](agent-audit/S2-crash-consistency.md),
`lead-agent acceptance` (local stand artifact).

B2 addendum: on the OpenWrt 25.12.6 VM the installed updater, with clock unchanged
and code untouched, applied a new local source endpoint after 3603.52 s. The generation and
HTTP/SOCKS route changed alpha → beta; nodeRef and the updater process were preserved.
At the next firing (3660 s between statuses) the corrupted source gave
ok=false, preserving the generation/catalogue/reference/process and the working beta route.
Original files, rules and VM state were restored; the lead agent separately
confirmed enabled/running, Xray/monitor/nfqws and NFQUEUE 200 over read-only SSH.
The first TERM cleanup failure is preserved as a runner error, only the test was fixed;
the rerun gave the expected exit 143 and a successful recovery. B2 is closed within
this scenario, with no conclusion about timed HTTPS/provider outage or a physical router.
[Report B2](agent-audit/B2-updater.md),
`lead-agent acceptance` (local stand artifact).

S1: two P2 write-method defects. Addsource admits URL/Title after which
the stored config is unreadable; save may expand a <4 MiB input via formatting
into config/nodes above its own limit. 15 top-level/46 event PASSes
in normal and race include successful reproduction of these defects. One helper
was skipped directly and run by the parent five times. Real rpcd authorization was not
tested. [Report S1](agent-audit/S1-security.md).

S4: 132 measurements / 250 iterations; 20 scenarios passed, 1 revealed S4-D01,
1 stopped by the RSS guard, 3 skipped. A valid 3 190 785-byte subscription
publishes a 7 818 180-byte catalogue, which read/sync then reject. This is a high
defect. The 745.77 MiB stop and the skips above are measurement boundaries, not a separate
bug or a router performance estimate. [Report S4](agent-audit/S4-performance.md).

S6: **84 scenarios, 73 PASS / 11 FAIL**. Two P2 causes — new DOM controls
bypass busy/dirty, a nonzero ubus code becomes an empty successful response.
S6-A01 is a separate P3 note on ArrowRight navigation of ARIA tabs, not a release
blocker. The real SDK rpc.js was used with request/backend mocks; real
rpcd/VM/restart was not executed. [Report S6](agent-audit/S6-luci-resilience.md).
