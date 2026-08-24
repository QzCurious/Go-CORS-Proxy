# Installed UserCA lifecycle

The gateway keeps one long-lived seamless-cors-owned development CA in the current user's operating-system trust store and protects its unencrypted local signing key with current-user file permissions. This avoids trust or unlock prompts during normal Gateway Runtime cycles without introducing a secret-store dependency. UserCA stores exactly one certificate and matching private key, publishing the complete pair with one directory rename.

Install reuses valid material and repairs missing trust or permissions in place. An authority within 90 days of expiry remains usable and reports renewal due, while explicit install replaces renewal-due, expired, invalid, or ambiguous state. Replacement removes and verifies all owned trust and local material before publishing and trusting one fresh pair; it deliberately provides no overlapping authority generations or zero-downtime rotation guarantee.

Install and uninstall are explicit, Upstream-independent CA Lifecycle Commands routed through Gateway Ownership. Gateway is the sole owner of CA mutation serialization and runtime consequences: it withdraws active HTTPS before either mutation, restores HTTPS only from a successful install Current State, rejects competing mutation admission, and makes Owner Stop wait for admitted work. UserCA does not own concurrency or Gateway Runtime behavior.

Uninstall removes and verifies all strictly owned trust and local material. An interrupted or failed replacement may leave an absent or unusable single-pair footprint; HTTPS remains not-ready, and the next explicit install or uninstall reconciles that footprint. This simpler recovery model is preferred to Candidate, Active, and Retired generations for a local DEV/QA tool.
