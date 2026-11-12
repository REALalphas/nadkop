# Contributing

Needs Go 1.23+, no external Go dependencies.

```sh
go test ./...
go vet ./...
python3 scripts/test-release.py
sh scripts/bump-version.sh --check
go build -o build/nadkop ./cmd/nadkop
./build/nadkop validate -config examples/config.json
```

Notes:

- `go test ./...` must be fully green. If a test unrelated to your change
  goes red, mention it in the PR.
- Network and VM tests (`scripts/test-*-openwrt.sh`) need a dedicated OpenWrt VM
  (`docs/openwrt-vm.md`). Never run them against a production router.
- OpenWrt packages are built with the SDK matching the router release
  (`./scripts/build-openwrt.sh /path/to/sdk /path/to/output`).
- Version bumps go through `scripts/bump-version.sh` (single command keeps
  Makefiles, the LuCI cache-buster, installer pins and `CHANGELOG.md` in
  sync); never edit version strings by hand in several places.

## Releases

- Push a version bump to `main` (`scripts/bump-version.sh X.Y.Z-rN`,
  replace the `TODO` in the `CHANGELOG.md` section it creates, commit,
  push): the Auto-release workflow tags `v<UI-version>` and the Release
  workflow builds host binaries + OpenWrt packages and publishes the
  GitHub release. No manual tagging needed.
- Auto-release refuses to tag while the `CHANGELOG.md` section still has
  the `TODO` placeholder — fill it in and push again.
- The tag is pushed with the `RELEASE_PAT` secret (a PAT with
  `contents:write`; repo Settings → Secrets and variables → Actions).
  A `GITHUB_TOKEN` push would not trigger the Release workflow, which is
  why the PAT is required; the job fails with instructions if it is missing.
- Every published bundle needs a new LuCI version, which identifies its release
  tag, including bundles with only core or installer changes. Use `--core` and
  `--ui` for separate revisions. Never move an existing release tag.
- Manual dispatch retries an existing `v<UI-version>` tag; it does not create
  a tag or replace an already published release. The version must match the
  LuCI Makefile at that tag.
- Release publication requires passing Go tests, vet, version consistency and
  release regression checks. Before declaring a stable release, rebuild with
  the matching SDKs and complete install, upgrade, reboot and LAN routing
  checks on dedicated VMs. Record the exact commit and package checksums in
  the release validation record.
- Keep `build/`, `reports/`, local agent instructions, test logs and package
  artifacts out of Git. Release source archives contain the source, tests,
  examples, build scripts, documentation, repository automation and license
  files. Local guidance is excluded through `.gitattributes`.
