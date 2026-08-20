# seamless-cors

seamless-cors is a DEV/QA context for controlled browser-origin testing across configured upstream domains.

## Language

**seamless-cors**:
A local DEV/QA network tool that sits between the browser and configured upstream domains so browser requests can be tested under adjusted cross-origin behavior without changing application request URLs.
_Avoid_: generic proxy, CORS middleware

**Gateway Module**:
The single internal module that owns start, serve, stop, status, and Installed User CA lifecycle commands together with their semantic results, fulfillment, state classifications, and details for CLI and HTTP Gateway Control Surfaces. Its small public interface hides owner discovery, authenticated local HTTP transport, process ownership, Gateway Footprint Cleanup decisions, Managed PAC state, runtime visibility, UserCA lifecycle behavior, and traffic-runtime sequencing; Inbound Adapters translate Gateway semantics without redefining them.
_Avoid_: surface-owned outcome, CLI result classification, HTTP-defined command semantics, Gateway Facade, gateway client package, gateway coordinator package, gateway owner package, gateway router package, command service

**Inbound Adapter**:
An architectural role that translates an external interaction into calls through an inward module interface and translates the result back without being imported by that module. The CLI Inbound Adapter and the Gateway Router are Inbound Adapters even though they serve different Gateway Control Surfaces and live at different seams.
_Avoid_: controller layer, delivery layer, outer module imported by Gateway, directory-only classification

**CLI Inbound Adapter**:
The terminal-facing Inbound Adapter that translates seamless-cors process arguments, terminal input, and process signals into calls to the appropriate Gateway or Version Module, then renders the result as terminal output and command failure. It does not own Gateway command semantics or process composition.
_Avoid_: CLI-owned Gateway semantics, command service, composition root, Gateway-only adapter

**Gateway Control Surface**:
A user-facing interaction surface through which a Gateway Control Command is issued and its outcome is presented. CLI and authenticated local HTTP are Gateway Control Surfaces; this product role is distinct from the architectural Inbound Adapter role.
_Avoid_: Inbound Adapter, Gateway Module interface, transport protocol

**Gateway Feature Orchestration**:
A rule that only the Gateway Module orders and combines file-observation facts, Upstream List Projections, PAC Projections, UserCA facts, Managed PAC mutations, and Gateway Runtime state; feature modules never initiate another feature's lifecycle. The Upstream List controls PAC Routing but never constrains HTTPS interception or UserCA behavior.
_Avoid_: feature-owned lifecycle orchestration, duplicated selector translation, ordering-means-waiting

**Independent Feature Serialization**:
A concurrency rule where each feature module serializes only its own mutations, while Gateway atomically adopts or deactivates one immutable UserCA MITM generation during live UserCA installation, expiry, and uninstall. This Gateway boundary does not hold UserCA's private mutation lock or Managed PAC serialization, and PAC publication remains independent.
_Avoid_: global lifecycle lock, PAC-blocked UserCA mutation, list-coupled UserCA adoption

**Surface-Neutral Command Result**:
The authoritative semantic outcome of a Gateway Module operation, describing successful, blocked, retryable, and next-action-required command outcomes without terminal text, HTTP status codes, or surface-specific formatting. Every anticipated command condition produces such a result, while an error means the Gateway could not produce a semantic outcome; every Inbound Adapter translates results and errors into its Gateway Control Surface representation.
_Avoid_: CLI output, HTTP response model, shared wire model, semantic sentinel error, stringly command result, terminal error text

**Command Fulfillment**:
An intrinsic two-state property of a Surface-Neutral Command Result: `fulfilled` when the requested postcondition was satisfied or `unfulfilled` when Gateway produced a semantic result but could not satisfy it. Gateway fixes this property for each Operation-Specific Result Kind so every control surface translates the same classification; a failure that prevents Gateway from producing a semantic result has no Command Fulfillment.
_Avoid_: surface-defined success, HTTP-defined fulfillment, success-means-result-exists, non-HTTP-error, unclassified command failure, panic-only failure

**Operation-Specific Result Kind**:
A closed command result vocabulary scoped to one Gateway Module operation, including its anticipated coordination, user-decision, and next-action conditions, so each operation exposes only outcomes that can actually happen for that command. A control surface identifies a kind together with its already-known operation rather than promoting kinds into a global vocabulary.
_Avoid_: operation echoed in result, globally prefixed result code, global result code, shared outcome enum, exported control-flow error, impossible command state

**Gateway Router**:
The private authenticated-local-HTTP adapter inside the Gateway Module that owns HTTP request and response representations, exposes gateway feature routes, and translates between those representations and Surface-Neutral Command Results. It introduces a private representation type only where HTTP changes the semantic shape, reusing stable Gateway request and detail value records where shape and meaning are identical. It renders fulfilled results as bare operation-specific success bodies and every non-success HTTP response through the Gateway Error Response.
_Avoid_: success response envelope, echoed operation name, non-success-means-no-result, Gateway semantic interface, shared command model, runtime control endpoint, proxy route, daemon supervisor

**Gateway Error Response**:
The shared Gateway Router representation for every non-success HTTP command response, containing a stable machine-authoritative error code, optional structured detail, and non-authoritative human-readable message without imposing an envelope on successful responses. The route and an operation-scoped code together identify an unfulfilled Surface-Neutral Command Result, while Router-wide protocol and infrastructure codes mean Gateway produced no semantic result; clients never parse or reuse the message as Gateway semantics.
_Avoid_: authoritative error prose, message-based result reconstruction, success response shell, route-specific error shape, HTTP status duplicated in the body, non-success-means-client-error

**Gateway Client**:
A typed client-facing layer used by CLI and future user interfaces to discover and call an existing Gateway Owner's Gateway Router through the Gateway State Cache identity. It reconstructs fulfilled results from operation-specific success bodies and unfulfilled results from recognized operation-scoped Gateway Error Response codes, while network, malformed, Router-wide, and unknown responses remain errors.
_Avoid_: HTTP response leak, non-success-means-error, message-parsing client, command service, lifecycle client, generic JSON caller, managed gateway

**Gateway Owner**:
The module that holds the Gateway Ownership Lease and publishes Gateway Router discovery state for a long-running ownerless `serve` or `start` command or transient ownerless CA work. Once published, start, CA Lifecycle Commands, status, and stop address that owner, while competing serve fails.
_Avoid_: daemon supervisor, client command, detached runtime owner, terminal command renderer

**Gateway Host**:
The process-bootstrap role that establishes and keeps a Gateway Owner available independently of whether Gateway Runtime is activated. An ownerless start combines Gateway Hosting with the Start operation, Router-Only Serve hosts without starting, and an HTTP control surface can only address an already-hosted owner.
_Avoid_: CLI-owned Start semantics, implicit serve command, HTTP process bootstrap, Gateway Runtime

