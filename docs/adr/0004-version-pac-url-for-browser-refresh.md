# Version PAC URL for browser refresh

Live Configuration must reach the browser, not only update the PAC content served by the gateway.

Some browsers may keep using a cached PAC file or cached proxy decision while the PAC URL is unchanged. After a hot-applicable route change, the Gateway Owner therefore refreshes already-owned Managed PAC state with a new PAC URL version such as `http://127.0.0.1:<pac-port>/seamless-cors.pac?v=<version>`.

Versioning the URL gives the browser a new PAC resource to pick up while keeping the PAC endpoint stable. The refresh may mutate only seamless-cors-owned Managed PAC state; it must not overwrite foreign PAC settings or bypass PAC Replacement Consent.
