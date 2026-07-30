---
status: accepted
---

# Rotate UserCA with atomic HTTPS generations

Live UserCA renewal uses immutable fingerprint-named authority generations, one atomic `active-fingerprint` pointer, and an atomically swapped runtime HTTPS Generation containing the certificate, signer, and generation-owned leaf cache. Candidate trust is installed beside Active before the marker commit, each new TLS handshake loads one generation once, and established connections are untouched; install succeeds when Candidate is trusted and durably Active, plus adopted when a live runtime remains, while the previous authority becomes non-active cleanup residue.

Non-Active UserCA Cleanup is deferred and fallible: it is attempted before another rotation, during explicit install, always during uninstall, and best-effort during graceful termination. Cleanup failure cannot disable active HTTPS or block termination, but it prevents another rotation from accumulating a third root and exposes a current typed warning; startup never mutates trust and serves only the valid authority named by `active-fingerprint`, while confirmed uninstall is the operation that guarantees removal of every owned UserCA.

HTTPS Readiness represents UserCA capability, while HTTPS Interception State represents runtime behavior as `inactive`, `active`, or `failed`. Gateway-owned pre-MITM preparation failure changes interception to failed and direct-tunnels the detecting request without changing readiness; explicit install resets interception from a still-valid Active authority without unnecessary trust mutation. Confirmed live uninstall atomically makes readiness not-ready, then removes every owned UserCA without draining connections.

CA lifecycle mutation is single-flight and fail-fast rather than queued. Start rejects while CA mutation is active, status reports `userca: mutating` without inspecting partial state, and stop closes runtime immediately but waits for an already-admitted independent CA operation before owner exit. This supersedes ADR-0011, extends ADR-0017, and replaces the CA mutation, active-runtime guard, and CA cancellation clauses of ADR-0013 and ADR-0014.
