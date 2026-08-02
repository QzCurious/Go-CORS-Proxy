# Keep OS integration state within features

Managed PAC system settings and UserCA trust-store integration are separate modules rather than capabilities of one platform adapter. Live Configuration is independent from both. Each feature owns its public seam, OS-specific implementation files, semantic types, errors, mutation serialization, and internal resource lifetime. No feature module imports or calls another feature module.

Managed PAC exposes only `Open`, `Inspect`, `Install`, non-blocking `RequestReconcile`, and `Uninstall`. Its platform settings adapter and mutation machinery remain private. Gateway Runtime retains only semantic Managed PAC Runtime State; it does not hold a feature session or raw system-settings handle.

Gateway alone owns cross-feature orchestration. It applies feature results to Gateway Runtime, establishes ordering through short runtime-state updates, and explicitly requests any resulting work from another feature. UserCA, Live Configuration, and Managed PAC do not share a feature lock and do not wait for one another's slow work. CLI depends on Gateway rather than importing feature modules directly.

There is no generic platform package for shared types, adapters, command execution, or dependency bundles. Small OS command helpers remain local to each feature until shared behavior becomes substantial enough to justify a separate deep module.
