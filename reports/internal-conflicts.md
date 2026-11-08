# Current state of system conflicts

Core 0.3.0-r4 / LuCI 0.3.0-r8 passed rerun checks without application changes
after the freeze. End-to-end LAN/DNS/IPv6/Zapret scenarios and service preservation on
faulty apply are confirmed in the OpenWrt VM.

The full C01–C16 registry, evidence, limitations and interaction results for
disablable rule conditions are in the
`current-run registry` (local stand log).
The previous registry is preserved in the `archive` (local stand log).


B3 addendum: criterion C13 (clean install/upgrade/service state) was
confirmed in dedicated OpenWrt 24.10.8 and 25.12.6 VMs. TPROXY/DNS/IPv6 cold boot
and real Xray 25.1.30 were also verified. Evidence:
[report B3](agent-audit/B3-install-boot.md). The application was not fixed.

B1 addendum: the ShellCheck dialect conflict with BusyBox ash and the external
`rc.common` API is analyzed in [report B1](agent-audit/B1-shellcheck.md). The correct
dialect and seven verified targeted exclusions yield a passing profile;
the original command keeps nine warnings and exit 1.

A separate E05 conflict is open between the local feed written by the Nadkop builder
and space-splitting of paths in OpenWrt SDK 24.10.8/25.12.6. Building from a path
with spaces is not reliably supported; fixing one feed does not remove
the `$(CURDIR)` split in `Build/Prepare`. The defect was reproduced independently,
the early-rejection proposal is preserved as a separate unapplied patch.
Details and commands are in the [error registry](errors.md).

I01 history: a provider limit interrupted the first S5 run. After resumption
the task was completed, and the lead agent accepted the final report. S5 is closed for Xray
26.3.27; this is not a check of the whole matrix of other versions. The incompatible REALITY+WS
and Vision+WS combinations are analyzed by failure stage. Vision+WS passes preflight but
fails at runtime; a successful format check does not guarantee a connection.
[Report S5](agent-audit/S5-protocol-compatibility.md).

S3-F01 is open: a new catalogue after an incomplete restore conflicts with stored
config references/pins. SyncServers publishes random IDs before Compile, so
CLI may fail after the catalogue has already changed. The old nodeRef is not substituted;
the group stays on block. A complete set restores 3/3 original IDs.

S3-F02 is open: current/generation/dat links in the archive may point to valid
geodata outside the restored root, including a mixed pair. Reading was confirmed
in 5/5 synthetic variants, without modifying external files.
Mode/UID on a deliberately unsafe restore and HWID loss are described separately
as conditional notes/limitations, not as additional unconditional bugs.
[Report S3](agent-audit/S3-backup-restore.md), [error registry](errors.md).

S2 confirmed three open conflicts: a stale-snapshot CLI bypasses the
RPC ui.lock serialization and deletes an already bound ID (D-S2-01); stop without a readable runtime
loses data for policy cleanup (D-S2-02); the network apply →
current publish sequence has no full rollback on publish/cleanup
errors (D-S2-03). The latter two were verified with real code and external command mocks,
not kernel traffic. Fsync/SIGKILL also showed the config/nodes consistency
boundary (F-S2-01), with no separate promise of a shared crash transaction.
[Report S2](agent-audit/S2-crash-consistency.md). No fixes were applied.

S1/S4 confirmed writer/reader boundary mismatch: addsource bypasses
full ReadConfig validation (S1-F1); pretty serialization of config/nodes may
exceed their read budget (S1-F2); the catalogue writer checks the server count
but not the size, which the catalogue reader then rejects (S4-D01). The last
defect is high severity; the first two require write ACL and are P2. Real
rpcd authorization was not tested. [S1](agent-audit/S1-security.md),
[S4](agent-audit/S4-performance.md).

S6: the global busy snapshot does not cover newly created DOM controls and
allows overwriting dirty for unsaved edits (S6-D01). RPC declarations
other than probe do not reject a nonzero ubus code, and expect turns it into {}:
checked treats the response as successful (S6-D02). Both P2 issues were confirmed in Chromium with
the real SDK rpc.js and backend mocks. S6-A01 separately notes ARIA navigation,
not a new blocker. [Report S6](agent-audit/S6-luci-resilience.md).

B2: a procd/Xray refresh on the real hourly schedule preserved the
updater process and nodeRef when the source endpoint changed. The next local-source error
preserved the working generation and the actual HTTP/SOCKS route. This positive
scenario does not close the reproduced D-S2-01/02/03 at other fault points.
The first conflict of the test TERM cleanup with the owner of Zapret file copies was fixed
in the runner only, history preserved. The VM was restored and read-only verified by
the lead agent. [Report B2](agent-audit/B2-updater.md).
