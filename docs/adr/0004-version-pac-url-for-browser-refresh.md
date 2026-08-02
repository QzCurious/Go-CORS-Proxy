# Version PAC URL for browser refresh

Live Configuration changes must reach the browser, not only update the PAC content served by the gateway. Some browsers may keep using a cached PAC file or cached proxy decision while its URL is unchanged.

Gateway Runtime therefore assigns a new desired PAC URL Version after a hot-applicable routing change, such as `http://127.0.0.1:<pac-port>/seamless-cors.pac?v=<version>`. Gateway applies the runtime change first and then submits one non-blocking reconciliation request to Managed PAC.

Managed PAC applies the new URL only to eligible members of the fixed Managed PAC Service Set. It manages marker-owned or empty settings, reports foreign settings as drift without changing them, and does not expand the set to newly visible services.
