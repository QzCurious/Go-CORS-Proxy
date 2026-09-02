# Simplify runtime signaling

Status: ready-for-agent

Depends on: `02-migrate-gateway-system-pac.md`

## Outcome

Replace the generic conflated runtime streams with two explicit ordinary channels: System PAC Delivery Requests and UserCA assessment requests.

## Scope

- Replace `deliveryPublisher`/`deliveryStream` with an ordinary unbuffered delivery-request channel. Each accepted effective Traffic Projection change must backpressure until Gateway receives its distinct request.
- Replace `RuntimeChangeKind` and its stream with a dedicated ordinary UserCA-assessment request channel.
- Delete `RuntimeStatusChanged`; status reads retained runtime facts and fresh System PAC Observation directly, so no notification is required.
- Remove priority/conflation code and `publishMu` where it no longer protects another invariant.
- Ensure cancellation unblocks publishers and consumers during Stop; do not add a goroutine-per-send, arbitrary buffer, drop policy, or queue abstraction.
- Remove `internal/lib/conflatedstream` after its last production use disappears.

## Acceptance criteria

- Two effective Traffic Projection changes produce two delivery calls in order, even when the first delivery is slow.
- A warning-only or otherwise behavior-equivalent source update produces no delivery request.
- UserCA assessment requests remain independent of delivery and cannot be overwritten by status-only changes.
- Stop cancellation cannot strand an Upstream List watcher on a channel send.
- No production or test package imports `internal/lib/conflatedstream`, and the directory is deleted.

## Regression stories

- Protect event identity: one accepted Traffic Projection change must not erase another delivery attempt and publication generation.
- Protect bounded concurrency: backpressure must serialize work without creating an implicit unbounded queue.
- Protect shutdown: blocked producers must exit when their runtime context is cancelled.
- Protect UserCA recovery: removing generic runtime-change conflation must not lose a required reassessment.
