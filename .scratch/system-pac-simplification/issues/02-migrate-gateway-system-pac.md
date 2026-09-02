# Migrate Gateway start, delivery, status, and reports

Status: ready-for-agent

Depends on: `01-build-system-pac-module.md`

## Outcome

Make Gateway coordinate the System PAC module directly, without activation assessment or a retained PAC Control handle, and make PAC problems reportable nonfatal runtime state.

## Scope

- Replace `managedPACCapabilities`, activation/footprint fields, and `activeRuntime.managedPAC` with one System PAC module dependency.
- Remove pre-start PAC cleanup and Managed PAC assessment from Start Sequence. Begin Gateway Runtime first, publish it, then request initial delivery.
- Keep the runtime active and return fulfilled Started guidance when discovery, observation, mutation, or verification fails or when no service is safely mutable.
- Make repeated start against an active runtime synchronously request another delivery without replacing the runtime.
- On every accepted delivery, replace the one retained latest delivery report, including a partial or failed report.
- Define Gateway-owned System PAC Report and per-service issue classifications from System PAC facts and concrete errors. Do not export aliases of System PAC or Network Service types through Gateway results.
- Make start guidance and runtime status include every visible Network Service. Remove fixed service-set, manageable, active-control, and drift concepts.
- Make status call fresh `Observe`, passing the active PAC Endpoint or nil when ownerless. Use fresh observation for current Routes Current Endpoint and Traffic Routing Ready; label retained delivery diagnostics as historical rather than current.
- Remove `StartNoManageablePACServices` and `StartManagedPACSetFailed` command outcomes and their fulfillment mappings.

## Acceptance criteria

- Start is fulfilled and runtime remains reachable when all delivery writes fail, discovery fails, or every visible setting is foreign or unobservable.
- Start guidance reports the attempted delivery and all available per-service facts.
- Repeated start causes a new generation and attempt while preserving listener addresses and the active runtime.
- Status discovers newly visible services and current foreign/owned transitions without waiting for delivery.
- Current status never claims a retained historical delivery failure is still current when fresh observation disproves it.
- Gateway—not System PAC—owns transport-neutral report classification and Traffic Routing Ready.

## Regression stories

- Protect runtime availability: inability to configure the browser must not tear down a usable gateway endpoint that a later delivery can repair.
- Protect repair behavior: repeated start must provide an explicit retry even when Traffic Projection did not change.
- Protect diagnostic truth: historical delivery failures and current operating-system state must not be conflated.
- Protect module depth: Gateway tests use the three-operation seam and do not reconstruct PAC ownership from URLs.
