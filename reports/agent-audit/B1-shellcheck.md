# B1 — independent ShellCheck review

Date: 2026-11-02. Target: pinned core 0.3.0-r4 / LuCI 0.3.0-r8 build.

**Criterion B1 status: closed** for the nine warnings and the verifiable `warning`-level
profile. At the same time, a **separate open build defect with a space
in the source path** was found: it is unrelated to the nine warnings and is unfixed.
This build-scenario limitation must be considered when assessing the release.

A plain unmodified ShellCheck run did not succeed: **9 warnings, exit 1** independently reproduced
(`log` (local stand artifact)).
The new `profile` (local stand artifact) checks the builder
in `sh`, the installer in both `sh` and `busybox`, and init in `busybox`.
In this profile, after dialect selection, **seven specific messages** remain;
exactly these messages are accepted by location and original line text, without disabling
any codes wholesale. Two SC3043s disappear due to the correct dialect.
The profile exits 0, but each ShellCheck run inside it returns
1 due to the corresponding accepted diagnostics. This is not a claim that the original
command passes without exceptions.

## Review of all nine messages

| Original message | Conclusion and basis |
| --- | --- |
| `build-openwrt.sh:10 SC1007` | `CDPATH= cd ...` — correct temporary empty assignment before the `cd` command, not a missing value. When computing the project directory it excludes `CDPATH` influence on directory selection and extra `cd` output into command substitution. |
| `build-openwrt.sh:11 SC1007` | Same construct when computing the SDK. Verified with a relative path, a conflicting `CDPATH`, and spaces in the directory name. |
| `build-openwrt.sh:19 SC1007` | Same construct when computing the results directory. An exact copy of the first 19 lines returned the correct path instead of the matching name in `CDPATH`. These three synthetic checks were run in ash, dash, and `/bin/sh`; the original builder was also executed up to the safe feed stub. |
| `nadkop.init:3 SC2034` | `USE_PROCD=1` is consumed externally: `rc.common` first sources `. "$initscript"`, then checks `USE_PROCD` and includes procd. No export is needed since it is the same shell. In a synthetic root with the real init, loading of the procd stub was confirmed; with empty `USE_PROCD` it is not loaded. |
| `nadkop.init:4 SC2034` | `START=95` is read by external `rc.common` for `enable`/`enabled`. The `S95nadkop` symlink was produced in the synthetic root. |
| `nadkop.init:5 SC2034` | `STOP=10` is likewise used for `K10nadkop`; `enabled` and link removal via `disable` were also verified. |
| `nadkop.init:25 SC3043` | `local` is not defined by the POSIX standard: the warning in `sh` mode is valid for a portable POSIX interpreter. But init has the shebang `#!/bin/sh /etc/rc.common` and targets OpenWrt/BusyBox ash, where `local` is supported. The scope of all variables declared here and the locality of the function positional parameters were verified. |
| `nadkop.init:47 SC2046` | Field splitting is intentional here: the kernel returns two numeric limits, `set --` turns them into two arguments, the next line checks `"$#" -eq 2`. Tabs/spaces, nonzero minimum, zero minimum, absence, and wrong field count were verified against the local file. Simply adding quotes turns the two limits into one argument and the check fails. The generalized ShellCheck warning is valid for untrusted arbitrary output: after splitting, globbing runs; the acceptability of the exception here depends on the numeric format of exactly this `/proc` and the normal `IFS`. |
| `nadkop.init:107 SC3043` | Same ash `local` extension, here for `path`; scope verified separately together with line 25. |

`rc.common` from both local SDKs has SHA256
`dd8ae95c78d10cccf78cb487874f163ace4c1ee14dace05fe28393b19ba639e4`.
In both files init sourcing is line 119, the `USE_PROCD` check is 121,
`START`/`STOP` usage is lines 47–51 and 60–62. Besides the external-contract
test with the stub, the real local `functions.sh`, `service.sh`,
`procd.sh`, and the available `jshn.sh` were used to verify preservation of the standard
`IFS` before and after `procd_open_service` on a synthetic command. No ubus connection,
real init launch, `/proc` write, or action on the router was performed.

The profile pins each exception by file, line number, code, `warning` level,
and **full line content**. For SC2046 the exact following argument-count check is
additionally required. Other warnings and errors are rejected;
an analyzer launch failure also does not count as success. After code changes
or line moves, exceptions must be re-reviewed, not automatically
reassigned. A separate test on copies proved rejection of new SC1007, SC2034,
SC2046, SC2115, a syntax error, modification of an accepted line, and removal of
the field-count check: **8/8**, `log` (local stand artifact).

## New defect: spaces in the source path and the local feed

**Open.** `scripts/build-openwrt.sh:26–33` writes the
`$project/openwrt` path into `feeds.conf` as part of `src-link nadkop …`. The real
`scripts/feeds` of both local SDKs split sources on whitespace.
With a project path like `source has spaces`, one path is parsed as three
sources, and the created `feeds/nadkop` in the reproduction points to
`spaces/openwrt` instead of the Nadkop directory. Scanning through this link does not see
the package. Without spaces the control link resolves correctly. This is not an error
in the `CDPATH=` assignment itself and not a ShellCheck warning.

