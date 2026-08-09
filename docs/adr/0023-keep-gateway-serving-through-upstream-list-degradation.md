# Keep Gateway serving through Upstream List degradation

**Status:** accepted

Upstream List source failure is non-fatal to Gateway Runtime. Gateway starts with an empty effective list before receiving any `ListAccepted`, thereafter retains the last list it adopted when `DiagnosticReported` arrives, and keeps CORS Proxy and PAC serving while presenting the underlying source or observation cause. Source publishes wholesale-conflated `ListAccepted` or `DiagnosticReported` transitions: healthy semantic no-ops are suppressed, diagnostics require no equality comparison, and the first valid projection after degradation always produces `ListAccepted` so Gateway can converge even when an earlier valid transition was replaced.

seamless-cors never repairs or rewrites the user-managed `upstreams.txt`. Missing-file creation is an explicitly disclosed, fingerprint-bound Start consent presented separately from and before independent Managed PAC Consent; acceptance performs an immediate exclusive best-effort creation that is not rolled back, while decline or failure continues Start in degradation. Active observation detects later user correction, whereas terminal observation preserves its cause and requires repair plus Gateway restart without stopping the already-serving runtime.
