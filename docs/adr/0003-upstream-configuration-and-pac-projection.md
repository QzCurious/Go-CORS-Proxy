# Upstream configuration and PAC projection

File Observation, Upstream List, and PAC Routing are independent modules coordinated only by Gateway. File Observation supplies complete contents or concrete observation failures, Upstream List projects normalized Host Selectors, Origin Selectors, and warnings, and PAC Routing infallibly derives a PAC Projection from the effective Upstream List Projection, HTTPS Pipeline state, and runtime endpoint.

Gateway Runtime retains separate current Upstream List File Sync and Projection Issues. Observation failure changes only the file issue and preserves the effective upstream and PAC projections; rejected contents change the projection issue and fail closed to the canonical Empty Upstream List; successful contents clear the relevant issues and are adopted as a new projection even when semantically equivalent to the prior projection.

Every adopted Upstream List Projection produces and publishes a PAC Projection. This deliberate transition-based policy includes warning-only projection changes, favors a single observable update path over cross-module semantic equality contracts, and leaves Managed PAC—not Gateway or Upstream List—in control of OS publication generation and retry.

`UpstreamList` directly owns its selector slices and warnings. Continuous observation, parser diagnostics, PAC route coalescing, HTTPS admission, UserCA mutation, and direct proxy admission remain outside the Upstream List module.

