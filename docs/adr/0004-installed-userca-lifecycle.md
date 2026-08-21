# Installed UserCA lifecycle

The gateway keeps one long-lived seamless-cors-owned development CA in the current user's operating-system trust store and protects its unencrypted local signing key with current-user file permissions. This avoids trust or unlock prompts during normal Gateway Runtime cycles without introducing a secret-store dependency.

UserCA uses immutable fingerprint-named authority generations and one atomically persisted active-fingerprint marker. Install reuses a valid Active UserCA, repairs missing trust or permissions in place, or rotates a renewal-due authority by fully preparing and trusting a Candidate before committing it active; ambiguous unmarked state is removed and verified rather than guessed, and non-active residue is reconciled before another authority is added.

Install and uninstall are explicit, Upstream-independent CA Lifecycle Commands routed through Gateway Ownership. UserCA privately serializes mutations with fail-fast admission, an admitted mutation belongs to its owner rather than the requesting connection, Stop waits for it to settle, and uninstall removes and verifies all owned trust and material without requiring an internal consent mechanism.

Rotation does not drain admitted or established connections. The previous authority may remain trusted briefly as Retired residue while old runtime work finishes, but its private material is removed as soon as practical and later lifecycle work retries any incomplete non-active cleanup.

