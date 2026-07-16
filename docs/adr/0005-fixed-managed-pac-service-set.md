# Managed PAC Service Set is fixed per owner run

Gateway Activation selects a Managed PAC Service Set from the visible platform services during start.

That service set is fixed for the Gateway Owner run. PAC install, PAC URL refresh, and Managed PAC Lease checks stay scoped to the selected services instead of silently expanding to services that appear later. A selected service remains a member while temporarily absent, absence alone does not lose the Managed PAC Lease, and newly appearing services remain outside the set until a later gateway activation. When a selected service reappears with a seamless-cors Managed PAC Ownership Marker, the session reattaches it by installing its current PAC URL; empty or foreign PAC state loses the lease instead of being taken over.

Gateway Activation succeeds when PAC installation reaches at least one selected service. Partial absence does not shrink the set, but installation reaching no selected service fails activation so Start Guidance never claims active Managed PAC state when no service was configured.

This keeps one owner run stable and explainable: PAC Replacement Consent Detail lists the entire proposed set while identifying its foreign PAC entries under one collective accept-or-decline decision. Consent never narrows the set; Start Guidance and runtime status report the same scope, and later platform changes do not broaden the gateway's managed footprint without another activation.

Gateway Footprint Cleanup remains marker-based rather than service-set-based. Cleanup inspects current OS state and removes seamless-cors-owned PAC settings by ownership marker, so it can clean owned state even when the active service set has changed or no owner is running.
