# Preserve the live owner during start cleanup

Start cleanup is ownership-aware rather than an unconditional removal of every gateway footprint. Direct start acquires Gateway Ownership before removing stale Gateway State Cache and marker-owned PAC state, then publishes its discovery cache. Router-hosted start preserves the existing owner's live cache and router throughout Managed PAC Consent and activation failure. Stop removes discovery state only when ownership is intentionally ending.

This prevents competing direct starts from cleaning or publishing concurrently and lets an explicitly started router remain available after a terminal start outcome, including `no-manageable-pac-services`.
