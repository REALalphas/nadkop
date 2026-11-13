# PR #1 verification — 2026-11-13

Scope: `feature/harden-init-import-tune`, based on `99002da`, plus the
installer and VM-test fixes recorded with this report. No release version bump.

## Local checks

- Go 1.26.10: `go test ./...`, `go test -race ./...`, `go vet ./...`, build,
  example-config validation, and `gofmt -l cmd internal`: passed.
- `scripts/bump-version.sh --check`: passed (core 0.3.0-r6,
  LuCI 0.3.0-r13, nfqws 2026.11.01-r1).
- `python3 scripts/test-release.py`: 32 passed, including local APK installation
  without network access and refresh/retry when a dependency is unavailable.
- `python3 scripts/test-init-lock.py`: 3 passed; already required by CI before this follow-up.
- ShellCheck warning profile, JavaScript/MJS syntax, Python compilation,
  Actionlint for CI, and `git diff --check`: passed.

## Dedicated OpenWrt VMs

Rebuilt the three packages using the matching x86_64 SDKs. OpenWrt 25.12.6
used the existing `nadkop-openwrt` VM; OpenWrt 24.10.8 used a separate disposable
KVM guest with user networking and a localhost-only SSH forward. Network tests
ran only inside these guests.

Both releases passed:

- `scripts/test-lifecycle-openwrt.sh`: 8 assertions, including real rc.common /
  procd handling of busy `stop` and `shutdown`. Both refuse promptly, retaining
  the generation and Xray/updater PIDs. Reload/restart retain the updater;
  invalid reload preserves the generation; disabling updates removes the
  updater; successful stop removes the generation pointer.
- `scripts/test-openwrt.sh`: direct/proxy/block routing, no direct fallback
  after upstream failure, unprivileged nfqws, NFQUEUE 200 and off-hostlist HTTPS,
  generation replacement, process/firewall cleanup, and direct-only routing.
- `scripts/test-openwrt-rpc.sh`: subscription import and duplicates, admin ACL,
  revision conflict, empty-source/catalogue cleanup, rule persistence, seven
  diagnostic stages, Xray preflight, RPC restart/stop and action validation.
  This checks the LuCI assets and RPC backend; no interactive browser test.
- Installation with external IPv4/IPv6 traffic rejected by a temporary nftables
  output chain. APK replaced the installed packages and upgraded LuCI;
  IPK was tested both with packages already installed and after removing the
  three project packages while retaining dependencies. Installer exits were 0.

The 25.12.6 guest retained byte-identical config, nodes, catalogue and HWID,
its enabled autostart state, and its initially stopped service state.
The 24.10.8 stopped/disabled installation also preserved its saved private files
and disabled/stopped state.

The 24.10.8 guest passed the final Zapret reboot check: Xray, nfqws and the
updater started automatically, private file checksums matched, and NFQUEUE 200 /
the nadkop nftables table reattached (`reboot24-after.log`, command exit 0).

An earlier combined direct-reboot / next-scenario command printed
`PASS-direct-autostart-reboot-and-private-state`, then exited 1 while preparing
Zapret: that candidate lacked required `zapretArgs` and was rejected as
`Invalid saved configuration` (`reboot24-direct.log`). This combined command is
not counted as a clean verification pass. After supplying `zapretArgs`,
validation succeeded (`reboot24-queue-before.log`), followed by the successful
Zapret reboot check above. Both guests were powered off afterward.

## Findings corrected during verification

- APK initially attempted network index downloads even before `apk update`.
  The first local attempt now passes `--no-network`; online refresh and retry
  remain available when dependencies are missing. The blocked-network install
  then completed successfully.
- The RPC harness expected six diagnostics although coexistence is the seventh.
  It now validates that stage and establishes its own clean fixture, restoring
  the original VM files afterward.
- On this NixOS host, the cached 24.10 SDK fakeroot and system tar produced IPKs
  owned by the build user. Rebuilding with the SDK tar corrected all package
  entries to UID/GID 0. This was a local build-environment workaround, not a
  change to runtime ownership validation. For reproduction:

  ```sh
  MAKEFLAGS="TAR=$SDK/staging_dir/host/bin/tar" NADKOP_CLEAN_PACKAGES=1 \
    scripts/build-openwrt.sh "$SDK" "$OUTPUT" --prepared
  ```

## Evidence and limits

Sanitized command logs, package checksums and build metadata are in the local,
gitignored `build/pr1-verification/` directory. Private fixture backups remain
inside the guests and are not published. SDK build logs are local `/tmp/nadkop-pr1-build*.log`.

These checks establish the exercised VM behavior; they do not establish
physical-router compatibility, ISP DPI effectiveness, browser interactions,
or package downgrade/rollback behavior.
