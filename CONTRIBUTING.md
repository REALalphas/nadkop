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
- Do not commit `build/` output, `*.apk`/`*.ipk`, or machine-generated
  `reports/logs/` / `reports/runs/`.
