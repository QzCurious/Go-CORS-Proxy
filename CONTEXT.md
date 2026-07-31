# seamless-cors

seamless-cors is a DEV/QA context for controlled browser-origin testing across configured upstream domains.

## Language

**seamless-cors**:
A local DEV/QA network tool that sits between the browser and configured upstream domains so browser requests can be tested under adjusted cross-origin behavior without changing application request URLs.
_Avoid_: generic proxy, CORS middleware

**Gateway Module**:
The single internal module that owns start, serve, stop, status, and Installed User CA lifecycle commands for CLI and HTTP control surfaces. Its small public interface hides owner discovery, authenticated local HTTP transport, process ownership, Gateway Footprint Cleanup decisions, Managed PAC state, runtime visibility, UserCA lifecycle behavior, and traffic-runtime sequencing.
_Avoid_: Gateway Facade, gateway client package, gateway coordinator package, gateway owner package, gateway router package, command service

**Surface-Neutral Command Result**:
A Gateway Module operation result that describes successful, blocked, retryable, and next-action-required command outcomes without terminal text, HTTP status codes, or surface-specific formatting.
_Avoid_: CLI output, HTTP response model, stringly command result, terminal error text

**Operation-Specific Result Kind**:
A closed command result vocabulary scoped to one Gateway Module operation, so each operation exposes only the outcomes that can actually happen for that command.
_Avoid_: global result code, shared outcome enum, impossible command state

**Gateway Router**:
The private authenticated-local-HTTP adapter inside the Gateway Module that exposes gateway feature routes and renders Surface-Neutral Command Results as HTTP responses.
_Avoid_: runtime control endpoint, proxy route, daemon supervisor

**Gateway Client**:
A typed client-facing layer used by CLI and future user interfaces to discover and call an existing Gateway Owner's Gateway Router through the Gateway State Cache identity.
_Avoid_: command service, lifecycle client, generic JSON caller, managed gateway

**Gateway Owner**:
The module that holds the Gateway Ownership Lease and publishes Gateway Router discovery state for a long-running ownerless `serve` or `start` command or transient ownerless CA work. Once published, start, CA Lifecycle Commands, status, and stop address that owner, while competing serve fails.
_Avoid_: daemon supervisor, client command, detached runtime owner, terminal command renderer

**Gateway Runtime**:
The live traffic-serving engine that owns the proxy listener, proxy server, PAC listener, PAC server, live configuration, runtime close behavior, and fatal runtime error reporting without installing or unsetting OS PAC state.
_Avoid_: lifecycle facade, command router, OS proxy manager, cleanup owner

**Router-Only Serve**:
A command behavior where the command becomes the Gateway Owner and starts the Gateway Router as an HTTP client entry point without automatically starting Gateway Runtime, running Gateway Footprint Cleanup at serve startup, or changing managed OS state; it fails clearly when a Gateway Owner already exists and may claim stale Gateway State Cache only after verification finds no reachable owner.
_Avoid_: implicit gateway start, daemonized start, hidden lifecycle activation, stale-cache cleanup, OS PAC repair

**Router-Hosted Start**:
An HTTP start behavior where CLI or another client calls `POST /start` against an existing Gateway Owner, renders PAC Replacement Consent when the result requires it, and retries with accepted consent to activate Gateway Runtime without creating a competing gateway process. The existing owner remains foreground, and an already-active runtime returns an idempotent start result.
_Avoid_: start plan, terminal prompt in serve, duplicate router start, serve-blocked start, split-brain gateway

**Start-Hosted Router**:
A startup behavior where gateway start becomes the Gateway Owner by hosting the Gateway Router and Gateway Runtime while activation remains governed by the Start Sequence.
_Avoid_: router-only fallback, control endpoint replacement, implicit consent

**Router-Hosted Start Failure**:
A failed start behavior where direct `start` exits and removes owner visibility, while `/start` sent to an existing router-only Gateway Owner leaves that owner alive.
_Avoid_: surprise serve fallback, failed-start owner leak, serve shutdown on start rejection

**Owner-Routed Start**:
A start behavior where an ownerless CLI command becomes the long-running Gateway Owner, while a CLI command finding an existing owner calls its Gateway Router and exits after the result. Routed start never transfers foreground ownership from the existing owner.
_Avoid_: competing gateway process, owner replacement, split-brain gateway, routed caller claiming foreground ownership

**Managed System Proxy**:
A traffic capture approach where the gateway configures the operating system or browser proxy settings on behalf of the user, so application requests keep their original URLs and no manual proxy setup is required.
_Avoid_: VPN, manual proxy, browser-only workaround

**Selective Managed Proxy**:
A Managed System Proxy behavior where only Upstream List matches are routed through the gateway and all other traffic bypasses it.
_Avoid_: whole-system proxy, blanket proxying

**PAC Routing**:
A Selective Managed Proxy approach that uses a proxy auto-configuration file to route Upstream List matches through the gateway while returning `DIRECT` for other traffic.
_Avoid_: global proxy, pass-through proxying

**Trust-Aware PAC Routing**:
A PAC Routing behavior where matched HTTPS traffic is routed through the gateway only when Trusted HTTPS Interception is enabled.
_Avoid_: routing unrepaired HTTPS, unnecessary HTTPS proxying

**Generated PAC**:
A runtime proxy auto-configuration artifact derived from Live Configuration and the Upstream List, not edited directly by the user.
_Avoid_: user-authored PAC, manual PAC rules

**PAC Route Set**:
The Host Routes and Origin Routes derived inside the PAC Routing module from normalized Upstream List Entries and the current Trusted HTTPS Interception state, keeping the Generated PAC JavaScript mostly static.
_Avoid_: hand-built JavaScript rules, duplicated Upstream List parsing, PAC-owned Upstream List syntax

**PAC Endpoint**:
A local HTTP endpoint served by the gateway that returns the current Generated PAC.
_Avoid_: file PAC, static PAC file

**PAC URL Version**:
A runtime-selected identity on the PAC Endpoint, usually carried by an owned URL query version, that changes when PAC Routing clients must fetch a newer Generated PAC while still preserving the seamless-cors Managed PAC Ownership Marker.
_Avoid_: port rotation, foreign cache-busting parameter, PAC file version, browser cache workaround

**Gateway Distribution**:
The installable form of seamless-cors for a specific operating system and CPU architecture.
_Avoid_: cross-platform binary

**Managed Platform**:
A supported operating system where the gateway can configure PAC Routing and user trust on behalf of the user.
_Avoid_: manual platform, manual proxy fallback, all-platform parity without adapters

**Best-Effort Stop**:
A stop behavior where Gateway Footprint Cleanup attempts every cleanup subject, including seamless-cors-owned active PAC state, live coordination cache, and the Gateway Owner, even when another cleanup subject reports a platform operation failure.
_Avoid_: failure-blocked cleanup, leaving owned runtime state behind, router-only stop

**Owner Stop**:
A stop behavior where explicit stop or graceful process termination tears down the Gateway Owner itself, including a router-only or transient owner, and closes Gateway Runtime before Gateway Footprint Cleanup so no new traffic is accepted. Owner exit rejects new work and waits for any already-admitted owner-owned CA mutation to settle without canceling it before releasing ownership.
_Avoid_: runtime-only stop, router-only survival, stop-as-status, start-survives-stop, accepted-before-cleanup, cleanup-before-runtime-close

**Owner Ending**:
A Gateway Owner lifecycle state that begins when Owner Stop takes precedence and lasts until stop succeeds or the process exits. A CA Lifecycle Command admitted earlier may settle, but new start, install, and uninstall work is rejected; a Retryable Stop Failure leaves only stop retries and status available.
_Avoid_: owner stopping, shutdown window, late start admission, start-after-stop

**Retryable Stop Failure**:
A failed stop behavior where the Gateway Owner remains alive after ordinary Blocking Cleanup Subject failure so the user can retry `stop` through the same command channel, even if Gateway Runtime has already been partially or fully closed.
_Avoid_: failed-stop owner exit, stale-cache recovery first, retry-without-owner, runtime-must-remain-active

**Blocking Cleanup Subject**:
A gateway cleanup subject, including seamless-cors-owned active OS PAC settings and the live Gateway State Cache, that must be removed and reported before `stop` can claim success because process exit will not reliably remove it or because leaving it behind would make the successful stop result untrue.
_Avoid_: best-effort durable cleanup, process-exit cleanup, warning-only cleanup

**Process-Bound Cleanup Subject**:
A gateway cleanup subject owned by the current process that should be closed gracefully but will be released by process termination if graceful close fails.
_Avoid_: stop-blocking runtime resource, durable cleanup, OS-managed state

