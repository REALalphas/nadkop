# C2 — network stop and current publication rollback

Implementer: gpt-6.1-sol, reasoningEffort=high. Fixes are **applied** in the
shared workspace. D-S2-02 and D-S2-03 are handed to the lead agent for acceptance:
baseline reproductions are separated from checks of the fixed behavior.
Shared registries/README and historical reports are unchanged.

## Changed files

- `internal/router/network.go`: stop cleanup without trusting runtime.json;
  private journal write before network changes; restoration of the original
  nft/input and actual route/rule; preservation of rollback errors; recovery of an
  unfinished operation; rollback/commit operations via `Network(action, dir)`.
- `openwrt/nadkop/files/nadkop.init`: rollback on ln/mv failure, including the first
  start; commit after current publication; candidate preservation if apply
  left an unfinished journal; removal of current.next after successful rollback.
- New `internal/router/closure_c2_network_test.go`: negative journal/generation boundaries,
  corrupt/missing previous runtime, foreign selectors,
  IPv6 JSON defaults, error visibility and ownership on late conflict,
  first-retry preflight with an unfinished journal.
- New `scripts/test-closure-c2-network.py`: independent baseline/fixed modes,
  namespace fixtures, observations before the first retry, strict target-state
  checks, launch with the selected shell.
- This report. All other C2 artifacts are in `build/blocker-closure/C2/`.
  `internal/router/network_test.go` and old S2 runners/tests were unchanged.

## What was personally reproduced on baseline

The `build/blocker-closure/C2/nadkop-baseline` executable was built from the saved
copy of the sources; init was also taken from baseline. In bwrap user/mount/net/pid
namespace the real CLI/network code and init were run; external ip/nft/uci/Xray
and procd are represented by logging mocks. No real routing/firewall commands
run on the host. The binary and all fixtures are recorded outside baseline.

Baseline command:

```sh
GOMAXPROCS=2 go build -p 1 -o <repo>/build/blocker-closure/C2/nadkop-baseline ./cmd/nadkop
```

Then from the project root:

```sh
python3 scripts/test-closure-c2-network.py --variant baseline --run-id final-ash --shell /nix/store/9k2rlqf5mwsnx1p25nvw1ranl8dz05b7-busybox-1.37.0/bin/ash
```

Log: `build/blocker-closure/C2/baseline-final-ash.log`; JSON:
`build/blocker-closure/C2/baseline-final-ash/results.json`.
**6 scenarios, 14/14 checks for presence of the previous defects**:

- first ln and mv failure: new nft and both IPv4 policies remain without current;
  first retry rejected with `without an owned generation`;
- previous TPROXY disabled, ln and mv failure: new policies remain;
- cleanup-error after successful nft during the enabled → disabled transition:
  previous nft not restored;
- corrupt runtime on stop: exit 0 with a remaining own IPv4 rule.

This matches the historical S2 `namespace-fixtures/final2`, which was also
read but not changed. A green baseline here means error reproduction,
not successful acceptance. Re-verification of **all** baseline files against the manifest
revealed no changes: `build/blocker-closure/C2/integrity-and-diff.log`.

## How it was fixed

`network-transaction.json` has mode 0600 in the private runtime. It is saved
via the existing atomicWrite before adding policies or nft apply and contains
the previous rules/input and the actual presence of own rule/route per family.
Therefore a disabled previous no longer hides just-added policies.

The new `rollback` and `commit` actions use the already existing
`Network(ctx, action, dir)` interface and CLI `-generation`; no cmd/rpc/core edits needed.
Rollback is bound to a specific unfinished generation; a foreign name and an already
published current are rejected. The first start may delete its own
table before current appears, since the journal proves ownership of the operation.
An ordinary table without current **and without a journal** is still rejected.

On policy add/nft apply error, the actually added objects are rolled back.
On cleanup-error **after** nft success, the previous nft and the full
original policy set are restored; the original error is still returned. Compensation
errors are not suppressed. If rollback itself failed, the journal remains, and a
subsequent check/apply/stop first tries to complete recovery. After
publication of a matching current, the journal is considered complete; init explicitly
calls commit. The preparatory nft preflight from prepareSteps can
recognize pending ownership and does not block the first retry before init recovery.

Stop cleans the exact IPv4/IPv6 policies regardless of corrupted, missing,
or stale runtime. An error of one operation does not prevent attempting to clean another
route/family; current is deleted only after successful cleanup. When
`/proc/sys/net/ipv6` is absent, IPv6 cleanup requests are not executed. An ip -6 error with
existing IPv6 capability is not masked as absence of IPv6.

