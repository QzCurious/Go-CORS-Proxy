# Module, adapter, and platform boundaries

Managed PAC, UserCA, File Observation, Upstream List, and PAC Routing are separate modules that own their semantic types, concrete errors, platform seams, mutation serialization, and internal resources. No feature module initiates another feature's lifecycle; Gateway alone coordinates cross-feature state and ordering without a global lifecycle mutex, and callers use feature-owned values unless Gateway deliberately exposes a different semantic shape.

Independently composed Inbound Adapters live under `internal/inbound`, depend inward on the modules they drive, and expose a small interface to the composition root. Production packages outside `cmd` do not import them; the CLI exposes `Run`, while the private authenticated HTTP Gateway Router remains inside the Gateway Module.

A Gateway Distribution is supported only when every required OS-managed feature has a complete build-tag-selected adapter and compile-time interface assertion. Missing implementations fail target compilation rather than becoming runtime no-ops or capability gates; production adapters remain pure Go and cross-compilable, while executable build and release configuration defines the changing OS and architecture matrix.

There is no generic platform package or dependency bundle. Small command helpers remain local to their feature until shared behavior is substantial enough to justify a separate deep module.