**UserCA**:
A simplified product name for the current user's seamless-cors-owned development certificate authority, including Installed User CA lifecycle and local signing material.
_Avoid_: root CA service, trust manager, certificate service

**User-Trusted Development CA**:
A local certificate authority trusted only in the current user's trust store so the gateway can inspect HTTPS traffic for configured upstream domains during DEV/QA work.
_Avoid_: system-wide CA, production CA, shared CA

**OS Trust Only**:
A trust model where the gateway manages only the current user's operating-system trust store and does not inspect or manage browser-specific trust stores.
_Avoid_: browser trust management, profile-specific trust diagnostics

**Installed User CA**:
A long-lived local development certificate authority trusted only in the current user's operating-system trust store and reused across trusted gateway starts until it is removed or replaced.
_Avoid_: per-start CA, system-wide CA, shared CA

**Installed User CA Renewal**:
A maintenance operation performed by explicit install that replaces an Installed User CA before expiry. A near-expiry authority remains valid for Trusted HTTPS Interception while producing a renewal warning, and runtime expiry detection directs the user to install rather than mutating OS trust automatically.
_Avoid_: traffic-triggered trust mutation, automatic root replacement, silent CA replacement, treating near-expiry as expired

**CA Replacement Rule**:
A CA lifecycle rule where a valid Active UserCA is reused, its missing trust or local permissions are repaired in place, and renewal-due authority rotates through an overlapping Candidate without interrupting active HTTPS. When the Active marker is absent, invalid, or does not identify valid material, install removes every ambiguous owned authority and verifies cleanup before creating a fresh Candidate rather than guessing an Active identity.
_Avoid_: newest-authority inference, unmarked authority adoption, adding a root beside ambiguous residue, proxy-failure-triggered replacement, destructive pre-candidate renewal, trusting invalid material, start-time repair

**CA Ensure**:
The trust-mutating operation behind the explicit UserCA install command, requesting platform approval only when trust must be added or replaced. Gateway start assesses HTTPS Readiness without invoking CA Ensure.
_Avoid_: start-time CA installation, activation-owned CA setup, repeated trust prompt, implicit trust repair

**Owner-Owned CA Mutation**:
An admitted install or uninstall belongs to the Gateway Owner and settles independently of request cancellation or client disconnection. Owner Stop waits for it, while process interruption relies on immutable generations, the Active fingerprint marker, and the next install or uninstall for recovery.
_Avoid_: request-owned mutation, disconnect cancellation, stop-cancelled CA command, caller-managed commit boundary

**Gateway-Owned CA Lifecycle**:
A lifecycle rule where install, UserCA Rotation, and uninstall execute through an existing Gateway Owner or a discoverable Transient Gateway Owner published before ownerless work. Gateway Ownership is the sole cross-process serialization authority, while the owner coordinates live runtime consequences and serializes CA mutation internally.
_Avoid_: ownerless CA mutation, undiscoverable ownership holder, separate CA Mutation Lease, direct UserCA command execution, caller-managed CA locking

**Transient Gateway Owner**:
A discoverable Gateway Owner published before ownerless CA lifecycle work. It exposes the Gateway Router and Gateway State Cache while coordinating one finite CA mutation; status reports `userca: mutating`, stop enters Owner Ending and waits, competing CA work and start fail fast, and the owner cannot be promoted into a long-running owner.
_Avoid_: promotable CA owner, install-owned Gateway Runtime, private one-shot lease holder, hidden CA process, background daemon, undiscoverable owner

**Fail-Fast CA Mutation Admission**:
A Gateway Owner admission rule where install and uninstall route through an available owner but are rejected for explicit retry when another complete CA command is already admitted or Owner Ending has begun. Admission spans UserCA mutation, runtime adoption or deactivation, Managed PAC refresh, and the command result; status remains available as `userca: mutating`, stop waits for admitted work, and no queue is maintained.
_Avoid_: owner-exists-means-busy, queued CA mutation, concurrent CA mutation, blocked status

**Ownership-Protected Status Assessment**:
An ownerless Read-Only Status behavior that briefly holds the Gateway Ownership Lease without publishing Gateway Router discovery state, assesses Gateway and UserCA facts coherently, then releases the lease. If ownership acquisition loses a race, status rediscovers the new owner rather than combining facts across ownership generations.
_Avoid_: Transient Gateway Owner for status, status-written discovery cache, unlocked multi-location CA assessment, status mutation

**Settled-CA Start Admission**:
An owner-coordinated startup boundary where Active UserCA admission is serialized with CA Lifecycle Commands, so Gateway Runtime never loads authority facts from an in-progress mutation.
_Avoid_: runtime boot from mutating CA state, marker polling, UserCA-owned runtime coordination

**Installed UserCA Set**:
The seamless-cors-owned immutable fingerprint-named authority generations represented in current-user OS trust or local authority storage. Normal state contains one Active UserCA and at most one Candidate or Retired UserCA; another rotation cannot begin until non-active residue is reconciled.
_Avoid_: permanent multiple UserCAs, ambiguous authority collection, unbounded trusted identities

**Active UserCA**:
The one Installed User CA identified by the durable atomic active-fingerprint marker and used to sign new intercepted HTTPS connections.
_Avoid_: unmarked authority, newest-certificate inference, arbitrary installed authority, multiple active signers

**Candidate UserCA**:
A fully prepared and OS-trusted immutable authority generation that may coexist with the Active UserCA but does not sign connections until its fingerprint is atomically persisted as active and the runtime adopts its HTTPS generation.
_Avoid_: partially installed CA, active signer, untrusted staging certificate, required Candidate marker

**HTTPS Generation**:
An immutable runtime bundle containing one UserCA fingerprint, certificate, signer, and generation-owned leaf-certificate cache. Each new TLS handshake atomically loads the current generation pointer once, preventing certificate, signer, and cached leaves from crossing authority generations.
_Avoid_: independent signer swap, shared cross-generation leaf cache, mutable authority bundle

**Retired UserCA**:
The previous Active UserCA after atomic HTTPS Generation swap. In-flight handshakes may continue using their loaded generation because its public root remains trusted; old private material is removed as soon as practical, while fallible OS trust removal is deferred to Non-Active UserCA Cleanup.
_Avoid_: active signer, permanent secondary root, connection drain, retained private key

**UserCA Rotation**:
An uninterrupted HTTPS maintenance transition that creates and trusts an immutable Candidate generation, atomically persists its fingerprint as active, then atomically swaps the runtime HTTPS Generation pointer when runtime remains live. If runtime closes during the independent operation, the durable marker is sufficient and the next start loads it. Install succeeds once the new authority is trusted and durably active, plus adopted when a live runtime exists; later Retired cleanup is not part of success. Failure or process interruption before marker persistence leaves the old Active authoritative, and the next install or uninstall privately reconciles residue without guessing Candidate active.
_Avoid_: TLS handshake barrier, handshake counter, connection registry, stop-required renewal, synchronous retired-root cleanup, rotation journal

**Interrupted UserCA Rotation**:
A recoverable state with a marked valid Active UserCA and non-active owned authority residue. Gateway restart serves the marked authority without mutating OS trust, while the next UserCA lifecycle event privately reconciles non-active authority state before adding another authority.
_Avoid_: Candidate marker, startup trust mutation, multiple-authority readiness failure, newest-certificate guessing, rotation journal

**Non-Active UserCA Cleanup**:
A private UserCA behavior inside install and uninstall that removes owned authority state not retained by the requested postcondition, including pre-marker Candidate and post-marker Retired residue. Post-rotation failure does not disable usable HTTPS and is retried by the next install or uninstall before another authority may be added.
_Avoid_: public reconcile operation, Gateway cleanup hook, Gateway cleanup state, public cleanup warning, administrator-assumed cleanup, active HTTPS disablement, unbounded trusted-root accumulation

**CA Material Integrity**:
A CA lifecycle invariant where current-user CA trust and local signing material match for each member of the Installed UserCA Set; missing or mismatched material is treated as repair-needed state.
_Avoid_: trusted cert without signing key, orphaned signing key, mismatched CA pair

**OS-Backed CA Installation**:
A CA lifecycle invariant where Installed User CA state requires current-user operating-system trust; local CA material alone is not installed trust.
_Avoid_: file-only installation, assuming trust from local material

**CA Permission Repair**:
A CA lifecycle behavior where otherwise-valid Installed User CA material with loose local file permissions is tightened in place without replacing trusted CA identity.
_Avoid_: permission-triggered CA rotation, loose CA key permissions

