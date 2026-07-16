# Check Managed PAC Lease at boundaries

Managed PAC Lease is checked and reconciled at explicit lifecycle, PAC refresh, and status boundaries rather than by continuous idle OS proxy polling or platform event watching.

This avoids repeatedly inspecting machine proxy settings while the gateway is not acting. A temporarily absent selected service remains in the fixed Managed PAC Service Set without losing the lease, and newly visible unselected services are ignored. At the next boundary, a visible selected service carrying any seamless-cors Managed PAC Ownership Marker is reattached to the session's current PAC URL; empty or foreign state causes the Gateway Owner to report `managed-pac-lease-lost` instead of taking over that state. Temporary drift while the gateway is idle is accepted for this DEV/QA lifecycle.
