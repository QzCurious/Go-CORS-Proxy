# Reconcile Managed PAC from complete desired state

**Status:** superseded by ADR-0024

Gateway publishes complete `managedpac.DesiredState` snapshots through a capacity-one latest-value channel whenever Managed PAC input changes. Each snapshot contains all input needed to derive the current effective PAC; Gateway does not assign a PAC version or replay PAC commands. The feature producing the runtime change does not wait for OS PAC writes.

Managed PAC privately serializes publication. A newer desired state replaces older pending state but does not interrupt the current publication attempt. Managed PAC suppresses effective no-ops, owns `publicationGeneration`, retains the last successfully published PAC after failure, and retries the newest desired state.

Reconciliation evaluates each visible fixed-set member independently. It applies the latest URL to marker-owned or empty state, preserves foreign state with a nonfatal drift warning, ignores absence without warning, and reports eligible platform write failure as a nonfatal update-failed warning. Foreign and absent services remain members of the fixed set. Services outside the set are never inspected for adoption.

Managed PAC Warnings remain independent from HTTPS Warnings. Managed PAC publication errors are internal for now; Gateway status does not receive a Managed PAC diagnostic stream.
