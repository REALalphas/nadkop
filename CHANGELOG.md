# Changelog

## 2026-11-12 — core 0.3.0-r6, LuCI 0.3.0-r13

- Normalize stock IPK `-N` package revisions to the `-rN` release asset names
  expected by the installer, including the architecture-independent LuCI package.
- Verify the downloaded installer checksum before execution and use a private,
  unique download directory. Reject unsupported release architectures early.
- Require Go tests, vet, matching release tags, and release regression checks
  before building and publishing; serialize publication for the same release.
- Validate version bump input before writing, check the nfqws pin, and create
  changelog entries for UI-only releases. Match exact LuCI versions in notes.
- Remove a duplicate VPN label and correct the corresponding UI assertion.
- Document supported package ABIs, upgrades and private security reporting.
- Remove internal audit reports and obsolete audit orchestration from the
  public source tree; keep local guidance out of source release archives.

## 2026-11-12 — core 0.3.0-r6, LuCI 0.3.0-r12

- Fix automatic release tagging so the release token is used without a
  persisted checkout authorization header overriding it.
- Fix GitHub Actions expression parsing in the generated release installer.
- Update OpenWrt SDK URLs and build prerequisites for APK and IPK builds.
- Include the LuCI APK and normalize architecture names in IPK release
  assets to match the installer download names.
- Add local release workflow regression checks to CI.

## 2026-11-05 — core 0.3.0-r6, LuCI 0.3.0-r10…r11

- Add the Zapret strategy tuner and expanded runtime diagnostics.
- Preserve focus and input during background tuner polling and keep open
  diagnostic sections when their state has not changed.
- Correct local flag URLs and update the stylesheet cache version.
- Retry installer validation and apply when a concurrent package hook holds
  the configuration lock.
- Include the interrupted-save marker in sysupgrade backups.

## 2026-11-03 — core 0.3.0-r5, LuCI 0.3.0-r9

Improve configuration safety, service recovery and LuCI accessibility.

### Saving and subscriptions

- Concurrent operations from the UI and the command line no longer overwrite each other's state from a stale copy of the configuration.
- Before a subscription is added, its address, parameters, and the resulting configuration are validated. Invalid data is rejected before writing.
- The size of the configuration, local servers, and catalogue is checked before writing. An oversized result never replaces working files.
- On incomplete backup restore, the app reports missing linked data while keeping existing server IDs.

### Start and stop

- Stopping cleans up its own network rules even with a damaged working-generation file, and reports cleanup errors.
- Errors from network commands and generation publishing trigger a rollback. If the rollback does not finish, the journal is kept for a later restore.
- A journal-cleanup error after a successful generation switch no longer leaves applied network rules behind with no running services.

### Interface

- During an operation, new and redrawn controls stay locked until it completes.
- Error codes of all 13 RPCs are checked. After a save error, no restart is requested.
- When a save response is lost, the UI shows "Save not confirmed". If the save went through but applying was not confirmed — "Saved, apply not confirmed".
- Edits made after a save started are not marked as saved. The draft stays available after an error.
- When moving rules and servers with the keyboard, focus and scroll position are kept. Pinning and the pinned filter also keep focus.
- The select and pin buttons distinguish same-named servers by subscription, and on a full match — by stable number. An already added connection is marked separately.
- The reason Zapret is unavailable opens next to the action and is reachable from keyboard and touchscreen.
- Tabs support arrows, Home, and End; Tab moves focus onward from the tab list.

### Service changes

- Added `nadkop --help`, `nadkop help COMMAND`, and per-command help. Reading help does not read settings or start operations.
- Link validation in the geodatabase catalogue is tightened: unsupported generation and file links are rejected.
- The build rejects unsupported paths with spaces and control characters up front, before touching the SDK.

### Upgrade compatibility

- Preserve configuration, local servers, device identity, selected servers and
  pins during package upgrades.
- Reject unsupported build paths before changing the SDK.

Saving detects an interrupted config/nodes update; recovery steps and durability
limits are described in [Interrupted saves](docs/interrupted-save.md).
