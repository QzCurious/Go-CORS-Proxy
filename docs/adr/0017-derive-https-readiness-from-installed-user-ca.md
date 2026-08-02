# Derive HTTPS readiness from Installed UserCA

`upstreams.txt` is the only user configuration source. Gateway start assesses HTTPS Readiness from the Installed UserCA without installing or repairing trust, keeps HTTP service running when readiness is not ready, and warns when explicit HTTPS Intent is unmet or owned UserCA state is unhealthy. Ready HTTPS Readiness activates HTTPS routing for both HTTPS Origin Selectors and Host Selectors, while near-expiry remains ready with a renewal warning.

Readiness is latched for the runtime: Upstream List edits do not revalidate UserCA. An explicit UserCA install returns a fresh snapshot, which Gateway Runtime adopts immediately for HTTPS Readiness Recovery. If that adoption changes the desired PAC URL Version, Gateway submits an independent non-blocking Managed PAC reconciliation request; the UserCA command does not wait for PAC writes.
