# Version PAC URL for browser refresh

Upstream List Source changes and HTTPS Interception changes must reach the browser, not only update the PAC content served by the gateway. Some browsers may keep using a cached PAC file or cached proxy decision while its URL is unchanged.

Gateway publishes the complete desired PAC input after applying the runtime change. Managed PAC renders and compares the effective PAC, then assigns its own publication generation in a URL such as `http://127.0.0.1:<pac-port>/seamless-cors.pac?v=<generation>` when a browser-visible publication is needed.

Managed PAC applies the new URL only to eligible members of the fixed Managed PAC Service Set. It manages marker-owned or empty settings, reports foreign settings as drift without changing them, and does not expand the set to newly visible services.
