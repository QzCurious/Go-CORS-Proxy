# Request Managed PAC reconciliation explicitly

Whenever Gateway Runtime changes its desired PAC URL Version, Gateway submits one explicit, non-blocking `RequestReconcile` call with the fixed Managed PAC Service Set and new URL. Managed PAC has no watcher, timer, or dependency on Live Configuration or UserCA events. The feature producing the runtime change does not wait for OS PAC writes.

Managed PAC privately serializes reconciliation. A newer request preempts older work, waits only until the current writer is quiescent, and converges directly to the newest requested URL. Only the newest settled request invokes its one-shot completion with the current typed warning snapshot.

Reconciliation evaluates each visible fixed-set member independently. It applies the latest URL to marker-owned or empty state, preserves foreign state with a nonfatal drift warning, ignores absence without warning, and reports eligible platform write failure as a nonfatal update-failed warning. Foreign and absent services remain members of the fixed set. Services outside the set are never inspected for adoption.

Managed PAC Warnings are independent from HTTPS Warnings. Gateway Runtime replaces its latched warning snapshot when the latest reconciliation completes, and status reads that snapshot without inspecting or mutating OS proxy settings.
