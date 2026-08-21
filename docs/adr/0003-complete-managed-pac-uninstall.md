# Separate Managed PAC active-state cleanup from uninstall

Managed PAC active-state cleanup and Managed PAC uninstall are distinct operations. Gateway Footprint Cleanup invokes active-state cleanup directly: it inspects current OS proxy settings, disables every enabled setting carrying the seamless-cors Managed PAC Ownership Marker, and verifies that none remains active without changing reconciliation admission. Startup cleanup therefore does not masquerade as lifecycle teardown.

`Uninstall` remains Managed PAC's complete teardown barrier. It closes reconciliation admission, cancels and discards pending work, waits until the current writer can perform no later platform write, and then performs the same active-state cleanup. Requests arriving after teardown begins are discarded until a later successful `Install` reopens reconciliation admission.

Both operations disable currently enabled owned settings regardless of publication generation or whether the exact URL changed after installation. Ownership identity authorizes disabling; an earlier settings snapshot does not. A disabled owned URL is inert retained configuration rather than cleanup residue, and foreign settings are never modified. Gateway clears Managed PAC Runtime State to absence only after successful uninstall, so no separate `Close` operation, stateful session, or external PAC mutex is needed.

Active-state cleanup attempts mutation with the current process authority and reports authorization failure as a concrete cleanup failure. It does not initiate privilege escalation or an administrator authorization flow; adding such a flow is a separate security and distribution decision.

Gateway Footprint Cleanup, including startup and ownerless stale-footprint cleanup, invokes active-state cleanup. Stop and activation rollback invoke `Uninstall` because a Managed PAC lifecycle may have admitted reconciliation and must first quiesce its writers.

Cleanup attempts every observed active owned service even after an earlier service fails, then verifies the resulting active state. It reports concrete per-service mutation failures together with any active owned services that remain rather than returning only the first platform error.

Ownership remains a property of the PAC URL marker, independently from enabled state. Cleanup and Gateway Footprint Cleanup classify residue through active ownership: a service is active and owned only when it is enabled and its URL is marker-owned. Platform adapters may realize the inactive postcondition differently; macOS retains the URL and disables PAC, while Windows removes `AutoConfigURL` because URL absence is that platform's inactive representation.

Managed PAC exposes a concrete aggregate cleanup error containing sorted per-service inspection or disable failures, a final verification failure when state cannot be re-inspected, and the sorted active owned services that remain. Gateway owns the caller-level classification as one Managed PAC cleanup-subject failure and never parses diagnostic text.

On macOS, `networksetup` cannot conditionally disable a service based on its current PAC URL. The adapter therefore inspects each service immediately before disabling it, serializes Managed PAC's own mutations, and verifies afterward; it accepts the narrow race with unrelated external settings writers rather than introducing unsupported preference editing or abandoning pure-Go distribution.

Active-state cleanup serializes with other Managed PAC mutations and rejects the operation with a distinct concrete error when reconciliation admission is open. It never silently shuts down an active lifecycle; callers in that state must invoke `Uninstall`. A disabled retained URL remains marker-owned in inspection and status while its disabled fact makes it irrelevant to Gateway Footprint Cleanup status.
