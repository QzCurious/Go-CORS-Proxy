---
status: accepted
---

# Derive HTTPS readiness from Installed UserCA

`upstreams.txt` is the only user configuration source; the former `config.yaml` and `ca-trusted` toggle are removed. Gateway start assesses HTTPS Readiness from the Installed User CA without installing or repairing trust, keeps HTTP service running when readiness is not ready, and warns when explicit HTTPS Intent is unmet or owned UserCA state is unhealthy. Ready HTTPS Readiness activates HTTPS routing for both HTTPS Origin Selectors and Host Selectors, while near-expiry remains ready with a renewal warning. Readiness is latched for the runtime: Upstream List edits do not revalidate UserCA, and the existing `install` command performs immediate HTTPS Readiness Recovery and PAC refresh without a separate endpoint.

This supersedes ADR-0008 and ADR-0010. It also replaces the `ca-trusted`, start-time CA Ensure, CA Ensure Result, and Trusted Runtime Admission clauses of ADR-0011 through ADR-0014. ADR-0018 extends this decision with runtime interception failure handling, live UserCA rotation and uninstall, deferred non-active cleanup, and CA-operation concurrency.