**Gateway Runtime**:
The live traffic-serving engine that owns the proxy listener, proxy server, PAC listener, PAC server, continuous Upstream List observation, current effective Upstream List Projection, optional Upstream List File Sync Issue, optional Upstream List Projection Issue, current PAC Projection, runtime close behavior, and fatal serving-error reporting without installing or unsetting OS PAC state. It begins only after initial observation has established those Upstream List conditions; feature degradation never ends it, while explicit Gateway stop or an irrecoverable proxy or PAC serving failure ends it coherently.
_Avoid_: initializing runtime, retained observation result, retained raw contents, lifecycle facade, command router, OS proxy manager, cleanup owner

**Router-Only Serve**:
A command behavior where the command becomes the Gateway Owner and starts the Gateway Router as an HTTP client entry point without automatically starting Gateway Runtime, running Gateway Footprint Cleanup at serve startup, or changing managed OS state; it fails clearly when a Gateway Owner already exists and may claim stale Gateway State Cache only after verification finds no reachable owner.
_Avoid_: implicit gateway start, daemonized start, hidden lifecycle activation, stale-cache cleanup, OS PAC repair

**Router-Hosted Start**:
A start behavior where CLI or another client calls `POST /start` against an existing Gateway Owner, renders each independently required Start consent in its fixed order, and retries with accumulated decisions to activate Gateway Runtime without creating a competing gateway process. The existing owner remains foreground, and an already-active runtime returns an idempotent start result.
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
A runtime proxy auto-configuration artifact rendered from the current PAC Projection, not edited directly by the user.
_Avoid_: user-authored PAC, manual PAC rules

**PAC Projection**:
The complete PAC Routing interpretation derived from the current Upstream List Projection, HTTPS Readiness, and runtime routing endpoint for both the live PAC Endpoint and Managed PAC publication. PAC Routing owns its formation and identity; Gateway adopts and coordinates changed projections, while Managed PAC does not reinterpret Upstream List semantics.
_Avoid_: Managed PAC desired Upstream List, Gateway-derived routes, duplicated PAC derivation, user-authored PAC

**PAC Route Set**:
The PAC Routes within a PAC Projection, derived inside the PAC Routing module from normalized Upstream List Entries and current HTTPS Readiness. Its identity is independent from Upstream List Projection Identity, so upstream projection changes that preserve effective routes do not imply a PAC Projection change.
_Avoid_: hand-built JavaScript rules, duplicated Upstream List parsing, PAC-owned Upstream List syntax

**PAC Endpoint**:
A local HTTP endpoint served by the gateway that returns the current Generated PAC.
_Avoid_: file PAC, static PAC file

**Managed PAC Publication URL**:
An owned PAC Endpoint identity carrying Managed PAC's publication generation in its URL query so PAC clients fetch a newer Generated PAC while the seamless-cors Managed PAC Ownership Marker remains stable.
_Avoid_: Gateway PAC version, routing revision, port rotation, foreign cache-busting parameter, PAC file version, browser cache workaround

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
A maintenance operation performed by explicit install that replaces an Installed User CA before expiry. An authority within 90 days of expiry remains valid for Trusted HTTPS Interception while producing a renewal warning, and runtime expiry detection directs the user to install rather than mutating CA material or OS trust automatically.
_Avoid_: traffic-triggered trust mutation, automatic root replacement, silent CA replacement, treating near-expiry as expired

**CA Replacement Rule**:
A CA lifecycle rule where a valid Active UserCA is reused, its missing trust or local permissions are repaired in place, and renewal-due authority rotates through an overlapping Candidate without interrupting active HTTPS. When the Active marker is absent, invalid, or does not identify valid material, install removes every ambiguous owned authority and verifies cleanup before creating a fresh Candidate rather than guessing an Active identity.
_Avoid_: newest-authority inference, unmarked authority adoption, adding a root beside ambiguous residue, proxy-failure-triggered replacement, destructive pre-candidate renewal, trusting invalid material, start-time repair

**UserCA Installation**:
The explicit UserCA operation that installs, repairs, or renews the current user's seamless-cors authority and requests platform approval only when trust must be added or replaced. It is independent of the Upstream List; a live Gateway atomically adopts the returned UserCA Signing Material before returning the command result.
_Avoid_: start-time CA installation, activation-owned CA setup, asynchronous live-install reconciliation, list-bound install result, repeated trust prompt, implicit trust repair

**Owner-Owned CA Mutation**:
An admitted install or uninstall belongs to the Gateway Owner and settles independently of request cancellation or client disconnection. Owner Stop waits for it, while process interruption relies on immutable generations, the Active fingerprint marker, and the next install or uninstall for recovery.
_Avoid_: request-owned mutation, disconnect cancellation, stop-cancelled CA command, caller-managed commit boundary

**Gateway-Owned CA Lifecycle**:
A lifecycle rule where install, UserCA Rotation, and uninstall route through an existing Gateway Owner or a discoverable Transient Gateway Owner published before ownerless work. Gateway Ownership provides cross-process routing and discovery, UserCA privately serializes its own mutations, and Gateway coordinates the short runtime adoption or deactivation consequence without blocking other features.
_Avoid_: ownerless CA mutation, undiscoverable ownership holder, separate CA Mutation Lease, direct UserCA command execution, caller-managed CA locking

**Transient Gateway Owner**:
A discoverable Gateway Owner published before ownerless CA lifecycle work. It exposes the Gateway Router and Gateway State Cache while coordinating one finite CA mutation; status reports `userca: mutating`, stop enters Owner Ending and waits, competing CA work and start fail fast, and the owner cannot be promoted into a long-running owner.
_Avoid_: promotable CA owner, install-owned Gateway Runtime, private one-shot lease holder, hidden CA process, background daemon, undiscoverable owner

**Fail-Fast CA Mutation Admission**:
A UserCA serialization rule where install and uninstall are rejected for explicit retry when another UserCA mutation is already admitted. Gateway maps that condition to `userca: mutating`, holds command admission only through the short runtime adoption or deactivation consequence, and never waits for independent Managed PAC Reconciliation; status remains available, stop waits for admitted work, and no queue is maintained.
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
The one Installed User CA identified by the durable atomic active-fingerprint marker and used to produce UserCA Signing Material. During live rotation, CONNECT requests admitted before atomic replacement may finish using the previous authority while its Retired root remains trusted.
_Avoid_: unmarked authority, newest-certificate inference, arbitrary installed authority, multiple active signers

