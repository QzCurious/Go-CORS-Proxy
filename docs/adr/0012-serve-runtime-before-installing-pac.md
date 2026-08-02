# Serve runtime before installing PAC

After Managed PAC Consent, Gateway Activation prepares and publishes Gateway Runtime and begins serving proxy and PAC listeners before installing the Generated PAC into the accepted Managed PAC Service Set. HTTPS readiness is derived from the freshly inspected Installed UserCA and is adopted by the runtime before traffic begins.

This prevents the OS from pointing at an endpoint that is not ready. Start succeeds only after Managed PAC installation reaches at least one accepted service. If installation reaches none, Gateway closes the runtime and invokes Complete Managed PAC Uninstall to remove any partial marker-owned state.
