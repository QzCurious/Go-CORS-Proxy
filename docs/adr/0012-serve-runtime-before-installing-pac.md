# Serve runtime before installing PAC

**Status:** accepted; unconditional initial UserCA inspection superseded by ADR-0027

After Managed PAC Consent, Gateway Activation prepares and publishes Gateway Runtime and begins serving proxy and PAC listeners before installing the Generated PAC into the accepted Managed PAC Service Set. HTTPS readiness is derived from the freshly inspected Installed UserCA and is adopted by the runtime before traffic begins.

This prevents the OS from pointing at an endpoint that is not ready. Managed PAC retains the accepted service set even when an initial platform publication reaches no service, records the failure internally, and retries while Gateway Runtime continues serving. Complete Managed PAC Uninstall still removes any partial marker-owned state when activation is cancelled or ownership ends.