The ownership check rejects extra policy selectors (uidrange, ipproto,
sport/dport, iif/oif, and others), unknown fields, a conflicting table, and duplicates.
Known ip JSON service fields, empty flags, and standard IPv6
metric=1024/pref=medium are allowed. Ownership is rechecked before compensating policy deletion.
No table flush or broad rule delete. Unchanged
other people's main route/rule and fw4 input rule are present in fixtures and verified.

## Checks of the fixed behavior

From the project root:

```sh
GOMAXPROCS=2 go test -race -p 1 -run 'TestClosureC2|TestProduct(Policy|Nft|Stop|Network)|TestS2NetworkRollbackBoundaries' -count=1 -v ./internal/router
GOMAXPROCS=2 go build -p 1 -o build/blocker-closure/C2/nadkop ./cmd/nadkop
python3 scripts/test-closure-c2-network.py --variant fixed --run-id final-source-ash --shell /nix/store/9k2rlqf5mwsnx1p25nvw1ranl8dz05b7-busybox-1.37.0/bin/ash
python3 build/agent-audit/B1/profile.py --shellcheck /nix/store/gds6snrqlmi2kwgamyqcjr1i4kvdqn15-shellcheck-0.11.0-bin/bin/shellcheck
/bin/sh -n openwrt/nadkop/files/nadkop.init
/nix/store/9k2rlqf5mwsnx1p25nvw1ranl8dz05b7-busybox-1.37.0/bin/ash -n openwrt/nadkop/files/nadkop.init
```

- Go targeted/race: PASS, no race detector remarks;
  `build/blocker-closure/C2/go-targeted-final-source.log`.
- Namespace final source + BusyBox ash: **38 scenarios, 234/234 assertions**;
  `build/blocker-closure/C2/fixed-final-source-ash.log` and
  `build/blocker-closure/C2/fixed-final-source-ash/results.json`.
- Historical B1 profile: PASS without carrying over exclusions/editing the profile;
  `build/blocker-closure/C2/shell-profile-final.log`. The unchanged SC2046 stays
  on line 47 with the previous immediate two-argument check.
- sh/ash syntax: exit 0, `build/blocker-closure/C2/shell-syntax.log`.

Scenarios include first ln/mv; previous disabled/enabled; IPv4↔dual-stack;
successful enable/disable; cleanup-error after nft success; policy/nft apply
failures; errors of nft/policy rollback itself and first retry after fault removal;
SIGKILL of own init before current and recovery; corrupt/missing/dangling/no
current; IPv6 absence; IPv6 inspection failure; foreign table/rule/route.
A separate snapshot is kept until recovery; retry does not manually repair current, policies,
table, or journal, only prepares a new candidate if init
deleted the previous one. Without capability, a repeated IPv6-required start
keeps being rejected; this is an expected negative boundary.

Early logs are kept. In `fixed-initial.log` three **runner expectation** errors:
no journal is required after successful full restoration under persistent
cleanup-error; an IPv6-required retry cannot pass on an IPv6-less fixture;
uninspectable IPv6 cannot be counted as cleaned. Expectations were refined without weakening
restoration checks: now the exact original network state is verified,
error/immutability under missing capability, and cleanup of available families.
Subsequent runs succeed. In `go-targeted-accepted.log` the new
ownership protection is additionally verified; the final source is confirmed by a separate run.

## Limitations and handover to the lead agent

The result is confirmation of orchestration/command-state on real Go/init with
external mocks, **not** verification of real nft/ip kernel, LAN/DNS/VPN, Xray/nfqws,
or procd on OpenWrt. IPv6-less capability is modeled by hiding the indicator
only inside the namespace and by mock ip -6 failure; there is no real IPv6-less kernel.
Conformance of ip-full JSON on target OpenWrt builds should be confirmed by a shared/VM
check; allowed IPv6 service fields are covered here by unit tests.

If system rollback commands keep failing, full restoration physically
does not happen: the operation returns an error, journal/candidate
are preserved, foreign policies are not deleted. This is a verified failure with
recovery capability, not a promise of success despite kernel failure. SIGKILL before current is covered;
kill/power loss after current and before process registration, flash durability,
concurrent foreign changes to reserved objects, and hardware failures
are not covered. These edits do not promise a general current/procd crash transaction.

The old CLI usage text still lists check|apply|stop; the new actions
work functionally through the existing parser. For documentation completeness
the lead agent **may** replace the usage line in `cmd/nadkop/main.go` with
`usage: nadkop network check|apply|stop|rollback|commit [-generation DIR]`.
This is a cosmetic suggestion; no mandatory integration edit of someone else's API
or file is needed for the fix to work.

Full SDK build, deployment, overall regression, and blocker status changes
were not performed — that is the lead agent's area. Package versions/dependencies,
SDK/VM, host firewall/routes/sysctl, user data, and credentials
were untouched.
