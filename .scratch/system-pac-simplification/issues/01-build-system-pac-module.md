# Build the System PAC module

Status: ready-for-agent

## Outcome

Introduce `internal/systempac` as the single deep module for System PAC Delivery, System PAC Observation, and System PAC Cleanup. Keep `internal/lib/networkservice` unchanged as its platform-mechanics dependency.

## Scope

- Define the three-operation `Module` interface and purpose-specific Endpoint, State, CleanupState, per-service fact, ownership, and concrete error types.
- Let `Observe` accept an optional endpoint. It must always return all discoverable service facts and only establish Routes Current Endpoint when an endpoint is supplied.
- Internally serialize operations that could otherwise observe or mutate overlapping OS PAC state.
- For each delivery, allocate a monotonic publication generation before discovery; discover and observe all current services; select only empty or marker-owned settings; write the generated ownership-marked URL; then freshly observe all services for verification.
- Return partial facts with concrete discovery, observation, mutation, and verification errors. Preserve ordinary wrapped causes and service identity.
- For cleanup, discover and observe every current service, disable every active marker-owned setting, then verify. Preserve foreign, disabled, and unobservable settings. Any uncertainty makes cleanup unfulfilled even when other services were cleaned successfully.
- Reuse the existing ownership URL rules and Network Service adapters. Do not add a Control handle, activation assessment, fixed service set, background worker, or caller-visible Network Service adapter.
- Replace the behavior-focused tests in `internal/managedpac` with System PAC interface tests; retain only tests with a current regression story.

## Acceptance criteria

- A service appearing after an earlier delivery is eligible on the next delivery.
- A formerly foreign or unobservable service becomes eligible after it is later observed empty or owned.
- Every delivery, including a fully failed one, consumes exactly one distinct publication generation.
- A delivery never overwrites foreign PAC and never rolls back another service's successful update.
- Returned state reflects fresh post-write verification rather than assumed mutation success.
- Ownerless observation works without an endpoint and does not claim Routes Current Endpoint.
- Cleanup leaves disabled owned URLs and all foreign settings untouched.
- Discovery or per-service observation, mutation, or verification uncertainty produces the corresponding concrete error while retaining available facts.

## Regression stories

- Protect ownership safety: a corporate PAC must never be replaced or disabled.
- Protect dynamic recovery: later delivery must adopt newly safe services without restarting the module.
- Protect truthful state: successful OS commands must not be treated as verified state without rereading the setting.
- Protect cleanup truth: an unobservable service can hide active residue and therefore cannot yield fulfilled cleanup.
