# Derive HTTPS readiness from Installed UserCA

**Status:** accepted

`upstreams.txt` is the only user configuration source. Gateway start assesses HTTPS Readiness from a coherent UserCA Assessment without installing or repairing trust, keeps HTTP service running when readiness is not ready, and warns when explicit HTTPS Intent is unmet or owned UserCA state is unhealthy. A usable assessment includes an opaque, self-tested HTTPS Certificate Provider; Gateway passes it to CorsProxy, while the status portion remains certificate-free. Ready HTTPS Readiness activates HTTPS routing for both HTTPS Origin Selectors and Host Selectors, while near-expiry remains ready with a 90-day renewal warning.

Readiness is latched for the runtime: Upstream List edits do not revalidate UserCA. An explicit UserCA install returns a fresh assessment, which Gateway Runtime adopts immediately by atomically replacing the CorsProxy provider. If that adoption changes the complete Managed PAC desired state, Gateway publishes it independently; the UserCA command does not wait for PAC writes. Gateway owns a timer for the provider's validity deadline; the timer emits a signal, Gateway freshly reassesses UserCA, and only an expired, unusable, or unassessable assessment deactivates HTTPS and withdraws HTTPS PAC routes. The provider repeats the expiry check at issuance as a safety backstop.
