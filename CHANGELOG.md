# Changelog

## 2026-11-05 — core 0.3.0-r6, LuCI 0.3.0-r10…r11

Install details: `docs/openwrt-vm.md`; check results are recorded
in local `verification.json` (not published). The r6/r10 Go core was
reconciled against the internal manifest (419 files, 0 differences); for r11 only
3 static files were reconciled.

- r6/r10: final X2/Zapret tuner bundle; 91 outbounds in candidate-preflight,
  Xray `Configuration OK`; the catalogue updated normally (37 IDs, 7 separators,
  0 unresolved links); `keep.d/nadkop` includes `.save-state.json`.
- Known limitation of the new diagnostics: `runtime-monitor-action` stays
  `unknown` because `/var` is a symlink to `/tmp` and `monitor.go` writes
  `monitor-status.json` only on an exact `runtimeRoot` (`internal/core/monitor.go`).
- r11 (LuCI only): background tuner polling no longer resets focus/input,
  diagnostics no longer closes open `details`, flag URL fixed,
  `picker.css` cache-buster `?v=0.3.0-r11`. Checks: tuner UI 30/30, product-UI
  and picker pass; `go test` was not rerun (Go code unchanged).
- Update: `install-openwrt.sh` repeats `validate` and `apply` on a
  `ui.lock` race from the post-upgrade hook (3 attempts, 2/5/10s delays).

## 2026-11-03 — core 0.3.0-r5, LuCI 0.3.0-r9

Upgrade from core 0.3.0-r4 and LuCI 0.3.0-r8. Includes the accepted C1–C4 fixes and X1 rework.

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

### Install and verification

- Rebuilt APK and IPK packages for x86_64; APKs installed on the working server.
- Before install, a private backup was saved; the current configuration, Xray, and network conditions were checked.
- After install, the Xray, Zapret, and monitor services, LuCI tab loading, device pickers, and mobile width were verified.
- Configuration, local servers, and HWID kept; bindings of selected and pinned servers verified.

No guarantee of overall multi-file atomicity on sudden power loss is added. Known HWID-restore and file-permission limitations are listed in the [fix acceptance](reports/blocker-closure/acceptance.md). Full UI-check history is in the [X1 report](reports/improvements-X/implementation.md).
