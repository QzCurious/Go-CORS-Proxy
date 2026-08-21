# Gateway ownership and lifecycle

One Gateway Owner holds the Gateway Ownership Lease and publishes Gateway Router discovery state. An ownerless `start` hosts the Router and Gateway Runtime, `serve` hosts only the Router, and a command finding a reachable owner routes through that owner instead of competing for ownership; ownerless CA lifecycle work uses a discoverable, non-promotable Transient Gateway Owner, while ownerless status briefly holds the lease without publishing discovery state.

Start has no read-only plan. It returns each independently required, fingerprint-bound consent detail in fixed order and accepts a retry carrying accumulated decisions; direct-start failure ends the newly created owner, while failure of a start routed to a Router-Only Serve owner preserves that owner. Runtime listeners are serving before Managed PAC publication can direct browser traffic to them.

Stop takes precedence over Start, rejects new lifecycle work, closes traffic before durable cleanup, waits for admitted owner-owned CA mutation, and attempts every cleanup subject even after one fails. Stop is terminal and best-effort: it truthfully returns an unfulfilled cleanup result when durable residue remains, but Router shutdown, discovery removal, lease release, and process exit continue; a later ownerless command verifies stale discovery and active owned PAC state and cleans it where that command permits.

Graceful process cancellation invokes the same Stop operation. A second cancellation may force immediate exit, and unexpected Router termination also invokes Stop before the process exits; these paths do not introduce a retrying owner or a Router-only fallback.

