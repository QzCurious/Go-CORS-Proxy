# Serialize feature mutations independently

Live Configuration, UserCA, and Managed PAC own separate mutation sequences. Each feature serializes only its own work; there is no global lifecycle mutex or cross-feature lock. Gateway Runtime uses a short private lock only to order in-memory state changes and PAC URL Versions, then releases it before any slow feature operation.

Gateway alone coordinates results across features. A UserCA result or Live Configuration snapshot may update Gateway Runtime and cause Gateway to submit a Managed PAC reconciliation request, but neither originating operation waits for PAC reconciliation. If concurrent runtime changes produce successive PAC URL Versions, Managed PAC preempts the older request and converges to the newest.

Stop has lifecycle precedence without broadening feature locks. It closes traffic, directs Managed PAC to preempt reconciliation and uninstall, and waits for an already-admitted owner-owned UserCA mutation before releasing Gateway Ownership. Managed PAC teardown and UserCA completion remain independent barriers.
