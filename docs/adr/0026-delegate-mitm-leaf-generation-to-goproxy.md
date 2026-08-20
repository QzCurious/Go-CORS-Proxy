# Delegate MITM leaf generation to goproxy

**Status:** accepted; intent-independent readiness and proxy-generation lifecycle placement superseded by ADR-0027

CORS Proxy treats every CONNECT reaching its loopback listener alike: when HTTPS Readiness is ready it returns a goproxy MITM action built with `TLSConfigFromCA`, and when readiness is not-ready it returns a direct-tunnel action. The Upstream List controls only PAC Routing and never proxy admission or certificate scope. UserCA returns the validated Active UserCA `tls.Certificate` with its coherent assessment, without projecting list-bounded providers or self-testing leaf generation; goproxy owns per-host leaf generation and connection-local signing or handshake failures.

Gateway keeps one stable proxy listener and atomically publishes one immutable goproxy handler generation for each adopted UserCA generation. Every active handler owns its CA-bound CONNECT action and a fresh concurrent LRU `CertStore` bounded to 1,024 hostnames, so cached certificates never cross CA generations; admitted and established connections retain the old handler without draining, while new requests on the same listener use the replacement. Gateway alone schedules the assessment expiry deadline. PAC is republished only when semantic routes change: CA rotation while already ready preserves the endpoint and PAC identity, while readiness recovery activates the new handler before publishing HTTPS routes. With provider projection and global provider failures removed, HTTPS Readiness is the only runtime HTTPS state; the separate interception state, failure warnings, provisioning dispositions, and list-change certificate consequences are removed.

This supersedes ADR-0025, ADR-0022's provider and request-boundary expiry decisions, ADR-0019's assessment-provider and leaf-certificate decisions, ADR-0018's provider replacement and separate interception-state decisions, and ADR-0017's opaque-provider and provider-deadline decisions. Their remaining UserCA lifecycle, Gateway coordination, readiness, atomic rotation, and no-drain decisions remain accepted.

## Considered Options

- Pre-generate certificates only for Upstream List identities. This duplicated goproxy signing, coupled certificate capability to PAC policy, and made the general loopback proxy behave differently for direct clients.
- Preserve direct-tunnel fallback after certificate-generation failure. goproxy generates after accepting CONNECT, so failure is necessarily a failed client TLS connection unless certificate lookup remains a custom pre-MITM responsibility.
- Mutate one goproxy server's CA and clear its hostname-only cache in place. Concurrent CONNECT handling can mix old signing callbacks with the cleared cache; replacing the complete handler generation makes CA and cache publication one atomic operation without replacing the listener or port.
