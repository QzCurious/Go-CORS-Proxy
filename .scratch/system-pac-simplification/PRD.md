# Simplify PAC integration with System PAC

Status: ready-for-agent

## Goal

Replace the activation-scoped Managed PAC design with a deep System PAC module that safely maintains current-user PAC settings through three operations—delivery, observation, and cleanup—while Gateway owns lifecycle triggers and user-facing reports.

The change is a clean break. It should delete obsolete types and behavior rather than preserve compatibility aliases or fallback paths.

## Product behavior

- `start` begins serving Gateway Runtime before its initial System PAC Delivery. Delivery problems are reported but never undo or prevent the running runtime.
- Initial start, every effective Traffic Projection change, and repeated start against an active runtime each produce one serialized delivery attempt and one new publication generation.
- Every delivery freshly discovers all visible Network Services, observes before writing, mutates only empty or marker-owned PAC settings, and verifies after writing.
- Foreign and unobservable PAC settings are never changed. Foreign PAC is ordinary excluded state, not drift or failure.
- `status` freshly observes every visible Network Service without mutation. It can determine Routes Current Endpoint only while a current runtime endpoint exists.
- Every `stop` attempts owned PAC and Gateway State Cache cleanup, including when the owner is router-only or absent. With a live runtime, PAC cleanup happens while its endpoints still serve.
- Stop remains fulfilled after its best-effort cleanup attempts even when the cleanup sub-operation is unfulfilled; the result prominently reports possible residue.
- Disabled marker-owned PAC URLs are inert, count as clean, and remain stored.
- There is no separate cleanup command, no previous-setting restoration, no fixed service set, no background PAC polling, and no delivery conflation.

## Architecture

`internal/systempac` is the deep module. Its external interface is conceptually:

```go
type Module interface {
	Deliver(context.Context, Endpoint) (State, error)
	Observe(context.Context, *Endpoint) (State, error)
	Cleanup(context.Context) (CleanupState, error)
}
```

`Observe` accepts a nil endpoint for ownerless status. System PAC owns all PAC policy and returns available module facts plus concrete errors. Gateway classifies those values into Gateway-owned reports and command semantics. `internal/lib/networkservice` remains the platform-neutral external-system module used only behind System PAC.

Gateway owns the three delivery triggers, lifecycle ordering, the current-vs-historical distinction, and cross-feature Traffic Routing Ready. Gateway Runtime retains only the latest delivery report. A fresh status observation supersedes that historical report for claims about current OS state.

## Non-goals

- Replacing or absorbing `internal/lib/networkservice`.
- Managing foreign PAC settings or restoring a previous PAC value.
- Adding service selection, PAC consent, a cleanup command, polling, retries, or an arbitrary delivery queue.
- Preserving Managed PAC result names, transport codes, or internal interfaces for compatibility.
- Changing PAC Routing, Traffic Projection equivalence, proxy behavior, or UserCA trust policy beyond the signaling seam required here.

## Acceptance criteria

- No production package or user-facing model refers to Managed PAC, its Control lifetime, activation assessment, fixed service set, drift warning, or No Manageable PAC Services.
- System PAC behavior is tested through its three-operation interface, including partial failures and ownership safety.
- Gateway starts and remains active when every PAC write fails or no setting is safely mutable.
- Repeated start and each effective Traffic Projection change produce distinct serialized delivery attempts.
- Current status includes all visible Network Services and does not present a retained delivery error as current OS truth.
- Stop cleanup runs for start-hosted, router-only, and ownerless cases; a live runtime remains reachable through PAC cleanup.
- Cleanup uncertainty is reported as an unfulfilled cleanup sub-operation without making Stop unfulfilled or nonzero at the CLI.
- `internal/lib/conflatedstream` and `RuntimeStatusChanged` are removed; UserCA assessment uses a separate ordinary channel.
- `go test ./...` and `make cross-build` pass.

## Delivery order

1. [Build the System PAC module](issues/01-build-system-pac-module.md)
2. [Migrate Gateway start, delivery, status, and reports](issues/02-migrate-gateway-system-pac.md)
3. [Implement universal best-effort Stop cleanup](issues/03-universal-stop-cleanup.md)
4. [Simplify runtime signaling](issues/04-simplify-runtime-signaling.md)
5. [Remove obsolete surfaces and verify the clean break](issues/05-remove-obsolete-surfaces.md)
