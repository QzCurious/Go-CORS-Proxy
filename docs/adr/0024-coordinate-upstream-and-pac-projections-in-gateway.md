# Coordinate Upstream List and PAC projections in Gateway

**Status:** accepted

Gateway Runtime owns continuous file observation, current Upstream List sync and projection-error states, the effective Upstream List Projection, and the current PAC Projection. File observation reports distinct concrete read, uncertain-observation, and stopped-observation errors; the Upstream List module projects contents and owns projection identity; PAC Routing projects the effective list with HTTPS Interception and runtime endpoint state and owns PAC Projection identity. Gateway classifies concrete errors into comparable Gateway-owned semantic state, invokes each module's equality, and alone decides projection adoption, fail-closed routing, status notification, and cross-feature consequences. Equivalent repeated errors and projections are suppressed, while cleared or semantically changed error state is observable.

Rejected whole-document contents return no usable projection. Gateway reports that fact independently, selects the canonical zero-value Empty Upstream List, and updates PAC only when the resulting PAC Projection changes; observation failure instead preserves the effective projection, and terminal observation directs restart without ending Gateway Runtime. Line-level invalid contents remain successful projections with warnings.

Gateway gives Managed PAC only a changed, already-derived PAC Projection. Managed PAC owns OS publication, generation, serialization, retry, drift, and publication warnings without receiving or reinterpreting Upstream List or HTTPS Interception inputs. This supersedes ADR-0006 and ADR-0023, plus the Upstream List Source ownership portions of ADR-0014 and ADR-0016.