**Leaf Certificate Reuse**:
A runtime behavior where generated per-host HTTPS certificates may be reused within one HTTPS Generation until their generation age exceeds the private cache reuse limit. Regenerated leaves never outlive their Active UserCA, the cache is discarded with its authority generation, and neither timing policy nor leaf state belongs to UserCA.
_Avoid_: cross-generation leaf cache, persistent leaf certificate inventory, per-request certificate churn, expiry-only cache policy

**Per-Host Leaf Certificate**:
An automatically generated and renewed HTTPS server certificate for the specific upstream hostname being intercepted, signed locally by the Active UserCA without changing OS trust. Its lifetime and cache remain within one HTTPS Generation.
_Avoid_: leaf CA certificate, user-installed leaf trust, Upstream List-wide leaf certificate, wildcard-first certificate strategy, persisted leaf identity

**HTTPS Intent**:
An Upstream List state containing at least one valid HTTPS Origin Selector. Host Selectors and HTTP Origin Selectors do not express this intent.
_Avoid_: Config File HTTPS toggle, inferred Host Selector HTTPS intent, invalid-line HTTPS intent

**Unmet HTTPS Intent**:
An HTTPS state where HTTPS Intent exists while HTTPS Readiness is not ready. Trusted HTTPS Interception remains inactive, the gateway continues serving HTTP, and the user receives an actionable warning.
_Avoid_: blocked gateway, failed gateway start, implicit UserCA installation

**HTTPS Warning**:
A typed, surface-neutral current diagnostic owned by the Gateway Module and exposed independently from HTTPS Intent and HTTPS Readiness so multiple conditions may coexist. Stable kinds cover unmet intent, unusable UserCA state, renewal due, interception failure, and PAC refresh failure; front ends render them for users and cleared warnings are not retained as history.
_Avoid_: terminal warning text, warning history, single CA warning string, readiness encoded as prose, mutually exclusive diagnostics, silent degraded HTTPS

**Live HTTPS Warning Delivery**:
A foreground lifecycle callback that publishes a surface-neutral HTTPS Warning snapshot when the current set changes. The CLI renders added or materially changed warnings live, while HTTP clients read the same current set through status without requiring a streaming endpoint.
_Avoid_: Gateway-owned terminal output, warning polling in the foreground CLI, warning event history, required HTTP event stream

**HTTPS Readiness**:
The runtime-assessed state of whether UserCA capability can support Trusted HTTPS Interception, expressed as `ready` or `not-ready` from the admitted two-state UserCA Snapshot. Assessment failure and renewal due are independent diagnostics, while expiry derives effective not-ready state from the Snapshot expiry and Gateway clock; private marker, trust, material, and cleanup facts do not cross the UserCA seam. Proxy operational failures belong to HTTPS Interception State instead.
_Avoid_: proxy health, continuous trust-store polling, installed-file check, expiry warning as not-ready

**HTTPS Readiness Loss**:
A runtime transition from ready to not-ready HTTPS Readiness when UserCA capability becomes unavailable, including certificate expiry detected at an HTTPS request boundary or confirmed Live UserCA Uninstall. Read-only status may report expiry-derived effective readiness before this transition without mutating runtime state.
_Avoid_: proxy operational failure, failed gateway, continuous trust-store revalidation, status mutation

**HTTPS Interception State**:
The runtime behavior state derived from HTTPS Readiness and gateway-owned proxy operation: `inactive` when readiness is not-ready, `active` when readiness is ready and interception works, or `failed` with a stable reason such as `signer-unavailable`, `leaf-certificate-failed`, `tls-configuration-failed`, or `active-signer-mismatch` when readiness remains ready but interception fails.
_Avoid_: HTTPS Interception Health, separate active boolean, UserCA health, client connection health, upstream availability

**Pre-MITM Interception Admission**:
A CONNECT boundary that atomically loads one HTTPS Generation and successfully obtains its host leaf certificate and TLS configuration before the proxy commits the connection to MITM. Gateway-owned preparation failure changes HTTPS Interception State to failed and lets that same CONNECT use an ordinary direct tunnel.
_Avoid_: post-200 leaf generation, failed TLS handshake as fallback, partial MITM commitment

**HTTPS Interception Failure**:
A failure in gateway-owned signer, leaf-certificate generation, internal TLS configuration, or active-marker/signer agreement that changes HTTPS Interception State from active to failed while leaving HTTPS Readiness ready. The detecting request and other stale-routed HTTPS requests direct-tunnel, subsequent PAC routing sends HTTPS directly, and a current actionable warning is exposed; client, browser, upstream TLS, and network failures do not cause this transition.
_Avoid_: any TLS error, client failure as global state, upstream outage as readiness loss, UserCA rotation

**HTTPS Readiness Recovery**:
A runtime transition from not-ready to ready HTTPS Readiness immediately after successful UserCA installation or repair.
_Avoid_: restart-required HTTPS activation, delayed readiness after successful install, Config File toggle

**HTTPS Interception Reset**:
A transition from failed to active HTTPS Interception State after explicit install verifies HTTPS Readiness remains ready, rebuilds in-memory signer and TLS interception state, and clears the leaf-certificate cache without unnecessary OS trust mutation.
_Avoid_: unnecessary UserCA rotation, implicit retry loop, restart-required proxy repair

**Trusted HTTPS Interception**:
A runtime behavior present only while HTTPS Interception State is active. HTTPS Origin Selectors and Host Selectors then produce HTTPS routes; inactive or failed state removes those routes while the gateway continues serving HTTP and stale-routed HTTPS direct-tunnels.
_Avoid_: readiness-only activation, separate active boolean, Config File HTTPS toggle, untrusted HTTPS interception, broken MITM

**Installed-CA HTTPS Enablement**:
A lifecycle rule where ready HTTPS Readiness allows HTTPS Interception State to become active without a separate configuration toggle. HTTPS Intent makes inactive interception caused by missing readiness warning-worthy, but does not install, repair, or substitute for UserCA capability.
_Avoid_: Explicit Trusted HTTPS, Config File HTTPS toggle, intent-as-capability, silent trust installation

**Live Configuration**:
A gateway module and code boundary that exclusively owns observing and reading the user-editable Upstream List and exposing its validated semantic meaning at startup and when it changes. Valid Upstream List changes apply without requiring the user to restart or reload the browser or gateway.
_Avoid_: external file-watcher boundary, raw filesystem event API, platform-specific watcher abstraction, consumer-owned config deduplication, runtime-only reload, manual reload, restart requirement, stale configuration, separate config package

**Live Configuration Snapshot**:
The validated immutable current Upstream List value exposed by Live Configuration, including diagnostic source metadata without interpreting HTTPS Readiness or other consumer capabilities. Its identity is based on parsed meaning rather than source-file representation, and change delivery may coalesce unconsumed intermediate snapshots in favor of the latest value.
_Avoid_: configuration event history, raw file content, file-change event, content fingerprint, watcher notification

**Upstream List Entries Revision**:
A run-local, non-persistent monotonic identity exposed with Live Configuration, beginning with the initial validated Upstream List Entries and advancing only when the normalized, deduplicated entry set changes. It allows consumers to apply the newest coalesced routing input without treating representation-only edits as routing changes.
_Avoid_: persisted Upstream List revision, Upstream List file revision, PAC content comparison, source-content fingerprint

**Gateway Control Command**:
A user-facing command that controls gateway-owned state or reports on it, including start, serve, stop, status, UserCA install, and UserCA uninstall.
_Avoid_: lifecycle operation, command service, control endpoint operation

**Start Sequence**:
The public Gateway Module start flow that verifies ownership, performs early ownership-aware Gateway Footprint Cleanup, loads the Upstream List, assesses HTTPS Readiness without mutating trust, and then attempts Gateway Activation. Direct start removes stale owner state before claiming ownership, while router-hosted start preserves the live owner cache; cleanup failure is returned as a structured start outcome identifying each failed cleanup subject.
_Avoid_: start-time CA installation, public raw activation, PAC-first start, cleanup-after-approval

**Gateway Activation**:
The internal operation that assesses PAC Replacement Consent, begins serving Gateway Runtime with its assessed HTTPS Readiness, installs managed PAC state, and then produces Start Guidance. It is invoked only through the Start Sequence so callers cannot bypass cleanup, Upstream List validation, readiness assessment, or traffic-before-PAC ordering.
_Avoid_: public activation command, CA installation, CA Trust Consent, lifecycle activation, runtime startup, command rendering, lifecycle orchestration package