**Candidate UserCA**:
A fully prepared and OS-trusted immutable authority generation that may coexist with the Active UserCA but does not become the source of UserCA Signing Material until its fingerprint is atomically persisted as active.
_Avoid_: partially installed CA, active signer, untrusted staging certificate, required Candidate marker

**UserCA Signing Material**:
The immutable Active UserCA certificate and matching private signer returned by a usable UserCA Assessment for HTTPS interception. CORS Proxy publishes one CONNECT action bound to this material, while goproxy owns per-host leaf generation and its connection-local failures.
_Avoid_: HTTPS Certificate Provider, HTTPS Provider Source, list-bounded signer, selector certificate set, Gateway leaf generator

**MITM Proxy Generation**:
An immutable goproxy handler bound to one UserCA Signing Material generation and one fresh concurrent LRU certificate cache bounded to 1,024 hostnames. Gateway atomically replaces the handler behind its stable Proxy Listener; admitted connections may retain the previous generation, while PAC changes only when HTTPS routes change.
_Avoid_: mutable in-place CA swap, cross-CA certificate cache, proxy-listener rotation, CA-rotation PAC rewrite

**Retired UserCA**:
The previous Active UserCA after a new active fingerprint is committed. CONNECT requests already admitted to its MITM generation may finish because its root remains trusted; after atomic replacement, old private material is removed as soon as practical and fallible OS trust removal remains Non-Active UserCA Cleanup.
_Avoid_: active signer, permanent secondary root, connection drain, retained private key

**UserCA Rotation**:
A CA maintenance transition that creates and trusts an immutable Candidate generation, atomically persists its fingerprint as active, and lets a live Gateway atomically replace the CONNECT action with one bound to the new UserCA Signing Material. Admitted and established connections are not drained, and a closing runtime may skip adoption because the durable marker is sufficient for the next start.
_Avoid_: list-bound rotation commit, TLS handshake barrier, connection registry, stop-required renewal, synchronous retired-root cleanup, rotation journal

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

**HTTPS Deadline Signal**:
A signal emitted when Gateway's deadline timer reaches the expiry reported by the adopted UserCA Assessment. Gateway reconstructs current truth through a fresh assessment and deactivates HTTPS Readiness only when that assessment is expired or otherwise unusable, making a stale signal harmless; a signal arriving during admitted CA lifecycle mutation is deferred until that mutation and its live adoption consequence settle.
_Avoid_: cached expiry truth, certificate-generation expiry callback, signal-carried UserCA state, silent renewal

**HTTPS Intent**:
An Upstream List state containing at least one valid HTTPS Origin Selector. Host Selectors and HTTP Origin Selectors do not express this intent.
_Avoid_: Config File HTTPS toggle, inferred Host Selector HTTPS intent, invalid-line HTTPS intent

**Unmet HTTPS Intent**:
An HTTPS state where HTTPS Intent exists while HTTPS Readiness is not ready. Trusted HTTPS Interception remains inactive, the gateway continues serving HTTP, and the user receives an actionable warning.
_Avoid_: blocked gateway, failed gateway start, implicit UserCA installation

**HTTPS Warning**:
A typed, surface-neutral current diagnostic owned by the Gateway Module and exposed independently from HTTPS Intent and HTTPS Readiness so multiple conditions may coexist. Stable kinds cover unmet intent, unusable UserCA state, and renewal due; Managed PAC failures use Managed PAC Warnings instead, front ends render both current sets, and cleared warnings are not retained as history.
_Avoid_: terminal warning text, warning history, single CA warning string, readiness encoded as prose, mutually exclusive diagnostics, silent degraded HTTPS

**Live HTTPS Warning Delivery**:
A foreground lifecycle callback that publishes a surface-neutral HTTPS Warning snapshot when the current set changes. The CLI renders added or materially changed warnings live, while HTTP clients read the same current set through status without requiring a streaming endpoint.
_Avoid_: Gateway-owned terminal output, warning polling in the foreground CLI, warning event history, required HTTP event stream

**HTTPS Readiness**:
The single runtime HTTPS state, expressed as `ready` or `not-ready` from a UserCA Assessment. Ready means Gateway has atomically adopted the assessment's UserCA Signing Material and enables Trusted HTTPS Interception and HTTPS PAC routes; Gateway alone schedules the assessment expiry deadline and freshly reassesses UserCA before changing readiness.
_Avoid_: separate interception state, proxy health, continuous trust-store polling, installed-file check, expiry warning as not-ready

**HTTPS Readiness Loss**:
A runtime transition from ready to not-ready HTTPS Readiness when a fresh UserCA Assessment after an HTTPS Deadline Signal finds the authority expired or otherwise unusable, when that assessment fails and current usability therefore cannot be established, or after confirmed Live UserCA Uninstall. The transition atomically installs direct-tunnel CONNECT behavior, withdraws HTTPS routes, and reports assessment failure as readiness unavailable while leaving HTTP service and CA material or OS trust untouched; recovery requires explicit install.
_Avoid_: proxy operational failure, failed gateway, continuous trust-store revalidation, status mutation

**HTTPS Readiness Recovery**:
A runtime transition from not-ready to ready HTTPS Readiness immediately after successful UserCA installation or repair.
_Avoid_: restart-required HTTPS activation, delayed readiness after successful install, Config File toggle

**Trusted HTTPS Interception**:
A runtime behavior present only while HTTPS Readiness is ready. CORS Proxy then asks goproxy to intercept every CONNECT reaching the loopback proxy and generate its leaf certificate from the adopted UserCA Signing Material; connection-local signing or handshake failure does not change Gateway state. HTTPS Origin Selectors and Host Selectors produce managed HTTPS routes only through PAC Routing.
_Avoid_: Upstream List proxy admission, list-bounded certificate signing, separate interception state, Config File HTTPS toggle

**Installed-CA HTTPS Enablement**:
A lifecycle rule where ready HTTPS Readiness enables Trusted HTTPS Interception without a separate configuration toggle. HTTPS Intent makes not-ready interception warning-worthy, but does not install, repair, or substitute for UserCA capability.
_Avoid_: Explicit Trusted HTTPS, Config File HTTPS toggle, intent-as-capability, silent trust installation

**Upstream List Projection**:
The decoded and normalized semantic interpretation of observed Upstream List contents, containing Host Selectors, Origin Selectors, and Upstream List Warnings. The Upstream List module owns projection formation and identity without owning continuous observation, application path policy, the Upstream List File Sync Issue, projection-rejection consequences, or PAC Route Set identity.
_Avoid_: Upstream List Source, raw contents, file snapshot, PAC Route Set, Gateway-decoded list

