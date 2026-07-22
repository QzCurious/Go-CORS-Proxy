# Keep OS integration state within features

Managed PAC system settings and UserCA trust-store integration are separate modules rather than capabilities of one platform adapter. Each feature owns its interface, OS-specific implementation files, types, errors, and any state or resource lifetime needed by that implementation; state and lifecycle ownership may persist within a feature but may not cross between Managed PAC and UserCA, while the Gateway Module coordinates the features only through their public results.

The generic platform package is removed instead of retained as a shared type, adapter, command-execution, or dependency-bundle package. Small OS command helpers remain local to each feature until shared behavior becomes substantial enough to justify a separate deep module.