**Automatic Listeners**:
A lifecycle behavior where the gateway chooses available loopback ports for its proxy, PAC, and router endpoints at startup, then wires dependent gateway state in sequence.
_Avoid_: user-selected listener ports, fixed listener ports, manual listener addresses

**Loopback Default**:
A listener behavior where gateway endpoints bind to loopback.
_Avoid_: LAN-exposed proxy, user-selected bind address

**Proxy Listener**:
A local proxy endpoint where traffic that reaches it is eligible for CORS repair, normally reached through PAC Routing for Upstream List matches.
_Avoid_: manual proxy endpoint, browser setup address, generic listen, gatewayListen

**CORS Proxy**:
The gateway module behind the Proxy Listener that owns CORS repair, Local Preflight Answer, Response Repair, and Trusted HTTPS Interception behavior for traffic that reaches it.
_Avoid_: Upstream List admission module, PAC Routing module, generic proxy

**Home Config Directory**:
The fixed seamless-cors location at `.seamless-cors` under the user's home directory. Live Configuration owns configuration sources within it, while Gateway Coordination and UserCA independently own their state in dedicated subdirectories.
_Avoid_: platform-native app config directory

**Runtime State Directory**:
The durable location under the Home Config Directory for Gateway Coordination state, including the Gateway Ownership Lease file and Gateway State Cache.
_Avoid_: temp runtime state, volatile cleanup files, miscellaneous runtime file storage

**Gateway Coordination**:
A lifecycle behavior that owns the Gateway Ownership Lease, Gateway State Cache operations, Gateway State Verification, and Single User Instance decisions while allowing lifecycle cleanup paths to remove cache state through Gateway Footprint Cleanup.
_Avoid_: Runtime Coordination, cleanup module, process supervisor, daemon manager, file-exists-is-running

**Installed CA Storage**:
The durable location under the Home Config Directory for seamless-cors-owned Installed User CA material, kept outside Gateway Footprint Cleanup.
_Avoid_: runtime CA storage, temp CA files, stop-owned CA files

**Gateway State Cache**:
A durable gateway coordination cache that lets client commands discover and verify the Gateway Owner by its HTTP Router listener and token identity.
_Avoid_: Runtime State File, control state, pid-only lock file, configured control address, in-memory instance registry, source of truth

**Gateway Ownership Lease**:
A process-lifetime, operating-system-backed exclusive lease that is the authoritative Single User Instance and CA lifecycle ownership signal. A contender must acquire it before verification, cleanup, publication, or ownerless CA work and fails immediately when another process holds it; the Gateway State Cache remains discovery data rather than ownership authority.
_Avoid_: Gateway State Lease, cache ownership watcher, verify-then-claim, advisory lock, waiting owner queue

**Gateway State Verification**:
A read-only Gateway Coordination behavior where an existing Gateway State Cache is checked through the HTTP Router before the gateway treats another Gateway Owner as active.
_Avoid_: Runtime State Verification, file-exists-is-running, port-only lock, stale state as active instance, cleanup validation

**Ownership Marker**:
A stable property proving a machine resource belongs to seamless-cors and may be modified or removed by gateway lifecycle cleanup.
_Avoid_: heuristic ownership, name-only matching, user intent guess

**Marker-Based Cleanup**:
A cleanup behavior where the gateway scans current machine state and removes resources only when an Ownership Marker proves the resource belongs to seamless-cors.
_Avoid_: footprint-based cleanup, previous-state restoration, guessed ownership

**Gateway Footprint Cleanup Status**:
A subject-level three-state result describing whether owned gateway footprint is `none`, `needed`, or `unknown`; `unknown` means current machine state could not be inspected and must not be treated as clean. The overall state is derived as `needed` when any subject is needed, otherwise `unknown` when any subject is unknown, and otherwise `none`.
_Avoid_: cleanup-needed boolean, assumed-clean inspection failure, suppressed cleanup inspection error

**Managed PAC Ownership Marker**:
The stable loopback HTTP PAC URL shape whose path ends in `seamless-cors.pac`, proving a current managed PAC setting belongs to seamless-cors without depending on a run-specific port.
_Avoid_: managed PAC footprint, run-specific PAC identity, port-based ownership, full-URL ownership, non-loopback PAC ownership

**Managed PAC Service Set**:
The network services selected during Gateway Activation for PAC Routing installation, PAC refresh, and Managed PAC Lease checks throughout that Gateway Owner run. A selected service remains a member while temporarily absent, and services first appearing after selection are ignored until a later Gateway Activation.
_Avoid_: live service discovery for expansion, current service list, implicit service expansion, removal-on-disappearance, plan-time service lock

**Managed PAC Session**:
A Gateway Activation-scoped behavior that installs PAC Routing for at least one visible member of the Managed PAC Service Set, tracks the current PAC URL Version and attempted PAC URL Version during refresh, and evaluates Managed PAC Lease while the Gateway Owner is live. Temporarily absent selected services remain members, but Gateway Activation fails if installation reaches no selected service; Gateway Footprint Cleanup remains responsible for marker-based PAC removal.
_Avoid_: runtime PAC state, PAC helper, cleanup owner, platform PAC wrapper, implicit refresh state

**Managed PAC Mutation Sequence**:
An owner-local ordering rule where PAC installation, refresh, lease reconciliation, and reattachment execute one at a time for a Managed PAC Session. Cancellation closes the sequence and waits for the current mutation before Gateway Footprint Cleanup, preventing any PAC write after cleanup.
_Avoid_: concurrent PAC writes, refresh-cleanup race, post-stop PAC install, global lifecycle mutex

**Managed PAC Lease**:
A Gateway Owner's ownership rule that treats the installed seamless-cors Managed PAC URL as live state for visible services in the Managed PAC Service Set, reconciled at explicit lifecycle, PAC refresh, and status boundaries rather than continuously. Temporarily absent selected services do not lose the lease, newly visible unselected services remain outside it, owned reappearing services are reattached, and empty or foreign reappearing services lose the lease.
_Avoid_: foreign PAC cleanup, silent proxy escape, best-effort PAC ownership, missing-service failure, implicit service expansion, continuous OS proxy polling, idle lease watcher

**Managed PAC Reattachment**:
A boundary-driven Managed PAC Session behavior where a visible selected service carrying any seamless-cors Managed PAC Ownership Marker is rewritten to the session's current PAC URL and enabled. Temporarily absent services wait for a later boundary; empty or foreign PAC state is not repaired and instead causes Managed PAC Lease Lost.
_Avoid_: idle watcher, new-service adoption, foreign PAC replacement, empty-state takeover, exact-old-URL rejection, missing-service failure

**Managed PAC Lease Lost**:
A user-facing fatal runtime condition where the gateway reports `managed-pac-lease-lost` when a visible selected service has empty or foreign PAC state, then gives restart or cleanup guidance without taking over or restoring that state.
_Avoid_: raw lease error, silent exit, forced PAC restoration, ambiguous runtime failure

**CA Ownership Marker**:
The strict seamless-cors-owned current-user CA trust identity used to identify Installed User CA trust for CA lifecycle management.
_Avoid_: CA footprint, name-contains matching, system-wide CA cleanup, user-authored CA identity

**Cleanup Retry Guidance**:
A user-facing cleanup behavior where failed cleanup explains that seamless-cors-owned state remains and tells the user to run `seamless-cors stop` again after resolving the OS or permission problem.
_Avoid_: silent cleanup failure, false cleanup success, manual OS instructions first

**Single User Instance**:
A gateway ownership rule where only one Gateway Owner may run for a user at a time, with the Gateway State Cache used as the first signal that an owner may already be active.
_Avoid_: multi-instance gateway, competing PAC state, port-based instance detection

**Configuration Bootstrap**:
A start-only behavior where the fixed Upstream List is created automatically when missing before validation continues, including required parent directories. The same start command continues with an Empty Upstream List, while passive reads remain non-mutating and an existing unsafe, non-file, or unreadable Upstream List remains an error.
_Avoid_: init command, manual file scaffolding, read-time mutation, configurable Upstream List path, replacing invalid paths

**Start Guidance**:
A start-time user-facing output behavior shown only after PAC consent has succeeded, Gateway Runtime is serving, and Managed PAC installation has reached at least one selected service. It points to the Upstream List, HTTPS Readiness, warnings, and managed state instead of runtime listener endpoints.
_Avoid_: pre-consent running message, listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

