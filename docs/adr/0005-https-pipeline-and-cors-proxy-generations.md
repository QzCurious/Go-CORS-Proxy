# HTTPS Pipeline and CORS Proxy generations

Only an effective HTTPS Origin Selector creates HTTPS Intent and admits an HTTPS Pipeline. Without intent, Gateway performs no UserCA assessment for runtime use, retains no signing material or expiry deadline, publishes no MITM generation, and includes no HTTPS PAC routes; Host Selectors contribute HTTPS routes only while an intent-admitted pipeline is ready.

An admitted pipeline assesses coherent UserCA facts asynchronously and is either assessing or settled. A usable assessment with matching signing material publishes an immutable CA-backed CORS Proxy generation before Gateway adds HTTPS PAC routes; an unmet capability, assessment failure, or signing-material inconsistency keeps CONNECT direct and exposes exactly one source-specific current detail without making Start unfulfilled.

When intent disappears or readiness is lost, Gateway first serves and enqueues the PAC Projection without HTTPS routes, then publishes a direct CORS Proxy generation and discards retained signing material. Pipeline work is generation-scoped so results from cancelled, removed, or replaced assessments are ignored, and Gateway schedules expiry reassessment only for an active ready pipeline.

CORS Proxy owns immutable direct and goproxy MITM handler generations. Gateway owns the stable listener, server, outbound transport, atomic generation publication, and lifecycle; goproxy owns per-host leaf generation, connection-local signing and handshake failures do not change Gateway state, and generation replacement does not drain admitted or established connections.

CA Lifecycle Commands remain independent of HTTPS Intent. Install has a runtime consequence only when a pipeline exists, while uninstall removes HTTPS PAC routes and publishes direct behavior before deleting trust and material.
