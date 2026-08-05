# Fix the consent-selected Managed PAC Service Set per runtime

Every Gateway Runtime activation inspects all visible network services. Marker-owned and empty services are manageable and proposed together for Managed PAC Consent; foreign services are shown as excluded. Consent accepts the entire nonempty proposal rather than selecting individual services, and the accepted names become the fixed Managed PAC Service Set for that runtime.

Initially foreign, excluded, and newly appearing services cannot join until another activation. An accepted service remains in the set if it later disappears or becomes foreign. Managed PAC freshly classifies fixed-set members whenever it acts: marker-owned or empty members are manageable, foreign members are left untouched, and absent members remain selected for a later operation.

Initial installation attempts every currently manageable visible member and retains the complete accepted set even when a transient platform failure updates none. Per-service exceptions are reported as Managed PAC Warnings, while Managed PAC retains the desired state and retries the initial publication internally. A proposal containing no manageable service still produces the terminal `no-manageable-pac-services` outcome without consent or runtime activation.

Activation creates Managed PAC Runtime State containing the fixed service names and desired PAC URL even when the first publication is temporarily unhealthy. Absence of that state means inactive. Presence means Managed PAC is active even if later drift leaves every selected service uncontrolled; current warnings qualify that active lifecycle without creating a persistent lease-loss state.