**Start Guidance Detail**:
A surface-neutral successful start result detail containing the user-relevant Upstream List and lifecycle state needed to render Start Guidance without exposing runtime listener endpoints.
_Avoid_: terminal start text, listener status detail, proxy setup instructions

**Already-Running Start**:
An idempotent start result where executing start against an active Gateway Runtime reports that the gateway is already running without treating the command as a failure.
_Avoid_: duplicate runtime activation, start failure for active runtime, second owner

**Execute-Time Start Assessment**:
A start execution rule where every `ExecuteStart` attempt computes current PAC Replacement Consent conditions before mutating, returning a structured consent-required result when supplied confirmation is missing, refers to different foreign PAC state, or is otherwise no longer sufficient.
_Avoid_: start plan, prior-result authorization, mutation-before-assessment

**Single-Flight Start**:
A start behavior where a Gateway Owner accepts only one complete Start Sequence at a time, acquiring exclusivity before cleanup and holding it through Upstream List loading, HTTPS Readiness assessment, PAC assessment, Gateway Activation, and the returned outcome. Concurrent attempts return already-running or start-already-mutating without duplicating lifecycle work.
_Avoid_: cross-command lifecycle lock, CA-command blocking, activation-only lock, queued start, duplicate mutation, competing activation, start plan reservation

**Stop-Preempted Start**:
A lifecycle precedence rule where `stop` cancels and supersedes an in-progress Start Sequence, waits for safe boundaries, then performs final Gateway Footprint Cleanup and ends ownership. Cancelled activation cannot later publish runtime or install PAC state.
_Avoid_: stop-busy result, start mutex wait, cleanup-before-cancellation, post-stop PAC install

**Stop-Cancelled Start**:
A surface-neutral expected start outcome returned to the original start caller after stop preemption reaches a safe boundary without treating cancellation as an infrastructure failure.
_Avoid_: context-canceled error, started result, stop failure

**PAC Replacement Consent Detail**:
A surface-neutral description of every service in the proposed Managed PAC Service Set, explicitly identifying every foreign PAC entry covered by one collective agreement, together with the PAC Replacement Consent Fingerprint and no-restoration cleanup behavior.
_Avoid_: service-selection UI, lifecycle consent detail, prompt text, OS trust approval payload, start plan token

**PAC Replacement Consent Fingerprint**:
A stable identity derived from the sorted network-service name and foreign PAC URL pairs that PAC Replacement Consent would authorize Gateway Activation to overwrite. Enabled flags, owned or empty PAC entries, and source ordering do not affect this identity.
_Avoid_: start plan token, full PAC state hash, enabled-state authorization, generic consent flag

**PAC Replacement Consent**:
State-bound, all-or-nothing user confirmation required when gateway start would overwrite one or more non-owned configured PAC URLs in the proposed Managed PAC Service Set, returned by an initial Gateway Activation attempt and reassessed on a consent-bearing retry before mutation. Consent collectively covers every identified foreign entry and applies only while that foreign PAC state matches what was shown; declining aborts Gateway Activation rather than narrowing the service set.
_Avoid_: per-service selection, partial activation, consent for empty or owned PAC state, generic replacement consent, silent proxy replacement, proxy chaining, broad proxy takeover

**Independent PAC Lifecycle**:
A lifecycle boundary where PAC Replacement Consent and PAC Routing setup follow gateway start independently of whether the Upstream List currently has active entries.
_Avoid_: domain-gated PAC setup, delayed proxy ownership, route-count-based lifecycle

**CA Trust Consent**:
A platform approval moment required before adding or replacing Installed User CA trust for HTTPS interception, with gateway context shown only when the platform requires approval.
_Avoid_: implicit CA trust, repeated consent for unchanged trust, app-only trust prompt, PAC Replacement Consent Detail

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Installed User CA mutation occur only through explicit CA Lifecycle Commands rather than gateway start or the Upstream List. Gateway Runtime may be updated as a consequence, while runtime stop does not cancel admitted CA work and owner exit waits for that work to settle.
_Avoid_: start-time CA trust, stop-cancelled CA command, intent-triggered installation, route-dependent trust setup

**Start Sequence Order**:
A startup lifecycle order where Gateway Footprint Cleanup and Upstream List validation precede PAC Replacement Consent assessment; HTTPS Readiness is assessed without mutating trust before Gateway Runtime serves; Gateway Runtime begins serving before Managed PAC installation; and Start Guidance follows successful installation on at least one selected service.
_Avoid_: start-time CA installation, PAC-before-runtime serving, PAC-first start, cleanup-after-approval, start guidance before PAC installation

**All-Service PAC Management**:
A Managed System Proxy behavior where Gateway Activation selects every network service the supported platform adapter manages at activation time, so PAC Routing is consistent across the Managed PAC Service Set for that Gateway Owner run.
_Avoid_: active-service-only PAC, partial network setup, silent service expansion

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and viewing gateway status while runtime behavior follows Live Configuration.
_Avoid_: command-heavy configuration, flag-driven operation

**CA Lifecycle Commands**:
Top-level user-facing commands that explicitly install, repair, or remove the Installed User CA outside the normal start/stop gateway loop. Install performs HTTPS Readiness Recovery when needed, while uninstall remains available during gateway operation and requires confirmation only when Trusted HTTPS Interception is active.
_Avoid_: nested CA command tree, hidden CA removal, per-start CA trust, config editing command, separate readiness command

**Upstream-Independent CA Install**:
A CA lifecycle command boundary where installing or repairing the Installed User CA does not read, require, create, or modify the Upstream List. When a gateway is running with not-ready HTTPS Readiness, successful install performs immediate HTTPS Readiness Recovery.
_Avoid_: install-time configuration bootstrap, intent-dependent install, separate readiness endpoint, restart-required recovery

**UserCA Install Reconciliation**:
An install order that first attempts Non-Active UserCA Cleanup, then reuses a valid Active UserCA for HTTPS Interception Reset, repairs its missing OS trust when required, or installs/rotates authority state that is invalid, expired, mismatched, or renewal-due. Failed cleanup blocks only work that would add another trusted root: reset may still reactivate interception from valid Active, while required rotation stops before Candidate creation; discovering missing active trust immediately makes HTTPS Readiness not-ready until repair succeeds.
_Avoid_: proxy failure-triggered CA rotation, trust repair before non-active reconciliation, arbitrary non-active adoption, unbounded trusted roots

**Idempotent CA Install**:
A CA lifecycle command behavior where installing reuses valid Active UserCA trust without requesting platform approval or changing CA material, including for HTTPS Interception Reset while a trusted Gateway Runtime is active.
_Avoid_: reinstalling valid CA, proxy failure-triggered rotation, noisy no-op install, repeated trust approval

**Active HTTPS Uninstall Consent**:
A confirmation required before UserCA uninstall disables active Trusted HTTPS Interception and removes the entire Installed UserCA Set. Consent authorizes that identity-independent consequence rather than one Active fingerprint; declining leaves HTTPS Readiness and all UserCA state unchanged, and no confirmation is required when interception is already inactive.
_Avoid_: certificate-bound consent, active-runtime uninstall block, unconditional uninstall prompt, partial UserCA removal, implicit consent

**Live UserCA Uninstall**:
A confirmed UserCA uninstall behavior whose linearization point atomically changes HTTPS Readiness to not-ready so no new handshake selects the Active UserCA. Before that point cancellation changes nothing; afterward uninstall is non-cancellable, runtime/PAC deactivation and removal of all current-user trust, marker, and local material proceed concurrently without draining handshakes, and incomplete removal remains not-ready with an actionable retry warning.
_Avoid_: stop-required uninstall, gateway shutdown, handshake drain, new handshake after revocation begins, readiness rollback after partial removal, Upstream List mutation

**Partial CA Command Success**:
A partial CA Lifecycle Command outcome where UserCA mutation remains complete but Gateway Runtime adoption or Managed PAC refresh fails. The Gateway reports the failed consequence for retry without rolling back, replacing, or disguising the durable UserCA result.
_Avoid_: CA rollback after Gateway failure, unnecessary CA replacement, false full success, hidden partial success

**Upstream-Independent CA Uninstall**:
A CA lifecycle command boundary where removing the Installed User CA does not modify the Upstream List.
_Avoid_: uninstall editing HTTPS Intent, config-coupled removal

**Idempotent CA Uninstall**:
A CA lifecycle command behavior where uninstalling reports already-absent seamless-cors-owned CA trust and local CA material as success without changing configuration or requiring repair.
_Avoid_: missing-CA uninstall failure, forced repair before removal, noisy no-op uninstall

