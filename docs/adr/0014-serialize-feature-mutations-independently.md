# Serialize feature mutations independently

The Upstream List Source, UserCA, and Managed PAC own separate mutation sequences. Each feature serializes only its own work; there is no global lifecycle mutex or cross-feature lock. Gateway Runtime uses a short private lock only to order in-memory state changes and complete desired-state snapshots, then releases it before any slow feature operation.

Gateway alone coordinates results across features. A UserCA result or Upstream List State may update Gateway Runtime and cause Gateway to publish a complete Managed PAC desired state, but neither originating operation waits for PAC publication. Concurrent desired-state changes are conflated to the newest state while an active Managed PAC publication remains serial.

Stop has lifecycle precedence without broadening feature locks. It closes traffic, directs Managed PAC to preempt reconciliation and uninstall, and waits for an already-admitted owner-owned UserCA mutation before releasing Gateway Ownership. Managed PAC teardown and UserCA completion remain independent barriers.
