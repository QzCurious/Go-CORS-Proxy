# Cleanup only seamless-cors-owned PAC state

Gateway Footprint Cleanup removes managed PAC settings only when the current machine state carries the seamless-cors Managed PAC Ownership Marker.

This keeps cleanup scoped to state the gateway owns. Foreign PAC settings require PAC Replacement Consent before start can overwrite them, and cleanup must not infer ownership from history, cached snapshots, or guessed previous machine state.

The consequence is a clean-break lifecycle: cleanup is marker-based and idempotent, and user-authored PAC state remains outside the gateway cleanup boundary.
