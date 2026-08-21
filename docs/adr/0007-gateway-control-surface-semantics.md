# Gateway control-surface semantics

The Gateway Module owns Surface-Neutral Command Results with operation-specific kinds, fixed fulfillment, structured detail, and ordinary errors only when no semantic result can be produced. CLI and authenticated HTTP Inbound Adapters translate those semantics into their own presentation without redefining command meaning or parsing diagnostic prose.

The Gateway Router keeps HTTP representations private where transport changes semantic shape. Fulfilled operations use bare operation-specific success bodies; every non-success response uses one Gateway Error Response with a stable operation-scoped or Router-wide code, optional structured detail, and non-authoritative message.

Gateway Client reconstructs fulfilled and recognized unfulfilled results from the known operation and response code. Network failure, malformed responses, Router-wide failures, and unknown codes remain errors, preventing HTTP representation details from leaking into Gateway command semantics.
