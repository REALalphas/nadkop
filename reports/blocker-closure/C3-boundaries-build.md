# C3 — geodata boundaries and OpenWrt build paths

Implementer: gpt-6.1-sol, reasoning effort high. Fixes actually applied
2026-11-02 in the shared workspace without Git. No new agents were launched.
S3-F02 and E05 are ready for verification by the lead agent within the boundaries described below;
shared registries are not declared closed by this report.

## Changed files

- `internal/core/geodata.go`: safe opening of the generation and dat files.
- `scripts/build-openwrt.sh`: early project/SDK paths check.
- `internal/core/closure_c3_geodata_test.go`: new desired-behavior checks.
- `scripts/test-closure-c3-build.py`: regression with copies of two SDK parsers.
- This report; logs, fixtures, applied diffs, and the proposed S4 integration are
  only in `build/blocker-closure/C3/`.

`geodata_update.go`, regular geodata tests, and `s3_backup_test.go` were unchanged.
Other people's owned files, historical reports, shared registries, README, package versions,
dependencies, SDK, VM, and packaged artifacts were not changed by this implementer.
All **398 files** of the saved baseline were verified against its manifest: no changes
(`baseline-integrity.json`). This is a baseline check, not a declaration of immutability of
the shared working tree where other implementers work in parallel.

## S3-F02: original behavior and fix

Read the S3 audit and proposed patch. Five boundary escapes were independently reproduced on a copy of
`build/blocker-closure/baseline/internal`:
absolute current, relative escaping current, symlink generation, absolute
geosite.dat symlink, and relative geoip.dat symlink. All five returned a valid
external database via `OpenGeoDatabase`/`Expand`; `CompileWithGeo` also accepted
each case. With a separate dat, internal and external
databases were mixed. Negative assertions on baseline ended in FAIL, as they should.
The positive check of a normal relative generation on baseline passed.
These are direct synthetic filesystem fixtures; no repeated tar round trip was run for
baseline; historical S3 archive evidence is preserved.

Applied read boundary:

- The root is opened as a directory with `O_NOFOLLOW`.
- `current` must be a symlink to a single nonempty `generation.*` name without
  path components. Absolute/escaping/nested links and directory current
  are rejected, even if a separate absolute link points inside the root.
- Generation is opened relative to the root fd via `openat` with
  `O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC` and held until the whole pair is read.
- Both dat files are opened relative to the generation fd with
  `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`; regular-file type is verified before reading.
  A FIFO does not block the process; directories and dat symlinks are rejected.
- Existing parse/size limits are preserved. No fallback to previous appears.
  The atomic publication algorithm of `InstallGeoFiles` was unchanged.

New tests independently require rejection by both `OpenGeoDatabase` and `CompileWithGeo`
for **14 variants**: the listed external links, internal generation/dat
symlinks, absolute internal current, `./generation`, symlink root, dat directory,
dat FIFO, directory current, empty generation suffix. External synthetic
files are checked for immutability. Positive checks verify pair expansion,
successful compile, three installs, previous preservation, and rejection of a corrupted
install without a current change. A separate concurrent check performs 150 reads
under atomic rename of current between two generations and forbids a mixed pair.

Regular TLS update tests confirm successful update and preservation of the previous
generation on HTTP/redirect/truncation/invalid/missing-set/cancel/non-HTTPS
failures. Existing S3 full restore, partial geodata, and five archive symlink
scenarios passed without changing their assertions.

Residual TOCTOU/trust boundary: the caller-provided root and its ancestors must
be trusted; `O_NOFOLLOW` on root checks the last component, not all
ancestors. `Readlink(root/current)` still uses a pathname, but the result
is limited to one name, and generation is opened relative to the already pinned
root fd: this race does not permit pathname escape. The dat pair is opened through
a single generation fd and does not follow current switching. A malicious owner
that concurrently moves the directory, replaces/rewrites regular dat files,
creates hardlinks, or changes mounts is not isolated by this fix. Reading
is not a snapshot transaction against such changes. On fast cleanup of
the old generation, the reader may also get an explicit failure. The protection covers
restored symlinks and normal atomic updates, not a hostile root.

## E05: original behavior and fix

Read the B1 audit and `build/agent-audit/B1/feed_space_repro.py`. The new runner was
independently run against the baseline script with the real SDK parsers
**25.12.6** (`build/openwrt-vm/sdk`) and **24.10.8**. Both baseline project-space
scenarios reached stub make with exit 73 after modifying SDK/output; the required
early rejection was absent. The historical split target defect is confirmed by the
saved `feeds/nadkop` fixtures and logs. Positive plain paths passed.
Final matrix of the expected fix on baseline: **8/56 PASS, 48 FAIL**;
this is the expected negative result, not baseline acceptance.

Now the Python preflight runs immediately after the arity check, **before mkdir
output, cd SDK, feeds.conf, .config, and feeds/make**. Supplied lexical
and physical project/SDK paths are checked, including ones hidden via directory symlinks.
Whitespace (`str.isspace`) and Unicode Cc/Cf/Cs categories are rejected with exit 1 and
a clear message. The check before shell command substitution also detects a
trailing newline, which would otherwise be stripped by `$(...)`. After the check, project/SDK
are resolved physically via `pwd -P`. `CDPATH=''` keeps the previous protection against
CDPATH and removes the previous SC1007s in this script.

