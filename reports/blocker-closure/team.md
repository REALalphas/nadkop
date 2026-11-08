# Blocker closure — Sol 6.1 High

The current user request permits fixing blockers from
`reports/blockers.md`. The previous audit freeze is preserved as history and
does not prohibit this separate fix stage. Before changes, a copy of sources,
tests, and registries was saved: `build/blocker-closure/baseline/manifest.json`.
There is no Git repository in the working directory; agents work in a shared folder
with separation of file ownership. Old packages and reports do not prove a fix.

## Implementers

All fix implementers: `gpt-6.1-sol`, `reasoningEffort=high`.

| Task | Blockers | Change ownership | Report |
| --- | --- | --- | --- |
| C1 — configuration and catalogue | D-S2-01, S3-F01, S1-F1, S1-F2, S4-D01 | cmd/nadkop, core/config and servers, router/rpc and source_import, related tests | C1-state.md |
| C2 — network and lifecycle | D-S2-02, D-S2-03 | router/network, nadkop.init, related tests | C2-network.md |
| C3 — geodata and builder | S3-F02, E05 | core/geodata and geodata_update, scripts/build-openwrt.sh, related tests | C3-boundaries-build.md |
| C4 — LuCI | S6-D01, S6-D02 | overview.js, picker.js/CSS if needed, related tests | C4-luci.md |

The lead agent maintains the shared registries, README, and final integration verification.
Implementers do not change other people's areas, shared SDKs and installed VMs, do not build
packages concurrently, and do not record secrets. For boundary changes they report
to the lead agent. New logs go to `build/blocker-closure/C*/`.

## Closure criterion

Each ID requires a verified original scenario, an applied fix,
verification of the required behavior, a suitable regression, and explicit limitations.
Previous audit tests that pass while the defect is present do not
constitute closure by themselves. A missing external verification is not replaced by success.
The final status in the registry changes after verification by the lead agent.

## Separate stream X

Coordinator and analysts: Sol 6.1 High. Areas: interfaces and usability; speed,
resources, reliability, and maintenance. Reading, limited local
measurements, and reports in `reports/improvements-X/` with artifacts in
`build/improvements-X/` are permitted. Product code, positioning, purpose, and composition
are unchanged. Proposals are kept as an interim summary.

Once these proposals are ready, a separate `gpt-6.1-terra` agent with
`reasoningEffort=max` must critique their evidence, benefit, cost,
risks, and compliance with constraints. The X coordinator keeps the critique and the refined
summary without implementing the proposals. Run IDs and links are in `assignments.json`.

## Status

All four C1–C4 tasks on Sol 6.1 High are complete and accepted by the lead agent.
All 11 original blockers are closed in the sources; S6-A01 is fixed separately.
C1 test expectations, the S4 geodata fixture, diagnostics, and the network
rollback/commit usage are integrated. The lead agent additionally fixed handling
of the cleanup error for an already published network journal.

Contract regression with race detector: 159 PASS / 0 FAIL / 11 SKIP; three
historical S3-R01/F03/F04 studies are explicitly excluded and not fixed.
Build/vet, UI runners, targeted C2–C4 runs, the ShellCheck profile, and its failure
checks passed. Details and external limitations are in [acceptance.md](acceptance.md).
APK/IPK packages were not rebuilt; no new install/device checks were performed.

The separate X analysis is also complete: two Sol 6.1 High analysts prepared a
subtotal, then a separate Terra 6.1 Max performed the critique.
[Summary](../improvements-X/final.md): one proposal kept, ten refined,
three excluded for now; [critique](../improvements-X/terra-critique.md).
Proposals are not implemented; product composition and positioning are preserved.
Models and effort confirmed; IDs/links are in [assignments.json](assignments.json)
and the [X registry](../improvements-X/assignments.json).