**Complete CA Uninstall**:
A CA lifecycle invariant where uninstall removes Active, Candidate, and non-active recovery state and reports success only after all seamless-cors-owned current-user CA trust, markers, and local material are absent.
_Avoid_: false uninstall success, trusted CA without local material, leftover private key

**Foreground Start**:
A v1 runtime behavior where `start` runs attached in the foreground rather than launching an official background daemon.
_Avoid_: daemon mode, background start

**Client Command**:
A command invocation that asks an existing Gateway Owner to perform user-facing gateway work and then exits without owning process lifetime or Gateway Footprint Cleanup.
_Avoid_: detached owner, fake foreground control, remote Ctrl-C ownership

**Owner-Routed CA Lifecycle Command**:
A CA Lifecycle Command behavior where work is sent to an existing Gateway Owner or publishes a Transient Gateway Owner when none exists. This keeps UserCA mutation available during a long-running gateway while the owner coordinates HTTPS Readiness and Managed PAC consequences.
_Avoid_: bypassing owner command authority, ownerless local mutation, separate CA Mutation Lease, separate readiness endpoint, blanket active-runtime rejection

**Gateway Footprint Cleanup**:
An idempotent, ownership-aware lifecycle behavior that removes only stale or intentionally released seamless-cors-owned managed PAC and Gateway State Cache subjects while leaving Installed User CA state untouched. Direct start holds the Gateway Ownership Lease while removing stale cache and owned PAC, router-hosted start removes owned PAC while preserving its live owner cache, and stop removes both when ending ownership.
_Avoid_: unconditional cache removal, live-owner eviction, runtime cleanup, status cleanup, serve-start cleanup, broad cleanup, CA removal, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Human HTTPS Status**:
A compact Human Status rendering of `https: active` when HTTPS Interception State is active and `https: inactive` otherwise, followed by actionable current HTTPS Warnings. Internal HTTPS Readiness, Interception State reasons, and redundant active fields are not printed as separate normal-status fields.
_Avoid_: `https-interception-health`, `trusted-https-active`, internal state dump, warning-free inactive status

**Read-Only Status**:
A status behavior that reports gateway, cleanup-needed, Installed User CA, Human HTTPS Status, and stale Gateway State Cache detection without latching HTTPS Readiness Loss or changing proxy settings, CA trust, local CA material, runtime files, or discovery state. An existing owner reports its latched UserCA Snapshot, an ownerless command uses Ownership-Protected Status Assessment, and admitted CA work is reported as `userca: mutating`.
_Avoid_: status-triggered cleanup, mutating status command

**Gateway Status State**:
A read-only gateway status vocabulary that describes whether the Gateway Owner and Gateway Runtime are absent, stale, router-only, starting, or running without encoding cleanup, HTTPS Readiness, or UserCA Usability.
_Avoid_: cleanup status, UserCA state, start result, runtime state file truth

**UserCA Usability**:
A two-state assessment where UserCA is `usable` only when one valid Active UserCA has matching local material and current-user OS trust, and is otherwise `not-usable`. Renewal due is an independent fact; private cleanup state does not cross the UserCA seam, assessment failure is an error, and `mutating` belongs to Gateway command coordination rather than UserCA state.
_Avoid_: public missing/expired/mismatched state taxonomy, unknown UserCA state, public cleanup state, mutation-as-UserCA-state

**UserCA Snapshot**:
An immutable semantic result freshly inspected by UserCA from authority material, the Active fingerprint marker, and current-user OS trust. It exposes UserCA Usability at inspection, expiry, renewal due, and defensive generic TLS signing material when usable; UserCA never caches or observes it, while Gateway Runtime may latch an admitted Snapshot and derive effective expiry with its own clock.
_Avoid_: exported Active authority type, raw PEM, CA storage paths, cached CA state, live CA watcher, mutable authority record, storage snapshot, public trust-store facts

**Diagnostic Runtime Endpoint**:
An automatically selected listener address shown by status for troubleshooting, not for user proxy setup or configuration.
_Avoid_: setup address, configured listener, manual proxy instruction

**Upstream List**:
The user-managed newline-delimited configuration at `~/.seamless-cors/upstreams.txt`, decoded by the Upstream List module into Host Selectors, Origin Selectors, and Upstream List Warnings for PAC Routing. Live Configuration reads and observes this ordinary-file source, whose live change observation is supported only on a local filesystem.
_Avoid_: Domain List, Target List, configurable Upstream List path, symlinked list, network-filesystem observation guarantee, proxy admission list, interception rules, proxy rules

**Upstream List Comment**:
A full-line or inline note in the Upstream List that is ignored during matching.
_Avoid_: comment-as-entry

**Empty Upstream List**:
A valid Upstream List state with no active entries, including a file that contains only comments, blank lines, or invalid lines carrying Upstream List Warnings; the gateway keeps managed PAC Routing installed and matches no upstreams until valid Upstream List Entries are added.
_Avoid_: startup failure for no active entries, proxy-all fallback

**Upstream List Warning**:
A persistent line-level diagnostic for an invalid Upstream List line that is ignored while other valid Upstream List Entries remain active. Warning appearance, change, and clearing publish a new Live Configuration Snapshot for successful startup and runtime status rather than asynchronous notification; they do not advance Upstream List Entries Revision unless the valid entry set also changes.
_Avoid_: silent invalid entry, fatal line error, transient log warning, asynchronous warning event, routing revision warning

**Fatal Upstream List Error**:
A live configuration behavior where a missing, unreadable, or structurally undecodable Upstream List reports the source problem, performs Gateway Footprint Cleanup, and stops the gateway. Individual invalid lines are Upstream List Warnings rather than fatal errors.
_Avoid_: stale valid routing, unreadable-as-empty, silent source failure

**Upstream List Entry**:
A normalized routing value decoded by the Upstream List module as either a Host Selector or an Origin Selector. Internal consumers that construct entries directly are responsible for satisfying the same normalized value contract.
_Avoid_: source-text-bearing entry, rule, matcher expression

**Host Selector**:
An Upstream List Entry variant containing a lowercase ASCII hostname and Hostname Match without a scheme or port, selecting matching hosts across HTTP and HTTPS on any port. Host Selector wildcard syntax is interpreted only for this variant, and IP literal spelling is not canonicalized.
_Avoid_: Domain Selector, Hostname Selector, Hostname Shorthand, scheme-less origin, port-qualified domain

**Host Route**:
A scheme-qualified hostname match derived from a Host Selector for the PAC Route Set. A Host Selector derives an HTTP Host Route and, when Trusted HTTPS Interception is enabled, an HTTPS Host Route; each route matches its hostname pattern on any port.
_Avoid_: Domain Route, Origin Route, port-qualified domain, PAC-owned selector

**Origin Selector**:
An Upstream List Entry variant containing an HTTP(S) scheme, lowercase ASCII hostname, and optional normalized explicit port, matched exactly without applying Host Selector wildcard semantics. Port presence is part of selector identity, so an omitted port and the scheme's explicit default port remain distinct Origin Selectors; accepted explicit ports are not range-validated, and IP literal spelling is not canonicalized, so a valid Origin Selector is not guaranteed to identify a browser-reachable origin.
_Avoid_: Full Origin, URL selector, scheme-qualified domain

**Origin Route**:
An exact origin representation derived from an Origin Selector for the PAC Route Set. PAC Routing owns default-port interpretation: an HTTP selector always derives Origin Routes, while an HTTPS selector derives them only when Trusted HTTPS Interception is enabled; a selector with an omitted or explicit default port derives both implicit-port and explicit-port Origin Routes, while any other port derives one Origin Route. Equivalent derived routes are deduplicated.
_Avoid_: duplicate Origin Selector, URL rule, inferred PAC port

**Hostname Match**:
The explicit Host Selector meaning that selects an exact hostname, exactly one leading subdomain label, or one-or-more leading subdomain labels without encoding that meaning in the normalized hostname.
_Avoid_: wildcard-bearing hostname, consumer-parsed wildcard

**Upstream List Routing Policy**:
A runtime interpretation owned by the PAC Routing module that decides whether normalized Upstream List Entries send a browser request to the Proxy Listener without revalidating them. Gateway Runtime supplies entries selected from the Live Configuration Snapshot rather than the snapshot itself.
_Avoid_: whole Live Configuration Snapshot dependency, proxy admission policy, raw string matching, duplicated PAC matchers, downstream Upstream List validation

**Line-Level Upstream Validation**:
An Upstream List behavior where each line is validated independently so valid Upstream List Entries are applied while invalid lines are ignored and reported precisely as Upstream List Warnings.
_Avoid_: Line-Level Domain Validation, silent invalid entry, whole-list rejection, invalid line as active entry