**Rejected Upstream List Contents**:
Successfully read Upstream List contents that the Upstream List module reports cannot form a semantic projection, distinct from line-level warnings and observation failure. Rejection leaves file observation current; Gateway records an Upstream List Projection Issue and independently applies its fail-closed policy by selecting an Empty Upstream List as the effective projection.
_Avoid_: Upstream List Sync Failure, upstreamlist-owned routing consequence, last-known-good routing, line warning, observation degradation

**Upstream List Fail-Closed Projection**:
The Gateway Runtime policy that selects the canonical Empty Upstream List when read contents are rejected, compares that selected projection with the current effective projection, and independently preserves the Upstream List Projection Issue for presentation. Repeated rejection may change the Issue without implying another projection or PAC change.
_Avoid_: parser-returned empty success, last-known-good routing, error-implies-PAC-update, Gateway-constructed projection

**Upstream List Projection Identity**:
The Upstream List module-owned equivalence of normalized selectors and warnings, independent from raw-content identity and PAC Projection identity. Gateway invokes this identity to decide whether to adopt a newly formed projection without defining why two projections are semantically equal.
_Avoid_: source-content identity, Gateway-defined projection equality, PAC equality, representation equality

**Upstream List File Sync Issue**:
An optional Gateway Runtime-owned current problem whose kind is File Unreadable or Observation Stopped and which contains its presented cause. Its kind and cause define issue identity; File Unreadable can recover, Observation Stopped requires Gateway restart, file observation privately rebuilds an uncertain watcher and rereads the complete file, and the Issue's appearance, change, and clearing must remain available for Inbound Adapter presentation without prescribing a synchronization interface.
_Avoid_: Upstream List Sync State, Upstream List Projection, content validity, parser state, PAC availability, watcher uncertainty, raw watcher error

**Upstream List Projection Issue**:
An optional Gateway Runtime-owned current problem containing the presented cause of Rejected Upstream List Contents. Its cause defines issue identity; successful projection clears it independently from Upstream List Projection Identity, rejection selects the Empty Upstream List without implying another PAC Projection change when effective routes are already empty, and the Issue's appearance, change, and clearing must remain available for Inbound Adapter presentation without prescribing a synchronization interface.
_Avoid_: Upstream List Projection Error State, combined Upstream List State, raw error identity, failure event history, file sync issue, projection identity, error-implies-PAC-update

**Gateway Control Command**:
A user-facing command that controls gateway-owned state or reports on it, including start, serve, stop, status, UserCA install, and UserCA uninstall.
_Avoid_: lifecycle operation, command service, control endpoint operation

**Start Sequence**:
The public Gateway Module start flow that verifies ownership, performs early ownership-aware Gateway Footprint Cleanup, establishes continuous Upstream List observation and its initial Gateway-owned state even when unavailable, assesses HTTPS Readiness without mutating trust, and then attempts Gateway Activation. Direct start removes stale owner state before claiming ownership, while router-hosted start preserves the live owner cache; cleanup failure is returned as a structured start outcome identifying each failed cleanup subject.
_Avoid_: start-time CA installation, public raw activation, PAC-first start, cleanup-after-approval

**Gateway Activation**:
The internal operation that assesses Managed PAC Consent, begins serving Gateway Runtime with its assessed HTTPS Readiness and current projections, installs managed PAC state, and then produces Start Guidance. It is invoked only through the Start Sequence so callers cannot bypass cleanup, Upstream List observation establishment, readiness assessment, or traffic-before-PAC ordering.
_Avoid_: public activation command, CA installation, CA Trust Consent, lifecycle activation, runtime startup, command rendering, lifecycle orchestration package

**Automatic Listeners**:
A lifecycle behavior where the gateway chooses available loopback ports for its proxy, PAC, and router endpoints at startup, then wires dependent gateway state in sequence.
_Avoid_: user-selected listener ports, fixed listener ports, manual listener addresses

**Loopback Default**:
A listener behavior where gateway endpoints bind to loopback.
_Avoid_: LAN-exposed proxy, user-selected bind address

**Proxy Listener**:
A host-local general proxy endpoint that accepts traffic independently of PAC Routing. The Upstream List controls Generated PAC selection and Trusted HTTPS Interception scope rather than proxy admission; HTTP traffic remains eligible for CORS repair, covered HTTPS is intercepted for repair, and uncovered HTTPS is direct-tunneled.
_Avoid_: Upstream-gated proxy, PAC-only proxy, LAN-exposed proxy, gatewayListen

**CORS Proxy**:
The gateway module behind the Proxy Listener that owns CORS repair, Local Preflight Answer, Response Repair, and Trusted HTTPS Interception behavior for traffic that reaches it.
_Avoid_: Upstream List admission module, PAC Routing module, generic proxy

**Home Config Directory**:
The fixed seamless-cors location at `.seamless-cors` under the user's home directory. Gateway owns the Upstream List path policy, creation assessment and execution, and observation lifecycle; Gateway Coordination and UserCA independently own their state in dedicated subdirectories.
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

**Complete Managed PAC Uninstall**:
An idempotent Managed PAC operation that closes reconciliation admission, ends pending work, removes every currently marker-owned PAC setting regardless of enabled status or publication generation, and reports success only after no marker-owned setting remains. Foreign PAC state is always preserved; late desired states are discarded until a later successful Managed PAC Installation reopens admission.
_Avoid_: enabled-only cleanup, exact-URL cleanup, service-set cleanup, previous-state restoration, partial uninstall success

**Managed PAC Service Set**:
The network services classified as manageable during Gateway Activation and collectively accepted by the user for PAC Routing installation and later Managed PAC Reconciliation. Initially foreign services remain outside the set, while membership becomes fixed after acceptance: selected services remain members through later absence or drift, and excluded or newly appearing services wait until another start.
_Avoid_: all visible services, initially foreign service, currently controlled service subset, live service discovery for expansion, implicit service expansion, removal-on-disappearance, removal-on-drift

**Managed PAC Installation**:
A Gateway Activation mutation that attempts the desired PAC URL on every currently manageable visible member of the accepted Managed PAC Service Set. The accepted set remains fixed even when a member becomes absent or foreign before mutation; per-service exceptions produce Managed PAC Warnings, and a failed initial publication remains an internal Managed PAC condition that is retried while Gateway Runtime continues serving.
_Avoid_: all-or-nothing PAC installation, failure-narrowed service set, silent partial installation, Gateway termination on transient PAC publication failure

