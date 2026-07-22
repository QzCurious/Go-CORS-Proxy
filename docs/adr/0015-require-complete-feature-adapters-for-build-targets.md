# Require complete feature adapters for build targets

A seamless-cors binary may be built for an operating system only when every required OS-managed feature, including Managed PAC and UserCA trust, has a complete platform implementation. Feature packages therefore provide build-tag-selected implementations and compile-time interface assertions without successful no-op or generic unsupported adapters, so a missing feature implementation fails the target build instead of deferring an incomplete Gateway Distribution to runtime; CI must cross-build every intended distribution target.

Because target compilation establishes platform support and read-only preflight cannot reliably predict transient permission, policy, or system-state failures, the Gateway has no runtime platform capability gate or user-facing capability-check command. Each feature operation validates its immediate prerequisites and reports execution failures directly.

The intended Gateway Distribution matrix is `darwin/amd64`, `darwin/arm64`, `windows/amd64`, and `windows/arm64`. Repository verification cross-builds every production package for all four targets so missing or incomplete build-tag-selected feature implementations fail before distribution.