**Upstream List Deduplication**:
An Upstream List module behavior where equivalent normalized source-level entries are treated as one active entry, keeping the first occurrence and ignoring later duplicates. Port presence is part of Origin Selector identity; PAC Routing separately deduplicates equivalent derived Origin Routes.
_Avoid_: duplicate source selectors, line-count domains, PAC-owned source deduplication

**Exact Host Match**:
A Host Match that selects only the named hostname.
_Avoid_: Exact Domain Match, implicit subdomain match, broad domain match

**Single-Label Wildcard**:
A Host Match written as `*.example.com` that selects exactly one leading subdomain label and does not select the parent domain or deeper subdomains.
_Avoid_: recursive wildcard, parent-domain wildcard, wildcard-bearing hostname

**Recursive Wildcard**:
A Host Match written as `**.example.com` that selects one or more leading subdomain labels at any depth without selecting the parent domain.
_Avoid_: zero-label wildcard, parent-domain wildcard, wildcard-bearing hostname

**Local Upstream**:
A localhost, loopback, private IP, or plain HTTP upstream entry that may be included in the Upstream List for DEV/QA work.
_Avoid_: Local Target, public-domain-only upstream, DNS-only upstream

**Bracketed IPv6 Selector**:
An Upstream List Entry source form for an IPv6 upstream that uses bracketed host syntax with an optional HTTP(S) scheme and port.
_Avoid_: unbracketed IPv6 selector, IPv6 Full Origin

**Reflective DEV/QA Policy**:
The default cross-origin behavior for traffic that reaches the Proxy Listener, where the gateway reflects the browser request's origin and requested CORS capabilities so credentialed development and testing flows are browser-valid.
_Avoid_: wildcard CORS, production CORS policy, allow-all policy

**Credentialed Reflection**:
A Reflective DEV/QA Policy behavior where Origin-gated responses reaching the Proxy Listener always include `Access-Control-Allow-Credentials: true` with a reflected request origin.
_Avoid_: wildcard credentials, credentialless default

**Null Origin Reflection**:
A Credentialed Reflection behavior where `Origin: null` is reflected for DEV/QA requests reaching the Proxy Listener.
_Avoid_: null origin rejection

**Origin Vary Preservation**:
A Response Repair behavior where existing `Vary` values are preserved and `Origin` is added when absent.
_Avoid_: Vary clobbering, duplicate Origin Vary

**Requested Header Reflection**:
A Reflective DEV/QA Policy behavior where preflight responses echo the browser's requested header list instead of using a wildcard.
_Avoid_: wildcard allow-headers

**Requested Method Reflection**:
A Local Preflight Answer behavior where preflight responses echo the browser's requested method instead of returning a broad method list.
_Avoid_: broad allow-methods

**Global CORS Policy**:
A gateway behavior where every request reaching the Proxy Listener uses the same Reflective DEV/QA Policy instead of per-domain policy settings.
_Avoid_: per-domain CORS policy, domain-specific overrides

**Origin-Gated Rewriting**:
A gateway behavior where cross-origin response changes are applied only when the browser request includes an `Origin` header.
_Avoid_: blanket rewriting, unconditional CORS headers

**Local Preflight Answer**:
A gateway behavior where browser CORS preflight requests that reach the Proxy Listener are answered by the gateway instead of being forwarded upstream.
_Avoid_: upstream preflight, preflight repair

**Private Network Access Reflection**:
A Local Preflight Answer behavior where requests reaching the Proxy Listener that ask for private network access receive `Access-Control-Allow-Private-Network: true`.
_Avoid_: PNA omission for local targets

**Fixed Preflight Cache**:
A Local Preflight Answer behavior that uses a fixed `Access-Control-Max-Age` of 600 seconds.
_Avoid_: configurable preflight cache, indefinite preflight cache

**Response Repair**:
A gateway behavior where real upstream responses for requests that reach the Proxy Listener are adjusted on the way back to satisfy the Reflective DEV/QA Policy.
_Avoid_: request rewrite, upstream configuration

**All-Status Repair**:
A Response Repair behavior where Origin-gated upstream responses reaching the Proxy Listener receive CORS repair regardless of upstream status code.
_Avoid_: success-only repair, hidden API error

**No Request Header Rewriting**:
The product boundary that leaves browser request headers unchanged except for ordinary proxy transport mechanics.
_Avoid_: Origin rewriting, Referer rewriting, auth header mutation

**CORS Header Replacement**:
A Response Repair behavior where existing upstream CORS headers are removed before the gateway writes the Reflective DEV/QA Policy headers.
_Avoid_: CORS header merge, duplicate CORS headers

**Concrete Exposed Headers**:
A Response Repair behavior where `Access-Control-Expose-Headers` lists actual upstream response header names, excluding CORS headers, for maximum browser compatibility.
_Avoid_: wildcard expose-headers

**HTTP CORS Scope**:
The product boundary that focuses gateway repair on browser HTTP/HTTPS CORS requests and leaves WebSocket protocol behavior out of scope.
_Avoid_: WebSocket repair, protocol frame rewriting

**WebSocket Skip**:
A gateway behavior where WebSocket upgrade requests are forwarded without CORS repair or protocol-specific changes.
_Avoid_: WebSocket origin bypass, upgrade repair

**Cookie Out of Scope**:
The product boundary that leaves cookie attributes, cookie values, sessions, and authentication behavior unchanged.
_Avoid_: cookie repair, auth bypass, session rewriting

## Example Dialogue

Developer: "Can I point my browser traffic through seamless-cors for the staging API?"

QA engineer: "Yes, but only for configured upstream domains so unrelated browsing stays outside the gateway."

Developer: "Do I need to configure my browser proxy manually?"

QA engineer: "No, the Managed System Proxy handles that while the gateway is running."

Developer: "Will unrelated browser traffic go through the gateway?"

QA engineer: "No, Selective Managed Proxy uses PAC Routing so only Upstream List matches go through the gateway."

Developer: "When will HTTPS domains route through the gateway?"

QA engineer: "Trust-Aware PAC Routing sends them through the gateway only while HTTPS Readiness is ready."

Developer: "Do I need to maintain the PAC file?"

QA engineer: "No, Generated PAC is derived from Live Configuration and the Upstream List."

Developer: "How do Upstream List changes reach the operating system proxy?"

QA engineer: "The PAC Endpoint serves the current Generated PAC, and the gateway refreshes the owned PAC URL Version when supported PAC clients need a new URL to observe the update."

Developer: "Can I avoid changing my system proxy settings?"

QA engineer: "No, the gateway uses Managed System Proxy so application requests keep their original URLs."

Developer: "What if I decline the PAC Replacement Consent prompt?"

QA engineer: "Start stops without changing machine proxy settings because there is no manual proxy fallback."

Developer: "Will the gateway configure Firefox or browser profile certificate stores?"

QA engineer: "No, OS Trust Only keeps certificate trust limited to the current user's operating-system trust store."

Developer: "What happens when HTTPS Readiness is not ready?"

QA engineer: "HTTPS traffic stays direct, HTTP gateway service continues, and HTTPS Intent produces an actionable warning."

Developer: "Will the first run automatically trust a CA?"

QA engineer: "No. Gateway start only assesses HTTPS Readiness; `seamless-cors install` is the explicit operation that requests platform approval."

Developer: "Will the gateway keep reusing the same development CA?"

QA engineer: "Yes. Installed User CA reuses trusted CA material across trusted gateway starts until it is removed or replaced."

Developer: "What removes trusted CA material?"

QA engineer: "CA lifecycle commands remove seamless-cors-owned CA trust and local CA material."

Developer: "What happens if the gateway crashes before removing its CA?"

QA engineer: "Installed User CA remains available for the next trusted gateway start unless the user removes it."

Developer: "Will every operating system have the same managed setup in v1?"

QA engineer: "Every supported platform needs a managed PAC adapter; platforms without one are not supported yet."

Developer: "After I update the Upstream List, do I need to restart the gateway?"

QA engineer: "No, Live Configuration applies the newest values to incoming requests."

Developer: "What happens if I save an invalid config file while the gateway is running?"

QA engineer: "Fatal Config Error reports the validation problem, performs Gateway Footprint Cleanup, and stops the gateway."

Developer: "What if my config still has removed listener or managed-proxy settings?"

QA engineer: "Lenient Configuration Shape treats them like any other unknown settings, so they do not affect gateway behavior."

Developer: "Do I need a command for every setting?"