**Managed PAC Runtime State**:
A Gateway Runtime's latched record of the fixed Managed PAC Service Set and the latest Managed PAC publication URL after Managed PAC installation. Its absence means the runtime has no active Managed PAC configuration; it retains no consent state and is not a live observation of operating-system proxy settings.
_Avoid_: Managed PAC Session, Managed PAC lease state, live PAC snapshot, attempted PAC URL

**Managed PAC Active**:
A status fact derived from the presence of Managed PAC Runtime State, meaning the Gateway Runtime retains a fixed reconciliation scope and desired PAC URL. It does not claim that every selected service is currently controlled; Managed PAC Warnings report per-service exceptions, including when every service has drifted.
_Avoid_: all-services-controlled, live OS PAC verification, warning-free Managed PAC, Managed PAC lease held

**Managed PAC Mutation Sequence**:
Managed PAC's private ordering rule where installation, PAC Projection publication, reconciliation, and uninstall execute one at a time independently from Upstream List observation and UserCA serialization. A newer changed PAC Projection replaces older pending publication input without interrupting an active attempt; failed attempts retain the last successfully published PAC and retry the newest projection, and uninstall waits for the current writer before removing and verifying all marker-owned PAC state.
_Avoid_: caller-owned PAC lock, operation-success wait, concurrent PAC writes, refresh-cleanup race, post-stop PAC install, uninstall racing an old writer, global lifecycle mutex

**Managed PAC Reconciliation**:
A PAC update behavior that independently evaluates each visible member of the fixed Managed PAC Service Set: marker-owned and empty settings receive the current PAC URL, foreign settings are preserved with a warning, and temporarily absent services wait for a later update. Reconciliation does not inspect or expand to services outside the fixed set.
_Avoid_: Managed PAC lease check, all-or-nothing refresh, idle watcher, new-service adoption, foreign PAC replacement, missing-service failure

**Managed PAC Reconciliation Request**:
The complete changed PAC Projection published by Gateway to Managed PAC after Gateway invokes PAC Routing-owned projection identity. Managed PAC owns publication generation, serial platform attempts, and retry without receiving or reinterpreting the Upstream List or HTTPS Readiness.
_Avoid_: Upstream List desired state, duplicate PAC derivation, PAC URL command, delta, event history, post-uninstall PAC write, PAC watcher, Gateway-owned PAC generation, UserCA-owned PAC refresh

**Managed PAC Publication Generation**:
The Managed PAC-owned monotonic generation allocated before each new PAC Projection publication attempt. A failed attempt consumes its generation, so gaps are valid; retries allocate a new generation and use the newest pending projection.
_Avoid_: Gateway PAC version, routing revision, rollback generation, transaction sequence, reclaimed failed version

**Managed PAC Drift**:
A nonfatal condition where a visible member of the fixed Managed PAC Service Set carries foreign PAC state during Managed PAC Reconciliation. The foreign setting is preserved, the Gateway Runtime continues, and a Managed PAC Warning reports that seamless-cors no longer controls that service.
_Avoid_: Managed PAC Lease Lost, consent-stale warning, fatal PAC drift, forced PAC restoration, foreign PAC takeover, silent proxy escape

**Managed PAC Update Failure**:
A nonfatal condition where Managed PAC Reconciliation is authorized to update an owned or empty selected service but its platform write fails. Managed PAC retains the last successfully published PAC, keeps the newest desired state, consumes the failed publication generation, and retries internally.
_Avoid_: fatal PAC refresh, PAC URL rollback, whole-runtime failure, silent partial update

**Managed PAC Warning**:
A typed, surface-neutral current diagnostic that identifies each visible service affected by Managed PAC Drift or Managed PAC Update Failure independently from HTTPS Warnings. Gateway Runtime replaces the warning snapshot after each Managed PAC Reconciliation, drops prior warnings for services now absent, and exposes the current snapshot through Start Guidance and status.
_Avoid_: superseded reconciliation warning, HTTPS Warning, terminal PAC error, warning history, untyped PAC warning, silent per-service drift

**CA Ownership Marker**:
The strict seamless-cors-owned current-user CA trust identity used to identify Installed User CA trust for CA lifecycle management.
_Avoid_: CA footprint, name-contains matching, system-wide CA cleanup, user-authored CA identity

**Cleanup Retry Guidance**:
A user-facing cleanup behavior where failed cleanup explains that seamless-cors-owned state remains and tells the user to run `seamless-cors stop` again after resolving the OS or permission problem.
_Avoid_: silent cleanup failure, false cleanup success, manual OS instructions first

**Single User Instance**:
A gateway ownership rule where only one Gateway Owner may run for a user at a time, with the Gateway State Cache used as the first signal that an owner may already be active.
_Avoid_: multi-instance gateway, competing PAC state, port-based instance detection

**Upstream List Creation**:
A Gateway-owned Start operation that assesses the fixed Upstream List path and, after Upstream List Creation Consent, immediately and exclusively attempts creation of the missing file and required parent directories with the Upstream List module's exact default contents. Failure returns its actionable cause without preventing Start; Gateway subsequently establishes observation independently, and creation is neither deferred until Gateway Activation nor rolled back when a later Start decision prevents activation.
_Avoid_: Configuration Bootstrap, silent file creation, init command, manual file scaffolding, read-time mutation, configurable Upstream List path, replacing invalid paths

**Upstream List Creation Warning**:
A surface-neutral, non-persistent Start warning containing the actionable cause of a failed authorized Upstream List Creation attempt. It appears only on the Start result produced by that attempt, is absent after successful creation, and remains independent from any Upstream List File Sync Issue observed afterward.
_Avoid_: runtime state, successful-creation notice, Upstream List File Sync Issue, merged creation and observation error, warning replay

**Upstream List Creation Consent**:
A fingerprint-bound user decision required when Gateway assesses the fixed path as missing, presented at most once per Start Sequence and authorizing immediate exclusive creation at the disclosed path with the Upstream List module's disclosed default contents and any disclosed missing parent directories, independently from Managed PAC Consent. Declining preserves the missing path but allows that Start Sequence to continue degraded without asking again; a later Start reassesses, while runtime disappearance never requests consent or recreates the file or its parent.
_Avoid_: combined Start consent, CLI-invented consent, consent error, overwrite authorization, runtime bootstrap, implicit default creation

**Start Guidance**:
A start-time user-facing output behavior shown only after PAC assessment and any required decision permit activation and Gateway Runtime is serving. Current Upstream List File Sync Issue, Upstream List Projection Issue, and Managed PAC publication warnings are included when applicable, while transient initial publication failure remains internal and is retried; guidance points to user-relevant state instead of runtime listener endpoints.
_Avoid_: pre-consent running message, listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

