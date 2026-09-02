# Implement universal best-effort Stop cleanup

Status: ready-for-agent

Depends on: `01-build-system-pac-module.md`, `02-migrate-gateway-system-pac.md`

## Outcome

Make every `stop` terminate any live Gateway Owner and attempt System PAC plus Gateway State Cache cleanup, while preserving fulfilled Stop semantics when cleanup is incomplete.

## Scope

- Route a live-owner stop through that owner; when no owner is reachable, perform the same Gateway Footprint Cleanup locally.
- Apply cleanup to start-hosted and router-only `serve` owners even though `serve` does not install PAC.
- During live-runtime stop, reject/cancel new delivery requests, quiesce any admitted delivery, run System PAC Cleanup while PAC and Proxy endpoints still serve, then close traffic and finish owner teardown.
- Continue Gateway State Cache cleanup and all remaining teardown after any System PAC cleanup error.
- Represent discovery, observation, mutation, verification, and observed-residue failures as an unfulfilled cleanup sub-operation in the Stop result.
- Keep Stop itself fulfilled and the CLI exit successful after best-effort attempts, regardless of cleanup completion. Prominently render possible PAC residue and manual-correction detail.
- Preserve Installed User CA state and existing admitted-CA-mutation coordination.

## Acceptance criteria

- Start-hosted, router-only, and ownerless stop each invoke System PAC Cleanup exactly once.
- A live PAC endpoint remains reachable while cleanup is blocked and closes after cleanup settles.
- Stop cancels pending delivery triggers and prevents any delivery from publishing PAC after cleanup begins.
- One cleanup subject failing does not skip another cleanup subject or preserve owner state.
- A cleanup error yields fulfilled Stop with an unfulfilled cleanup detail and successful CLI exit.
- A second forced cancellation can still terminate immediately under the existing foreground rule.

## Regression stories

- Protect safe ordering: browsers must not be left pointing at an endpoint that Stop closed before attempting to disable PAC.
- Protect crash recovery: ownerless Stop must clean marker-owned residue left by a dead process.
- Protect command meaning: `stop` must clean owned footprint even when the stopped owner was only `serve`.
- Protect terminal shutdown: cleanup failure must not resurrect or retain Gateway Ownership.
