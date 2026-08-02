# Execute start without a Start Plan

Start clients call the Gateway Module start operation directly without a separate read-only Start Plan. The first attempt inspects Managed PAC state and returns Managed PAC Consent Detail without mutating. That detail lists every visible network service, labels marker-owned or empty services as manageable, labels foreign services as excluded, and proposes the complete nonempty manageable set.

The caller renders the detail and retries with the sorted accepted service names and a fingerprint derived only from those names. Gateway validates that the fingerprint describes the submitted names and fixes that accepted set for the runtime; it does not bind consent to PAC URLs or request another consent round because machine state changed. Installation freshly classifies each accepted service, manages marker-owned or empty state, and reports foreign or failed members as warnings. Services outside the accepted set cannot join until another activation.

If the proposed manageable set is empty, start returns the terminal `no-manageable-pac-services` outcome without consent or runtime activation. A direct start process exits because it has no service to provide, while a router-hosted failed attempt preserves the explicitly requested router-only owner.