QA engineer: "No, the Minimal Command Surface keeps commands rare and lets configuration drive behavior while the gateway is running."

Developer: "Which commands exist in v1?"

QA engineer: "`start`, `stop`, and `status` manage the gateway runtime; CA Lifecycle Commands manage Installed User CA trust."

Developer: "Does `start` launch a background service?"

QA engineer: "No, Foreground Start keeps the gateway attached and lets Ctrl-C run Gateway Footprint Cleanup in v1."

Developer: "Does Ctrl-C clean up the proxy and CA?"

QA engineer: "Ctrl-C runs Gateway Footprint Cleanup for seamless-cors-owned managed PAC settings and the Gateway State Cache, but Installed User CA trust remains until a CA Lifecycle Command removes it."

Developer: "What if stop finds only cleanup-needed state?"

QA engineer: "Gateway Footprint Cleanup finishes removing seamless-cors-owned managed PAC settings and the Gateway State Cache before stop exits."

Developer: "Is status intended for scripts?"

QA engineer: "No, Human Status is optimized for interactive understanding."

Developer: "Can status change proxy or CA state?"

QA engineer: "No, Read-Only Status reports state without performing cleanup."

Developer: "Why does status show listener addresses if I cannot configure them?"

QA engineer: "Diagnostic Runtime Endpoint values are shown for troubleshooting, not setup."

Developer: "What if status finds a stale Gateway State Cache?"

QA engineer: "Read-Only Status reports that the gateway is not running and leaves cleanup to start or stop."

Developer: "Can editing the Upstream List unexpectedly trigger an OS permission prompt?"

QA engineer: "No. HTTPS Intent can produce an Unmet HTTPS Intent warning, but only `seamless-cors install` mutates UserCA trust."

Developer: "What happens if a default listener port is already in use?"

QA engineer: "Automatic Listeners choose available loopback ports at startup."

Developer: "Can other machines use my gateway by default?"

QA engineer: "No, Loopback Default keeps listener endpoints local."

Developer: "Do stop and status go through the proxy listener?"

QA engineer: "No, Gateway Router keeps command traffic separate from browser proxy traffic."

Developer: "Do I need to know which listener browsers use?"

QA engineer: "No, PAC Routing connects browsers to the Proxy Listener through managed proxy settings."

Developer: "What happens if I install UserCA while the gateway is already running?"

QA engineer: "The existing install command performs HTTPS Readiness Recovery immediately and refreshes HTTPS PAC routing without a restart."

Developer: "What happens if the gateway crashes after changing my proxy settings?"

QA engineer: "Gateway Footprint Cleanup removes leftover seamless-cors-owned managed PAC settings before a new start or stop finishes."

Developer: "What if I already use a corporate proxy?"

QA engineer: "PAC Replacement Consent shows the current managed PAC state and asks before replacing existing PAC settings for this run."

Developer: "What do I need to configure before starting?"

QA engineer: "Only the Upstream List: one Host Selector or Origin Selector per line."

Developer: "Where do config files live by default?"

QA engineer: "Home Config Directory keeps them under `.seamless-cors` in the user's home directory."

Developer: "Where does the Gateway State Cache live?"

QA engineer: "Runtime State Directory keeps runtime coordination state and product-owned cleanup files under the Home Config Directory."

Developer: "How do client commands find the Gateway Owner?"

QA engineer: "They read the Gateway State Cache to find the active Gateway Router and authenticate with its token."

Developer: "Does a Gateway State Cache always mean the gateway is still running?"

QA engineer: "No, Gateway State Verification checks the Gateway Router before treating it as an active owner."

Developer: "Will cleanup modify state just because it looks suspicious?"

QA engineer: "No, Marker-Based Cleanup acts only on state proven by a seamless-cors Ownership Marker."

Developer: "Can I run two gateway owners at once?"

QA engineer: "No, Single User Instance uses the Gateway State Cache to guard active gateway ownership."

Developer: "Can the gateway start with no upstreams?"

QA engineer: "Yes, Empty Upstream List is valid, so the gateway runs while PAC Routing matches no upstreams until valid Upstream List Entries are added."

Developer: "Do I need to run an init command first?"

QA engineer: "No, Configuration Bootstrap creates the fixed Upstream List when missing, and Empty Upstream List lets that same start command keep running with no matched upstreams yet."

Developer: "Can I write just `api.dev.example.com`?"

QA engineer: "Yes, that Upstream List Entry is a Host Selector; use an Origin Selector when a scheme or port should constrain matching."

Developer: "Can I annotate upstreams in the Upstream List?"

QA engineer: "Yes, Upstream List Comment supports full-line and inline comments."

Developer: "Does a Host Selector include custom ports?"

QA engineer: "No, a Host Selector matches any port; use an Origin Selector to constrain the scheme and port."

Developer: "What if one Upstream List line is wrong?"

QA engineer: "Line-Level Upstream Validation ignores that line, reports an Upstream List Warning, and continues routing with the valid Upstream List Entries."

Developer: "What if I save an invalid Upstream List while the gateway is running?"

QA engineer: "Invalid lines produce Upstream List Warnings while valid entries are applied; Fatal Upstream List Error is reserved for a missing, unreadable, or structurally undecodable source."

Developer: "Does `api.dev.example.com` include its subdomains?"

QA engineer: "No, Exact Host Match requires an explicit wildcard when subdomains should be included."

Developer: "Does `*.example.com` match `deep.api.example.com`?"

QA engineer: "No, Single-Label Wildcard matches only one subdomain label; use Recursive Wildcard when deeper subdomains should also match."

Developer: "Can I include my local API or a LAN staging service?"

QA engineer: "Yes, Local Upstreams are allowed."

Developer: "Can I write IPv6 shorthand in the Upstream List?"

QA engineer: "Yes, use `[::1]` as a Host Selector or an origin such as `http://[::1]:3000`."

Developer: "Can credentialed browser requests work without configuring allowed origins?"

QA engineer: "Yes, the Reflective DEV/QA Policy reflects the request origin instead of using a wildcard."

Developer: "Are credentials allowed for CORS-repaired responses?"

QA engineer: "Yes, Credentialed Reflection always allows credentials with the reflected origin."

Developer: "What if the browser sends `Origin: null`?"

QA engineer: "Null Origin Reflection treats it as a valid DEV/QA origin value."

Developer: "What happens to existing `Vary` headers?"

QA engineer: "Origin Vary Preservation keeps existing values and adds `Origin` once."

Developer: "How are preflight request headers allowed?"

QA engineer: "Requested Header Reflection echoes the browser's requested header list."

Developer: "How are preflight request methods allowed?"

QA engineer: "Requested Method Reflection echoes the browser's requested method."

Developer: "Can each domain have a different CORS policy?"

QA engineer: "No, Global CORS Policy applies the same DEV/QA behavior to every request reaching the Proxy Listener."

Developer: "Will same-origin or non-browser traffic get CORS headers added?"

QA engineer: "No, Origin-Gated Rewriting leaves traffic without an `Origin` header unchanged."

Developer: "Does the upstream server need to handle preflight correctly?"

QA engineer: "No, Local Preflight Answer handles browser preflight and Response Repair handles the real upstream response."

Developer: "Are Private Network Access preflights handled?"

QA engineer: "Yes, Private Network Access Reflection allows PNA preflights that reach the Proxy Listener."

Developer: "Will `401` or `500` upstream responses still be readable by frontend code?"

QA engineer: "Yes, All-Status Repair applies CORS repair to upstream errors too."

Developer: "How long can browsers cache local preflight answers?"

QA engineer: "Fixed Preflight Cache uses 600 seconds."

Developer: "Will the gateway rewrite `Origin` if the upstream rejects it?"

QA engineer: "No, No Request Header Rewriting keeps upstream application checks out of scope."

Developer: "What if the API already returns partial CORS headers?"

QA engineer: "CORS Header Replacement removes them first so the browser sees one consistent DEV/QA policy."

Developer: "How are response headers exposed to frontend code?"

QA engineer: "Concrete Exposed Headers lists the upstream response headers instead of using a wildcard."

Developer: "Will the gateway fix WebSocket origin behavior?"

QA engineer: "No, HTTP CORS Scope keeps WebSocket protocol behavior out of v1."

Developer: "What if the WebSocket upstream is in the Upstream List?"

QA engineer: "WebSocket Skip still forwards it without gateway repair."

Developer: "Will the gateway rewrite cookies so login works?"

QA engineer: "No, Cookie Out of Scope leaves cookie and authentication behavior unchanged."
