# Keep OS integration state within features

Managed PAC system settings and UserCA trust-store integration are separate modules rather than capabilities of one platform adapter. The Upstream List Source is independent from both. Each feature owns its public seam, OS-specific implementation files, semantic types, errors, mutation serialization, and internal resource lifetime. No feature module imports or calls another feature module.

Managed PAC operations are limited to `Open`, `Inspect`, complete desired-state installation/publication, and `Uninstall`. Its platform settings adapter, publication generation, retry machinery, and mutation serialization remain private. Gateway behavioral seams use feature-owned semantic types directly instead of recreating isomorphic Gateway types. Gateway-owned projections exist only for command results that deliberately expose a different semantic shape; they never hide a concrete feature value inside a parallel projection. Gateway Runtime retains the feature-owned Managed PAC Runtime State, not a feature session, hidden raw value, or system-settings handle.

Gateway alone owns cross-feature orchestration. It applies feature results to Gateway Runtime, establishes ordering through short runtime-state updates, and publishes complete desired state to Managed PAC. UserCA, the Upstream List Source, and Managed PAC do not share a feature lock and do not wait for one another's slow work. CLI depends on Gateway rather than importing feature modules directly.

There is no generic platform package for shared types, adapters, command execution, or dependency bundles. Small OS command helpers remain local to each feature until shared behavior becomes substantial enough to justify a separate deep module.