**Start Guidance Detail**:
A surface-neutral successful start result detail containing the user-relevant Upstream List and lifecycle state needed to render Start Guidance without exposing runtime listener endpoints.
_Avoid_: terminal start text, listener status detail, proxy setup instructions

**Already-Running Start**:
An idempotent fulfilled start result where executing start against an active Gateway Runtime reports that the requested running postcondition is already satisfied without requiring another mutation.
_Avoid_: changed-means-fulfilled, duplicate runtime activation, start failure for active runtime, second owner

**Execute-Time Start Assessment**:
A start execution rule that presents independently required Upstream List Creation Consent and Managed PAC Consent at most once each in a fixed creation-then-PAC order. Accepted creation mutates immediately without changing PAC consent semantics; accepted PAC consent fixes the agreed manageable service set, whose members that become absent or foreign remain selected but are skipped with Managed PAC Warnings while excluded and newly appearing services do not join.
_Avoid_: consent dependency, combined consent, fulfilled assessment, successful start assessment, start plan, repeated consent loop, consent-time service expansion

**Single-Flight Start**:
A start behavior where a Gateway Owner accepts only one complete Start Sequence at a time, acquiring exclusivity before cleanup and holding it through Upstream List creation assessment and Source establishment, HTTPS Readiness assessment, PAC assessment, Gateway Activation, and the returned outcome. Concurrent attempts return already-running or start-already-mutating without duplicating lifecycle work.
_Avoid_: cross-command lifecycle lock, CA-command blocking, activation-only lock, queued start, duplicate mutation, competing activation, start plan reservation

**Stop-Preempted Start**:
A lifecycle precedence rule where `stop` cancels and supersedes an in-progress Start Sequence, waits for safe boundaries, then performs final Gateway Footprint Cleanup and ends ownership. Cancelled activation cannot later publish runtime or install PAC state.
_Avoid_: stop-busy result, start mutex wait, cleanup-before-cancellation, post-stop PAC install

**Stop-Cancelled Start**:
A surface-neutral expected start outcome returned to the original start caller after stop preemption reaches a safe boundary without treating cancellation as an infrastructure failure.
_Avoid_: context-canceled error, started result, stop failure

**Managed PAC Consent Detail**:
A surface-neutral description of every visible network service, identifying which services are manageable and therefore proposed for the fixed Managed PAC Service Set, together with the Managed PAC Consent Fingerprint and no-restoration cleanup behavior. Foreign services are shown as excluded rather than offered for replacement.
_Avoid_: service-selection UI, foreign PAC authorization, lifecycle consent detail, prompt text, OS trust approval payload, start plan token

**Managed PAC Consent Fingerprint**:
A stable identity derived only from the sorted names in the proposed manageable service set shown in Managed PAC Consent Detail. An accepted retry echoes those names and the fingerprint so Gateway can validate and fix the collectively agreed set without retaining pending consent state.
_Avoid_: PAC URL authorization, full PAC state hash, enabled-state authorization, source ordering, generic consent flag, start plan token

**Managed PAC Consent**:
A collective user confirmation required before each new Gateway Runtime activation manages a nonempty proposed set of manageable network services. Accepting fixes that proposed set for the runtime, declining aborts activation, and foreign services remain excluded rather than being replaced; an empty proposed set produces No Manageable PAC Services without prompting.
_Avoid_: PAC Replacement Consent, per-service selection, foreign PAC takeover, implicit system PAC mutation, reusable consent

**No Manageable PAC Services**:
A terminal start outcome where every visible network service is foreign or no manageable service is visible, so Gateway presents the inspected service detail without requesting Managed PAC Consent or starting Gateway Runtime. A direct start process exits because no managed routing can be provided, while a router-hosted attempt leaves its explicitly requested router-only Gateway Owner alive.
_Avoid_: empty Managed PAC consent, zero-service runtime, foreign service takeover, successful inactive start

**Independent PAC Lifecycle**:
A lifecycle boundary where Managed PAC Consent and PAC Routing setup follow gateway start independently of whether the Upstream List currently has active entries.
_Avoid_: domain-gated PAC setup, delayed proxy ownership, route-count-based lifecycle

**CA Trust Consent**:
A platform approval moment required before adding or replacing Installed User CA trust for HTTPS interception, with gateway context shown only when the platform requires approval.
_Avoid_: implicit CA trust, repeated consent for unchanged trust, app-only trust prompt, Managed PAC Consent Detail

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Installed User CA mutation occur only through explicit CA Lifecycle Commands rather than gateway start or the Upstream List. Gateway Runtime may be updated as a consequence, while runtime stop does not cancel admitted CA work and owner exit waits for that work to settle.
_Avoid_: start-time CA trust, stop-cancelled CA command, intent-triggered installation, route-dependent trust setup

**Start Sequence Order**:
A startup lifecycle order where Gateway Footprint Cleanup, the fixed Upstream List Creation Consent stage, immediate best-effort consented creation, and Upstream List observation establishment precede the independent Managed PAC Consent stage. HTTPS Readiness is assessed without mutating trust before Gateway Runtime serves; Gateway Runtime begins serving before Managed PAC installation, whose unhealthy initial publication remains Managed PAC-owned retry work.
_Avoid_: start-time CA installation, PAC-before-runtime serving, PAC-first start, cleanup-after-approval, start guidance before PAC installation

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and viewing gateway status while Gateway Runtime continuously observes and projects the Upstream List.
_Avoid_: command-heavy configuration, flag-driven operation

**CA Lifecycle Commands**:
Top-level user-facing commands that explicitly install, repair, or remove the Installed User CA outside the normal start/stop gateway loop. Install performs HTTPS Readiness Recovery when needed, while uninstall remains available during gateway operation and requires confirmation only when Trusted HTTPS Interception is active.
_Avoid_: nested CA command tree, hidden CA removal, per-start CA trust, config editing command, separate readiness command

**Upstream-Independent CA Install**:
A CA lifecycle command boundary where installing or repairing the Installed User CA does not read, require, create, or modify the Upstream List. When a Gateway is live, Gateway atomically adopts the returned UserCA Signing Material without consulting the current projection.
_Avoid_: install-time configuration bootstrap, intent-dependent install, separate readiness endpoint, restart-required recovery

**UserCA Install Reconciliation**:
An install order that first attempts Non-Active UserCA Cleanup, then reuses valid Active UserCA Signing Material, repairs its missing OS trust when required, or installs/rotates authority state that is invalid, expired, mismatched, or renewal-due. Failed cleanup blocks only work that would add another trusted root: a valid Active authority can still be reused, while required rotation stops before Candidate creation; discovering missing active trust makes HTTPS Readiness not-ready until repair succeeds.
_Avoid_: proxy failure-triggered CA rotation, trust repair before non-active reconciliation, arbitrary non-active adoption, unbounded trusted roots

