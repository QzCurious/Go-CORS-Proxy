# Managed PAC Service Set is fixed per owner run

Gateway Activation selects a Managed PAC Service Set from the visible platform services during start.

That service set is fixed for the Gateway Owner run. PAC install, PAC URL refresh, and Managed PAC Lease checks stay scoped to the selected services instead of silently expanding to services that appear later. Newly appearing services can be included by a later gateway activation.

This keeps one owner run stable and explainable: start consent and start guidance describe the services the gateway is taking responsibility for, runtime status can report that same scope, and later platform changes do not broaden the gateway's managed footprint without another activation.

Gateway Footprint Cleanup remains marker-based rather than service-set-based. Cleanup inspects current OS state and removes seamless-cors-owned PAC settings by ownership marker, so it can clean owned state even when the active service set has changed or no owner is running.