Minimal reproduction that changes neither the product nor the SDK:

```sh
python3 build/agent-audit/B1/feed_space_repro.py
python3 build/agent-audit/B1/feed_space_repro.py --sdk-root build/openwrt-vm/sdk-24.10.8
```

The test copies the **unmodified** build script and the real `feeds`/`metadata.pm`
into its temporary synthetic SDK; `make` is replaced with a safe stub.
The control and the spaces case reach the stub's intentional `exit 73`;
the test then checks the actual parser result and visibility of the synthetic
package file. `exit 73` does not mean a successful build. Results:
`SDK 25.12` (local stand artifact),
`SDK 24.10` (local stand artifact).
In the test SDK, warnings about missing indexes are expected due to the stubs and
are also present in the control; no full package was built there.

The minimal **unapplied** proposal —
`feed-space.patch` (local stand artifact): reject a source
path containing whitespace with a clear message before writing `feeds.conf`.
In isolated copies the guard was verified for space, tab, and a normal path
(`log` (local stand artifact)); correct application of the
patch to a copy was verified separately
(`log` (local stand artifact)). This is a restrictive measure,
not support for building from paths with spaces; the defect remains open. If full
support for such paths is needed later, separate verification of package recipes
and a real build is required. As an example: an attempt to fix only the feed via a symlink
in the synthetic SDK restored package visibility, but the exact
`Build/Prepare` command from `openwrt/nadkop/Makefile:27` still splits the
`$(CURDIR)` containing a space and fails with a `cp` error. This insufficient variant and
its verification are saved in the `log` (local stand artifact);
it must not be treated as a complete fix.

## Commands and results

The tool paths below refer to the current Nix system; in a different environment
the corresponding executables can be passed via the same parameters.

```sh
B1_SHELLCHECK=/nix/store/gds6snrqlmi2kwgamyqcjr1i4kvdqn15-shellcheck-0.11.0-bin/bin/shellcheck
B1_ASH=/nix/store/9k2rlqf5mwsnx1p25nvw1ranl8dz05b7-busybox-1.37.0/bin/ash
B1_DASH=/nix/store/h543kp9iqwh0w2hrh3ckikhp8irgmp3a-dash-0.5.13.3/bin/dash

"$B1_SHELLCHECK" --norc -S warning scripts/build-openwrt.sh scripts/install-openwrt.sh openwrt/nadkop/files/nadkop.init
python3 build/agent-audit/B1/profile.py --shellcheck "$B1_SHELLCHECK"
python3 build/agent-audit/B1/test_profile.py --shellcheck "$B1_SHELLCHECK"
python3 build/agent-audit/B1/semantic.py --ash "$B1_ASH" --dash "$B1_DASH"
python3 build/agent-audit/B1/test_guard.py
python3 build/agent-audit/B1/test_linkonly_limit.py
python3 build/agent-audit/B1/integrity.py
```

- ShellCheck 0.11.0 without profile: **exit 1, 9 warning**; under the profile:
  **exit 0**, 7 point exceptions, `log` (local stand artifact).
- Behavior and syntax checks: **54/54**,
  `log` (local stand artifact). BusyBox ash
  1.37.0 from Nix, dash 0.5.13.3, and host `/bin/sh` (bash 5.3) were used, not the OpenWrt
  shell installed on the VM.
- Failed-run history is preserved: in the first extended locality test,
  incorrect escaping in the **test** caused 3 failures out of 49
  (`log` (local stand artifact)); after
  the test was fixed, the check passed. The first check of the real `procd.sh`
  lacked a synthetic `var/lock`: 2 failures out of 54
  (`log` (local stand artifact)); after creating
  the directory only inside the test root, the check passed. The first feed experiment
  had an unprepared `make` stub and ended at the missing
  `defconfig` target, exit 2; it is saved
  `here` (local stand artifact). The subsequent
  reproducible run is explicitly stopped by the stub with exit 73.
- During preliminary verification of `printf` output, suspicion arose due to
  escaped rendering. Byte-wise inspection of the source lines showed a single
  backslash before `n`, i.e. a correct newline; no defect in these
  formats was confirmed.

## Limits and integrity

The `ping_group_range` check reproduced the original three lines with the path replaced by
a local file: the kernel and the actual sysctl write were not tested. The numeric
`/proc` format and stock `IFS` are preconditions of the SC2046 exception. Testing
`rc.common` in a separate directory does not replace real service control and
OpenWrt installation. The `203.0.113.10` VM, shared SDKs, packages, and real system
settings were unchanged; no network requests were made.

The `frozen-source.json` manifest has SHA256
`09c809c6688f4369011e6b02d2bf52bc06be08300d3487d80977677a47e534d2`.
All **327/327** files were reconciled before and after the work: **0** changed or missing;
`initial` (local stand artifact) and
`final` (local stand artifact) logs. Production code,
package recipes, README, and existing reports were not edited.
