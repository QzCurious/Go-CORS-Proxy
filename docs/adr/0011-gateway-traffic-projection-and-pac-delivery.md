# Gateway traffic projection and PAC delivery

This decision supersedes the Managed PAC publication model in ADR-0002, the transition-based publication policy in ADR-0003, the independent Proxy-generation and PAC ordering in ADR-0005, and the independently published live HTTPS Facade projection in ADR-0010. Their unaffected ownership, parsing, routing-specificity, forwarding, cleanup, and platform-boundary decisions remain in force.

Gateway owns feature composition. It retains selector and UserCA module facts, derives boolean HTTP CORS Demand and HTTPS CORS Demand, and composes one latest desired Traffic Projection containing a PAC Projection plus matching Proxy and HTTPS Facade configuration. HTTPS Facade remains an automatic ability rather than a demand. Internal HTTPS Pipeline Required is derived from the demands and usable UserCA facts but is not surfaced as feature state.

Gateway switches traffic behavior as one coherent runtime operation. It first composes and validates the complete Traffic Projection, then atomically replaces the PAC Endpoint contents and matching Proxy and HTTPS Facade configuration. A successful switch establishes the Served Traffic Projection before Managed PAC delivery begins. A failed switch preserves the previous served projection, makes Traffic Projection Current false, and starts no Network Service delivery for the rejected projection. Obsolete browser-cached PAC contents are outside this coherence invariant.

Traffic Projection Current compares the Served Traffic Projection with Gateway's latest desired Traffic Projection by effective behavior. PAC routes, HTTP and HTTPS CORS behavior, HTTPS Facade mappings, interception behavior, and UserCA identity participate in equivalence. Selector order, source text, warnings, rendered byte identity, PAC URL generation, and Network Service delivery state do not.

Managed PAC receives the current PAC Endpoint and makes one serialized delivery attempt across the fixed Managed PAC Service Set. A failed service retains its previous working PAC setting and produces a service-specific warning; successful services are not rolled back when another service fails. Managed PAC performs no background retry or request conflation. Recovery requires a later Set requested after Gateway switches a served projection, but delivery success neither establishes Traffic Projection Current nor activates a feature.

Traffic Routing Ready is true when the PAC Endpoint and Proxy are serving and fresh Managed PAC Control State reports Routes Current Endpoint. Managed PAC privately establishes that report from at least one enabled, seamless-cors-owned Network Service setting whose URL identifies this Gateway Runtime's PAC endpoint; Gateway does not inspect the URL or ownership itself. An older delivery-generation query remains valid when its host, port, and owned path identify the current endpoint; a marker-owned URL for a previous runtime does not count, and no browser end-to-end probe is required. Set and Read-Only Status obtain this observation without polling. Even when every attempted service update fails, an earlier observed working setting can preserve Traffic Routing Ready.

Feature outcomes are derived as follows:

```text
HTTP CORS Active
  = Traffic Routing Ready
    AND Served Traffic Projection contains HTTP CORS routes

HTTP CORS Blocked
  = HTTP CORS Demand
    AND NOT HTTP CORS Active

HTTP CORS Inactive
  = NOT HTTP CORS Active
    AND NOT HTTP CORS Blocked

HTTPS CORS Active
  = Traffic Routing Ready
    AND Served Traffic Projection contains HTTPS CORS routes
    AND UserCA Current State is usable
    AND its UserCA identity matches the Served Traffic Projection

HTTPS CORS Blocked
  = HTTPS CORS Demand
    AND NOT HTTPS CORS Active

HTTPS CORS Inactive
  = NOT HTTPS CORS Active
    AND NOT HTTPS CORS Blocked

HTTPS Facade Active
  = Traffic Routing Ready
    AND Served Traffic Projection contains HTTPS Facade routes
    AND UserCA Current State is usable
    AND its UserCA identity matches the Served Traffic Projection

HTTPS Facade Inactive
  = NOT HTTPS Facade Active
```

These outcomes do not require Traffic Projection Current and are not changed by per-service delivery warnings. Not-usable UserCA, a UserCA Assessment Issue, or an identity mismatch makes both HTTPS outcomes inactive; retained HTTPS routes alone do not establish activity. HTTPS CORS is blocked when its demand remains true, HTTPS Facade has no blocked outcome, and HTTP CORS remains independent.
