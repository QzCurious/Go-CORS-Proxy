# Managed PAC lifecycle and publication

Managed System Proxy uses PAC Routing so unrelated traffic remains direct. Each Gateway Runtime activation proposes every visible marker-owned or empty network service together, excludes foreign services, and fixes the accepted nonempty Managed PAC Service Set for that runtime; every later operation freshly classifies those fixed members without adopting newly visible services or overwriting foreign state.

Gateway gives Managed PAC each accepted, already-derived PAC Projection. Managed PAC owns publication generation, versioned ownership-marked URLs, capacity-one latest-value reconciliation, serial platform mutation, retry, drift classification, and current per-service warnings; every accepted projection is published even when it preserves effective routes, so warning-only Upstream List changes may advance the publication generation. Gateway Runtime exposes the latest Managed PAC Runtime State and warning snapshot through command results and status.

Initial publication begins only after Gateway Runtime and its PAC Endpoint are serving. Publication failure is nonfatal, preserves the fixed service set and desired projection, reports current warnings, and retries without stopping Gateway Runtime.

Active-state cleanup disables and verifies every enabled marker-owned setting without changing reconciliation admission. Uninstall is the complete teardown barrier: it closes admission, discards pending work, quiesces the current writer, performs active-state cleanup, and clears runtime state only after successful teardown; ownership authorizes cleanup regardless of publication generation, while disabled retained URLs and foreign settings are not active residue.