Final matrix on each of sh/BusyBox ash/dash: **56/56 PASS**,
**168/168** total. Each parser has four positive scenarios:
absolute ordinary paths, relative paths, `--prepared`, output path with a space.
The real parser creates the correct feed; Makefiles of all three packages are visible;
stub make finishes on defconfig with exit 73. The other 24 scenarios
per parser check project/SDK with space, tab, LF, CR, vertical tab, form feed,
escape, DEL, NBSP, Unicode format, trailing LF, and physical alias with a space.
Failures check the exact exit/message, immutability of the whole SDK fixture, and absence of
new output or immutability of already existing output with a sentinel. Some
fixtures initially lack feeds.conf/.config; some contain both files.

The SDK source is used only for reading `scripts/feeds` and `metadata.pm`;
their copies in C3 are executed. SHA256 of both source parsers are verified before/after and
recorded in each fixture run. make/gmake are fully substituted; package builds,
downloads, and live deployments are not run. Due to the stub metadata scan,
expected feed warnings about indexes are possible in positive fixtures;
feed resolution and file visibility are verified, not package installation/build.

This is **early rejection of unsupported project/SDK paths**, not support for building from
paths with spaces. `Build/Prepare` was not verified by a full build and was unchanged.
Arbitrary other make/shell-special characters are not covered by a separate guarantee.
Output with a space is not part of the feed and is not prohibited by the guard;
its check is limited to a safe stop at defconfig.

## Commands and results

From the project root, `GOMAXPROCS=2`, `-p 1`:

```sh
go test -p 1 ./internal/core -run '^(TestClosureC3|TestGeoParsing|TestMatcherValidation|TestZapretGeo|TestProductGeo|TestProductConcurrentGeo|TestS3GeodataArchiveSymlinkBoundary|TestS3PartialGeodata|TestS3FullBackupRestore|TestS2CorePermissionAndIncompleteGeo)' -count=1 -parallel=1 -timeout 90s -v
go test -race -p 1 ./internal/core -run '^TestClosureC3' -count=1 -parallel=1 -timeout 60s -v
python3 scripts/test-closure-c3-build.py
python3 scripts/test-closure-c3-build.py --shell /nix/store/9k2rlqf5mwsnx1p25nvw1ranl8dz05b7-busybox-1.37.0/bin/ash
python3 scripts/test-closure-c3-build.py --shell /nix/store/h543kp9iqwh0w2hrh3ckikhp8irgmp3a-dash-0.5.13.3/bin/dash
python3 scripts/test-closure-c3-build.py --candidate build/blocker-closure/baseline/scripts/build-openwrt.sh
/nix/store/gds6snrqlmi2kwgamyqcjr1i4kvdqn15-shellcheck-0.11.0-bin/bin/shellcheck --norc -S warning -s sh scripts/build-openwrt.sh
```

Go targeted: exit 0, **13 top-level PASS**, log `geodata-targeted.log`.
Race: exit 0, **3 top-level PASS**, `geodata-race.log`.
Build fixed: exit 0, `build-final-sh.log`, `build-final-ash.log`,
`build-final-dash.log`. Baseline build: expected exit 1,
`build-baseline-final.log`. ShellCheck: exit 0, **no warnings/errors**, empty
`shellcheck-final.log`. Syntax `-n` passed in three shells.

Baseline Go was run in **C3/baseline-probe**, copies of baseline go.mod/internal
with only the new regression test added:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/core -run '^TestClosureC3GeodataSymlinkBoundary$/^(absolute-current|relative-escape-current|generation-symlink|geosite-absolute-symlink|geoip-relative-symlink)$' -count=1 -timeout 45s -v
GOMAXPROCS=2 go test -p 1 ./internal/core -run '^TestClosureC3RelativeGenerationUpdateCompile$' -count=1 -timeout 45s -v
```

First: expected exit 1, five FAILs, `baseline-geodata-five.log`.
Second: exit 0, `baseline-geodata-positive.log`.
Intermediate runs are kept; the early `baseline-geodata.log` accidentally also picked
internal-generation-symlink due to an unpinned subtest regex, so the
exact five-case rerun is kept separately. `workspace-subset-initial.log`
refers to the fixed workspace, not baseline.

## Integration for the lead agent and remaining limitations

1. Clarify the README/build contract: "project and SDK, including their physical paths,
   must be free of whitespace and control characters; the builder rejects such
   paths before modifying SDK/output. Full support for spaces in Build/Prepare is not
   implemented." C3 did not edit these shared documents.
2. The historical B1 profile is tied to old line numbers/contents. For the
   current build script all three previous SC1007s are eliminated; the ShellCheck check
   passes without exclusions. The profile will require integration review together
   with the C2 changes, without automatically carrying over/extending old exclusions.
3. The foreign `internal/core/s4_performance_test.go:s4RealGeoRoot` creates an absolute
   current pointing at an external directory. This fixture is incompatible with the fixed
   boundary; its opt-in real-geodata cases cannot be counted as product regression.
   Did not change the file. Exact proposed diff:
   `build/blocker-closure/C3/S4-real-geo-fixture.integration.patch` — read
   the pair from S4_GEO_DIR and install it via InstallGeoFiles into a private root.
   This preserves the real dataset and assertions. The optional S4 benchmark
   on real large databases was not run here.
4. Full overall regression, real OpenWrt package builds, and router lifecycle
   were not run per ownership/scope; their results are not claimed here.
   The residual TOCTOU/hostile-writer boundary is described above. No other unclosed
   defects in the assigned C3 area were found by targeted checks.
