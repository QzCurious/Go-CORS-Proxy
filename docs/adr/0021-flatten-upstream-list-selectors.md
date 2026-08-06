# Flatten Upstream List selectors

`UpstreamList` owns its normalized `HostSelectors`, `OriginSelectors`, and `Warnings` directly; the nested `Entries` value and its public equality helpers are removed. Source retains private deduplication and state identity checks, while PAC Routing accepts selector slices directly and owns effective route-set coalescing, keeping parser diagnostics out of routing APIs and avoiding a wrapper whose only purpose was cross-module transport.

## Considered Options

- Keep `Entries` as the selector-only routing value.
- Move `Entries` into PAC Routing as `RouteEntries`.
- Flatten selectors into `UpstreamList` and pass them directly to PAC Routing.

The last option was chosen because the selectors remain Upstream List concepts, not PAC-derived routes, and the current architecture already has Source and PAC Routing boundaries for the two kinds of deduplication.
