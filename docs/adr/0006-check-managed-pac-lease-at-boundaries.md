# Check Managed PAC Lease at boundaries

Managed PAC Lease is checked at explicit lifecycle, PAC refresh, and status boundaries rather than by continuous idle OS proxy polling.

This avoids repeatedly inspecting machine proxy settings while the gateway is not acting, while still protecting PAC refresh from overwriting or assuming ownership after a user or another tool has disabled or replaced seamless-cors managed PAC state. If lease loss is detected at a refresh boundary, the Gateway Owner reports `managed-pac-lease-lost` instead of continuing in a warning-only state.