**Idempotent CA Install**:
A CA lifecycle command behavior where installing reuses valid Active UserCA trust without requesting platform approval or changing CA material, while Gateway still atomically adopts a fresh CONNECT action when Gateway Runtime is active.
_Avoid_: reinstalling valid CA, proxy failure-triggered rotation, noisy no-op install, repeated trust approval

**Active HTTPS Uninstall Consent**:
A confirmation required before UserCA uninstall disables active Trusted HTTPS Interception and removes the entire Installed UserCA Set. Consent authorizes that identity-independent consequence rather than one Active fingerprint; declining leaves HTTPS Readiness and all UserCA state unchanged, and no confirmation is required when interception is already inactive.
_Avoid_: certificate-bound consent, active-runtime uninstall block, unconditional uninstall prompt, partial UserCA removal, implicit consent

**Live UserCA Uninstall**:
A confirmed UserCA uninstall behavior where Gateway first atomically installs direct-tunnel CONNECT behavior, cancels its deadline timer, and withdraws HTTPS PAC routes before UserCA removes owned CA material and OS trust. Successful removal adopts the returned not-usable snapshot; failed or incomplete removal leaves HTTPS not-ready without restoring the previous signing material, and recovery requires explicit install or an uninstall retry.
_Avoid_: trust removal before proxy deactivation, automatic signing-material restoration, partial-failure HTTPS recovery, uninstall-owned PAC coordination

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
A lifecycle behavior that asks Managed PAC to uninstall stale or intentionally released marker-owned PAC state and independently removes the appropriate Gateway State Cache while leaving Installed User CA state untouched. Direct start holds the Gateway Ownership Lease while cleaning stale cache and PAC state, router-hosted start preserves its live owner cache, and stop removes both when ending ownership.
_Avoid_: unconditional cache removal, live-owner eviction, runtime cleanup, status cleanup, serve-start cleanup, broad cleanup, CA removal, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Human HTTPS Status**:
A compact Human Status rendering of `https: active` when HTTPS Readiness is ready and `https: inactive` otherwise, followed by actionable current HTTPS Warnings. Internal readiness vocabulary and redundant active fields are not printed as separate normal-status fields.
_Avoid_: `https-interception-health`, `trusted-https-active`, internal state dump, warning-free inactive status

**Read-Only Status**:
A status behavior that reports gateway, cleanup-needed, Installed User CA, Human HTTPS Status, and stale Gateway State Cache detection without latching HTTPS Readiness Loss or changing proxy settings, CA trust, local CA material, runtime files, or discovery state. An existing owner reports its latched UserCA Snapshot, an ownerless command uses Ownership-Protected Status Assessment, and admitted CA work is reported as `userca: mutating`.
_Avoid_: status-triggered cleanup, mutating status command

**Gateway Status State**:
A read-only gateway status vocabulary that describes whether the Gateway Owner and Gateway Runtime are absent, stale, router-only, ending, starting, or running without encoding Command Fulfillment, cleanup, HTTPS Readiness, or UserCA Usability. A Status Result keeps its Operation-Specific Result Kind separate from this state: `reported` is fulfilled for every reported state, while an ownership-transition result is unfulfilled and has no reported state.
_Avoid_: status-as-command-failure, cleanup status, UserCA state, start result, runtime state file truth

**UserCA Usability**:
A two-state assessment where UserCA is `usable` only when one valid Active UserCA has matching local material and current-user OS trust, and is otherwise `not-usable`. Renewal due is an independent fact; private cleanup state does not cross the UserCA seam, assessment failure is an error, and `mutating` belongs to Gateway command coordination rather than UserCA state.
_Avoid_: public missing/expired/mismatched state taxonomy, unknown UserCA state, public cleanup state, mutation-as-UserCA-state

**UserCA Snapshot**:
An immutable status-only result freshly inspected by UserCA from authority material, the Active fingerprint marker, and current-user OS trust. It exposes UserCA Usability at inspection, expiry, and renewal due without carrying certificates, private keys, signers, or another operational capability; UserCA never caches or observes it, while Gateway Runtime may latch an admitted Snapshot.
_Avoid_: signing-material container, exported Active authority type, raw PEM, CA storage paths, cached CA state, live CA watcher, mutable authority record, storage snapshot, public trust-store facts

**UserCA Assessment**:
One coherent UserCA result containing a status-only UserCA Snapshot and, only when usable, the matching immutable UserCA Signing Material. Inspection and successful installation return this pair from the same authority facts so Gateway never reconstructs or matches signing material itself; UserCA validates authority structure, validity, constraints, self-signature, and key correspondence but does not generate a leaf as a self-test.
_Avoid_: independently loaded snapshot and signer, list-bound install success, optional invalid signing material, leaf-generation self-test

**Diagnostic Runtime Endpoint**:
An automatically selected listener address shown by status for troubleshooting, not for user proxy setup or configuration.
_Avoid_: setup address, configured listener, manual proxy instruction

**Upstream List**:
The user-managed newline-delimited configuration at `~/.seamless-cors/upstreams.txt`, decoded by the Upstream List module into Host Selectors, Origin Selectors, and Upstream List Warnings for PAC Routing. It never controls direct proxy admission or certificate scope. Except for consented Upstream List Creation, seamless-cors only observes this ordinary-file source and never repairs, rewrites, or recreates it.
_Avoid_: Domain List, Target List, configurable Upstream List path, symlinked list, automatic file repair, runtime recreation, network-filesystem observation guarantee, proxy admission list, proxy rules

**Upstream List Comment**:
A full-line or inline note in the Upstream List that is ignored during matching.
_Avoid_: comment-as-entry

**Empty Upstream List**:
A valid Upstream List state with no active entries, including a file that contains only comments, blank lines, or invalid lines carrying Upstream List Warnings; the gateway keeps managed PAC Routing installed and matches no upstreams until valid Upstream List Entries are added.
_Avoid_: startup failure for no active entries, proxy-all fallback

**Upstream List Warning**:
A persistent line-level diagnostic for an invalid Upstream List line that is ignored while other valid Upstream List Entries remain active. Warning appearance, change, and clearing are Upstream List Projection changes eligible for runtime status, while warning-only changes preserve PAC Projection identity and do not request Managed PAC publication.
_Avoid_: silent invalid entry, fatal line error, transient log warning, command replay, routing revision warning

