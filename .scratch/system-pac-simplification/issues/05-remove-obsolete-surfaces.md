# Remove obsolete surfaces and verify the clean break

Status: ready-for-agent

Depends on: `02-migrate-gateway-system-pac.md`, `03-universal-stop-cleanup.md`, `04-simplify-runtime-signaling.md`

## Outcome

Finish the clean break across transports, CLI output, documentation, package names, and tests, leaving no compatibility layer for Managed PAC semantics.

## Scope

- Delete `internal/managedpac` after all imports move to `internal/systempac`.
- Remove obsolete Gateway result kinds, JSON fields/codes, CLI branches, test fixtures, and vocabulary for Managed PAC Active, fixed services, manageability, drift, Control Close, and terminal PAC start failure.
- Add transport and CLI representations for Gateway-owned System PAC reports, including every visible Network Service, current-vs-historical labeling, and cleanup-sub-operation issues.
- Ensure fulfilled Stop with incomplete cleanup renders prominently but returns a successful CLI exit.
- Update README and README.zh-TW command/status descriptions to match ADR-0012, especially ownerless cleanup and nonfatal start delivery.
- Prune tests whose only purpose is preserving deleted structure. Consolidate replacement tests around the System PAC seam, Gateway lifecycle outcomes, transport round trips, CLI rendering, and platform adapters.
- Run formatting and the full verification matrix.

## Acceptance criteria

- `rg` finds no production references to `managedpac`, Managed PAC, No Manageable PAC Services, Managed PAC Active, RuntimeStatusChanged, or conflatedstream except historical text inside superseded ADRs.
- CLI and HTTP round trips preserve the new Gateway report semantics without parsing diagnostic prose.
- README command behavior agrees with `CONTEXT.md` and ADR-0012.
- Existing Network Service adapter tests remain intact and no adapter is absorbed into System PAC.
- `gofmt` is clean, `go test ./...` passes, and `make cross-build` passes for supported Darwin and Windows targets.

## Regression stories

- Protect surface consistency: direct, routed HTTP, and CLI commands must describe the same Gateway result.
- Protect the clean break: obsolete result codes or aliases must not allow old semantics to survive accidentally.
- Protect platform coverage: the refactor must remain cross-compilable with complete Darwin and Windows Network Service adapters.
- Protect test signal: deleted structural tests must be replaced only where a current observable contract or safety property needs coverage.
