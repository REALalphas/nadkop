# Contributing

Needs Go 1.23+, no external Go dependencies.

```sh
go test ./...
go vet ./...
go build -o build/nadkop ./cmd/nadkop
./build/nadkop validate -config examples/config.json
```

Notes:

- `go test ./...` must be fully green. If a test unrelated to your change
  goes red, mention it in the PR.
- Network and VM tests (`scripts/test-*-openwrt.sh`,
  `scripts/test-product-*.py`) need a dedicated OpenWrt VM
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
- The release tag tracks the LuCI UI version. For changes auto-release
  does not cover (e.g. a core-only bump that leaves the UI version — and
  its tag — unchanged), trigger the Release workflow manually
  (Actions → Release → Run workflow).
- Do not commit `build/` output, `*.apk`/`*.ipk`, or machine-generated
  `reports/logs/` / `reports/runs/`.