**Upstream List Observation Failure**:
A concrete file-observation failure that Gateway records as an Upstream List File Sync Issue while retaining the current effective Upstream List Projection. A read failure remains recoverable, while failure to rebuild observation is terminal and requires cause repair plus Gateway restart; watcher uncertainty is private recovery work rather than a Gateway-visible condition.
_Avoid_: projection rejection, sync-error-as-empty, fatal Gateway Runtime error, Gateway-visible watcher uncertainty, silent observation failure

**Upstream List Entry**:
A normalized routing value decoded by the Upstream List module as either a Host Selector or an Origin Selector. Internal consumers that construct entries directly are responsible for satisfying the same normalized value contract.
_Avoid_: source-text-bearing entry, rule, matcher expression

**Host Selector**:
An Upstream List Entry variant containing a lowercase ASCII hostname without a scheme or port, selecting that exact hostname across HTTP and HTTPS on any port unless its source uses `*.` to select a Single-Label Wildcard. Host Selector wildcard syntax is interpreted only for this variant, and IP literal spelling is not canonicalized.
_Avoid_: Domain Selector, Hostname Selector, Hostname Shorthand, scheme-less origin, port-qualified domain

**PAC Route**:
A scheme-qualified effective match containing an exact or Single-Label Wildcard hostname and either any port or one normalized numeric port. Host Selectors derive any-port routes for active HTTP(S) schemes, while Origin Selectors derive exact-port routes after default-port interpretation.
_Avoid_: Host Route, Origin Route, Domain Route, PAC-owned selector

**Origin Selector**:
An Upstream List Entry variant containing an HTTP(S) scheme, lowercase ASCII hostname without wildcard syntax, and optional normalized explicit port from 1 through 65535, matched exactly. Port presence is part of selector identity, so an omitted port and the scheme's explicit default port remain distinct Origin Selectors; IP literal spelling is not canonicalized, so a valid Origin Selector is not guaranteed to identify a browser-reachable origin.
_Avoid_: Full Origin, URL selector, scheme-qualified domain, wildcard-bearing origin

**Upstream List Routing Policy**:
A runtime interpretation owned by the PAC Routing module that decides whether normalized Upstream List Entries send a browser request to the Proxy Listener without revalidating them. Gateway Runtime supplies entries from the current effective Upstream List Projection rather than a source representation or diagnostic state.
_Avoid_: Gateway diagnostic-state dependency, proxy admission policy, raw string matching, duplicated PAC matchers, downstream Upstream List validation

**Line-Level Upstream Validation**:
An Upstream List behavior where each line is validated independently so valid Upstream List Entries are applied while invalid lines are ignored and reported with their line, active text, and a stable generic syntax diagnostic as Upstream List Warnings. Host Selectors and Origin Selectors use the same conservative DNS/IP hostname validation; only Host Selectors support Single-Label Wildcard matching.
_Avoid_: Line-Level Domain Validation, parser-reason diagnostic taxonomy, silent invalid entry, whole-list rejection, invalid line as active entry

**Upstream List Deduplication**:
An Upstream List module behavior where equivalent normalized source-level entries are treated as one active entry, keeping the first occurrence and ignoring later duplicates. Port presence is part of Origin Selector identity; PAC Routing separately deduplicates equivalent derived PAC Routes.
_Avoid_: duplicate source selectors, line-count domains, PAC-owned source deduplication

**Exact Host Match**:
A Host Selector behavior that selects only the named hostname.
_Avoid_: Exact Domain Match, implicit subdomain match, broad domain match

**Single-Label Wildcard**:
A Host Selector behavior written as `*.example.com` that selects exactly one leading subdomain label and does not select the parent domain or deeper subdomains.
_Avoid_: parent-domain wildcard, wildcard-bearing hostname

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

Developer: "Can another host-local client configure the Proxy Listener directly?"

QA engineer: "Yes. The Proxy Listener is independent of PAC Routing: it proxies arbitrary HTTP traffic, intercepts HTTPS covered by the effective Upstream List, and direct-tunnels uncovered HTTPS."

Developer: "When will HTTPS domains route through the gateway?"

QA engineer: "Trust-Aware PAC Routing sends them through the gateway only while HTTPS Readiness is ready."

Developer: "Do I need to maintain the PAC file?"

QA engineer: "No, PAC Routing projects the current effective Upstream List, HTTPS Readiness, and runtime endpoint into the Generated PAC."

Developer: "How do Upstream List changes reach the operating system proxy?"

QA engineer: "The PAC Endpoint serves the current Generated PAC, and Managed PAC advances its publication generation when clients need a new URL to observe an effective update."

Developer: "Can I avoid changing my system proxy settings?"

QA engineer: "No, the gateway uses Managed System Proxy so application requests keep their original URLs."

Developer: "What if I decline the Managed PAC Consent prompt?"

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

QA engineer: "No, Gateway continuously observes the file, adopts a semantically changed Upstream List Projection, and publishes only a changed PAC Projection to Managed PAC."

Developer: "What happens if I save an invalid config file while the gateway is running?"

QA engineer: "Rejected Upstream List Contents produce an Upstream List Projection Issue while Gateway selects an Empty Upstream List, updates PAC only if its projection changes, and continues observing for a valid correction."

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

QA engineer: "Managed PAC Consent shows every visible service, excludes foreign PAC settings, and asks before managing the remaining proposed service set for this run."

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

QA engineer: "No. Start presents Upstream List Creation Consent once; acceptance immediately attempts exclusive Upstream List Creation, while decline or creation failure continues degraded with no matched upstreams."

Developer: "Can I write just `api.dev.example.com`?"

QA engineer: "Yes, that Upstream List Entry is a Host Selector; use an Origin Selector when a scheme or port should constrain matching."

Developer: "Can I annotate upstreams in the Upstream List?"

QA engineer: "Yes, Upstream List Comment supports full-line and inline comments."

Developer: "Does a Host Selector include custom ports?"

QA engineer: "No, a Host Selector matches any port; use an Origin Selector to constrain the scheme and port."

Developer: "What if one Upstream List line is wrong?"

QA engineer: "Line-Level Upstream Validation ignores that line, reports an Upstream List Warning, and continues routing with the valid Upstream List Entries."

Developer: "What if I save an invalid Upstream List while the gateway is running?"

QA engineer: "Invalid lines produce Upstream List Warnings while valid entries are projected; whole-document rejection selects an empty effective projection, while missing or unreadable source observation preserves the current effective projection."

Developer: "Does `api.dev.example.com` include its subdomains?"

QA engineer: "No, Exact Host Match requires an explicit wildcard when subdomains should be included."

Developer: "Does `*.example.com` match `deep.api.example.com`?"

QA engineer: "No, Single-Label Wildcard matches only one subdomain label; add `*.api.example.com` when that deeper level should also match."

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
