## What
<!-- one paragraph: what changes and why -->

## Checks
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l cmd internal` clean
- [ ] `scripts/bump-version.sh --check` passes (if versions touched)
- [ ] CHANGELOG.md updated (if user-visible change)
- [ ] VM verification referenced (if router behavior changed)

## Risk
<!-- rollback: previous packages reinstall cleanly? conffiles preserved? -->
