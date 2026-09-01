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
A rule that only the Gateway Module combines module-owned facts into HTTP CORS Demand, HTTPS CORS Demand, and active traffic outcomes, then orders the required projections and mutations; feature modules never initiate another feature's lifecycle. Selector, UserCA, and routing facts remain owned by their source modules while Gateway owns their cross-feature consequences.
_Avoid_: feature-owned lifecycle orchestration, per-request Upstream List gate, duplicated selector translation, ordering-means-waiting

**Gateway Traffic Demand**:
A current Gateway-derived boolean instructing it whether to produce one family of traffic routes from separately retained module-owned facts. It is a decision rather than user intent or selector scope and may change when its input facts change.
_Avoid_: user intent, configuration flag, route set, selector projection, active feature, module-owned fact

**HTTP CORS Demand**:
The boolean Gateway Traffic Demand produced by HTTP Origin Selectors and Host Selectors for browser HTTP requests to reach Proxy under the fixed CORS policy. Selector facts remain separate and supply routing scope.
_Avoid_: selector-specific demand, HTTPS Facade demand, active HTTP CORS

**HTTPS CORS Demand**:
The boolean Gateway Traffic Demand produced unconditionally by HTTPS Origin Selectors and produced by Host Selectors only while UserCA is usable, for browser HTTPS requests to reach Proxy under the fixed CORS policy. Selector facts remain separate and supply routing scope.
_Avoid_: HTTPS Intent, HTTPS Facade demand, active HTTPS CORS, unconditional Host Selector HTTPS demand

**Traffic Routing Ready**:
A current Gateway fact that the PAC Endpoint and Proxy are serving and the latest Managed PAC Control State reports Managed PAC Routes Current Endpoint. Managed PAC privately establishes that report from at least one Network Service with an enabled, seamless-cors-owned PAC URL identifying this Gateway Runtime's PAC endpoint; any delivery-generation query for that current host, port, and owned path remains valid, while a marker-owned URL for a previous runtime does not. No end-to-end browser probe is required, and Gateway does not reinterpret PAC URLs or ownership to derive this fact. Managed PAC Set and Read-Only Status obtain fresh Control State without polling.
_Avoid_: runtime active, proxy listener ready, Traffic Projection Current, warning-free PAC delivery, manual proxy availability

**Traffic Projection**:
The complete Gateway-composed traffic state containing one PAC Projection and its matching Proxy and HTTPS Facade configuration. Gateway derives its latest desired Traffic Projection from current demands and module facts, then switches the PAC Endpoint and traffic behavior together to make that projection served.
_Avoid_: PAC-only projection, Network Service delivery state, independently published Proxy configuration

**Served Traffic Projection**:
The Traffic Projection currently exposed by Gateway's PAC Endpoint and matching Proxy and HTTPS Facade configuration. Switching it retires the previous served configuration; obsolete browser-cached PAC contents are outside its coherence invariant. Network Service PAC delivery succeeds or fails independently and never changes which projection Gateway serves.
_Avoid_: latest desired projection, browser PAC cache state, per-service PAC setting, delivery rollout

**Traffic Projection Current**:
A synchronization and diagnostic fact that the Served Traffic Projection is semantically equivalent to Gateway's latest desired Traffic Projection. Equivalence includes PAC routes, HTTP and HTTPS CORS behavior, HTTPS Facade mappings, interception behavior, and UserCA identity, while selector order, source text, warnings, byte identity, PAC URL generation, and Network Service delivery state do not determine it.
_Avoid_: Traffic Routing Ready, PAC delivery status, publication generation, byte identity, retry active

**Traffic Projection Switch**:
The Gateway Runtime transition that first composes and validates a complete Traffic Projection, then atomically replaces the PAC Endpoint contents and matching Proxy and HTTPS Facade configuration before requesting per-service PAC delivery. Failure preserves the previous Served Traffic Projection, leaves Traffic Projection Current false, and starts no delivery for the rejected projection.
_Avoid_: PAC-first publication, independently visible proxy update, Network Service transaction, partial served projection

**HTTP CORS Active**:
The aggregate active traffic outcome present when the Served Traffic Projection contains at least one HTTP CORS route and Traffic Routing Ready holds. It describes served behavior rather than current HTTP CORS Demand or Traffic Projection Current; per-service PAC delivery failures do not deactivate it while a working managed route remains.
_Avoid_: HTTP CORS Demand, selector-specific HTTP outcome, proxy ability

**HTTP CORS Blocked**:
The aggregate traffic outcome present when HTTP CORS Demand holds but HTTP CORS Active does not. It reports demanded behavior that is not currently served through working managed routing.
_Avoid_: inactive HTTP CORS, PAC delivery warning, selector-specific blockage

**HTTP CORS Inactive**:
The aggregate traffic outcome present when neither HTTP CORS Active nor HTTP CORS Blocked holds.
_Avoid_: blocked HTTP CORS, absent route alone

**HTTPS CORS Active**:
The aggregate active traffic outcome present when Traffic Routing Ready holds, the Served Traffic Projection contains at least one HTTPS CORS route, and the current usable UserCA identity matches that projection's interception identity. It describes usable served behavior rather than current HTTPS CORS Demand or Traffic Projection Current; per-service PAC delivery failures do not deactivate it while a working managed route remains.
_Avoid_: HTTPS CORS Demand, HTTPS Facade, selector-specific HTTPS outcome, proxy ability

**HTTPS CORS Blocked**:
The aggregate traffic outcome present when HTTPS CORS Demand holds but HTTPS CORS Active does not. Its concrete cause may be not-usable UserCA, a UserCA Assessment Issue, mismatch between the current usable UserCA identity and the Served Traffic Projection, absent working managed routing, or demanded behavior not yet served.
_Avoid_: inactive HTTPS CORS, generic HTTPS failure, PAC delivery warning

**HTTPS CORS Inactive**:
The aggregate traffic outcome present when neither HTTPS CORS Active nor HTTPS CORS Blocked holds.
_Avoid_: blocked HTTPS CORS, absent route alone

**Independent Feature Serialization**:
A concurrency rule where Gateway serializes UserCA lifecycle work and conditionally coordinates one required HTTPS Pipeline, while other feature modules serialize their private mutations. Switching the Served Traffic Projection must nevertheless keep its PAC Endpoint contents and matching Proxy and HTTPS Facade behavior coherent; per-service PAC delivery remains independent from that runtime switch.
_Avoid_: mismatched published PAC and proxy behavior, global lifecycle lock, list-coupled UserCA adoption

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
The module that holds the Gateway Ownership Lock and publishes Gateway Router discovery state for a long-running ownerless `serve` or `start` command or transient ownerless CA work. Once published, start, CA Lifecycle Commands, status, and stop address that owner, while competing serve fails.
_Avoid_: daemon supervisor, client command, detached runtime owner, terminal command renderer

**Gateway Host**:
The process-bootstrap role that establishes and keeps a Gateway Owner available independently of whether Gateway Runtime is activated. An ownerless start combines Gateway Hosting with the Start operation, Router-Only Serve hosts without starting, and an HTTP control surface can only address an already-hosted owner.
_Avoid_: CLI-owned Start semantics, implicit serve command, HTTP process bootstrap, Gateway Runtime

**Gateway Runtime**:
The live traffic-serving engine that owns the proxy listener and server, Gateway-owned outbound proxy transport, Served Traffic Projection, PAC listener and server, current UserCA Current State, Gateway Traffic Demands and active outcomes, independent continuous observations and projections for each Upstream List, their current Effective Upstream List, source-specific File Sync and Projection Issues, runtime close behavior, and fatal serving-error reporting without installing or unsetting OS PAC state. It begins only after initial observation and UserCA assessment have established their facts; feature degradation never ends it, while explicit Gateway stop or an irrecoverable proxy or PAC serving failure ends it coherently.
_Avoid_: initializing runtime, retained observation result, retained raw contents, lifecycle facade, command router, OS proxy manager, cleanup owner

**Router-Only Serve**:
A command behavior where the command becomes the Gateway Owner and starts the Gateway Router as an HTTP client entry point without automatically starting Gateway Runtime, running Gateway Footprint Cleanup at serve startup, or changing managed OS state; it fails clearly when a Gateway Owner already exists and may claim stale Gateway State Cache only after verification finds no reachable owner.
_Avoid_: implicit gateway start, daemonized start, hidden lifecycle activation, stale-cache cleanup, OS PAC repair

**Router-Hosted Start**:
A start behavior where CLI or another client calls `POST /start` against an existing Gateway Owner with the invoking client's absolute working directory, renders any required Start decision, and retries with that decision to activate Gateway Runtime without creating a competing gateway process. The existing owner remains foreground, and an already-active runtime returns an idempotent start result.
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
A runtime proxy auto-configuration artifact rendered from the PAC Projection inside the Served Traffic Projection, not edited directly by the user.
_Avoid_: user-authored PAC, manual PAC rules

**PAC Projection**:
The PAC Routing component of a Traffic Projection, derived from Gateway's selector facts, demands, UserCA facts, and runtime routing endpoint. PAC Routing owns its formation, while Gateway composes it with matching Proxy and HTTPS Facade configuration and Managed PAC does not reinterpret its inputs.
_Avoid_: complete Traffic Projection, Managed PAC desired Upstream List, duplicated PAC derivation, user-authored PAC

**PAC Route Set**:
The PAC Routes within a PAC Projection, derived inside the PAC Routing module from normalized Upstream List Entries and Managed HTTPS Routing. HTTP Origin Selectors always contribute their HTTP routes and contribute HTTPS Facade routes while Managed HTTPS Routing is active unless a more explicit HTTPS Origin Selector takes precedence; HTTPS Origin Selectors contribute routes only while managed HTTPS routing is active, and Host Selectors contribute HTTP routes always plus HTTPS routes only while managed HTTPS routing is active.
_Avoid_: hand-built JavaScript rules, duplicated Upstream List parsing, PAC-owned Upstream List syntax, publication identity

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
A terminal stop behavior where Gateway Footprint Cleanup attempts every cleanup subject, including successfully observed seamless-cors-owned active PAC state and live coordination cache, even after another subject fails. Observed cleanup residue makes the result unfulfilled, while a Managed PAC Observation Issue is disclosed without preventing fulfillment; neither condition preserves Gateway Ownership, and a later ownerless command verifies and cleans observable stale footprint where its semantics permit.
_Avoid_: first-error cleanup, successful result with observed residue, silent PAC observation issue, retrying owner, router-only fallback

**Owner Stop**:
A stop behavior used by explicit stop, graceful process termination, and unexpected Gateway Router termination. It rejects new work, closes Gateway Runtime before Gateway Footprint Cleanup, waits for admitted owner-owned CA mutation, truthfully reports cleanup residue, and then tears down Router and ownership even when cleanup is incomplete.
_Avoid_: runtime-only stop, router-only survival, retrying owner, cleanup-before-runtime-close, cleanup-failure-means-success

**Owner Ending**:
A terminal Gateway Owner lifecycle state that begins when Owner Stop takes precedence and lasts until the process exits. An admitted CA Lifecycle Command may settle, but new start, install, and uninstall work are rejected and cleanup failure does not reopen command admission.
_Avoid_: owner stopping, retry window, late start admission, start-after-stop

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
A maintenance operation performed by explicit install that replaces an Installed User CA before expiry. An authority within 90 days of expiry remains usable while exposing renewal due as an Installed User CA fact for operation results and status; an active ready HTTPS Pipeline may continue Trusted HTTPS Interception until expiry, and runtime expiry detection directs the user to install rather than mutating CA material or OS trust automatically.
_Avoid_: traffic-triggered trust mutation, automatic root replacement, silent CA replacement, treating near-expiry as expired

**CA Replacement Rule**:
A CA lifecycle rule where valid UserCA material is reused, its missing trust or local permissions are repaired in place, and renewal-due, expired, invalid, or ambiguous state is completely removed and verified before one replacement authority is installed. Gateway withdraws active HTTPS before replacement and restores it only from the returned usable UserCA Current State.
_Avoid_: overlapping roots, proxy-failure-triggered replacement, trusting invalid material, start-time repair, UserCA-owned HTTPS coordination

**UserCA Installation**:
The explicit UserCA operation that ensures the current user's seamless-cors authority is usable by reusing, repairing, or renewing it and requests platform approval only when trust must be added or replaced. Its fulfilled outcome reports that postcondition without exposing which reconciliation path occurred; a live Gateway adopts the resulting UserCA Current State and recomputes its demands and active outcomes.
_Avoid_: start-time CA installation, activation-owned CA setup, asynchronous live-install reconciliation, list-bound install result, mutation-reporting install outcome, repeated trust prompt, implicit trust repair

**Owner-Owned CA Mutation**:
An admitted install or uninstall belongs to the Gateway Owner and settles independently of request cancellation or client disconnection. Owner Stop waits for it, while process interruption leaves an assessable single-pair footprint that the next install or uninstall reconciles.
_Avoid_: request-owned mutation, disconnect cancellation, stop-cancelled CA command, caller-managed commit boundary

**Gateway-Owned CA Lifecycle**:
A lifecycle rule where install, Installed User CA Renewal, and uninstall route through an existing Gateway Owner or a discoverable Transient Gateway Owner published before ownerless work. Gateway Ownership provides cross-process routing, discovery, mutation serialization, and active-HTTPS-Pipeline coordination without blocking other features.
_Avoid_: ownerless CA mutation, undiscoverable ownership holder, separate CA Mutation Lease, direct UserCA command execution, caller-managed CA locking

**Transient Gateway Owner**:
A discoverable Gateway Owner published before ownerless CA lifecycle work. It exposes the Gateway Router and Gateway State Cache while coordinating one finite CA mutation; status reports `userca: mutating`, stop enters Owner Ending and waits, competing CA work and start fail fast, and the owner cannot be promoted into a long-running owner.
_Avoid_: promotable CA owner, install-owned Gateway Runtime, private one-shot lease holder, hidden CA process, background daemon, undiscoverable owner

**Fail-Fast CA Mutation Admission**:
A Gateway serialization rule where install and uninstall are rejected for explicit retry when another CA mutation is already admitted. Gateway maps that condition to `userca: mutating`, holds command admission through the short runtime withdrawal and adoption consequence, and never waits for an independent Managed PAC Set; status remains available, stop waits for admitted work, and no queue is maintained.
_Avoid_: owner-exists-means-busy, queued CA mutation, concurrent CA mutation, blocked status

**Ownership-Protected Status Assessment**:
An ownerless Read-Only Status behavior that briefly holds the Gateway Ownership Lock without publishing Gateway Router discovery state, assesses Gateway and UserCA facts coherently, then releases the lock. If ownership acquisition loses a race, status rediscovers the new owner rather than combining facts across ownership generations.
_Avoid_: Transient Gateway Owner for status, status-written discovery cache, unlocked multi-location CA assessment, status mutation

**Settled-CA Start Admission**:
An owner-coordinated startup boundary where UserCA assessment is serialized with CA Lifecycle Commands so every Gateway Runtime begins with coherent UserCA Usability and never loads authority facts from an in-progress mutation.
_Avoid_: conditional runtime UserCA assessment, runtime boot from mutating CA state, marker polling, UserCA-owned runtime coordination

**Installed UserCA Pair**:
The one seamless-cors-owned certificate and matching private key represented in current-user OS trust and local authority storage. A usable state has exactly one matching trusted identity; replacement does not preserve an overlapping old authority. UserCA exposes only current facts and retains no version, generation, or authority history.
_Avoid_: authority history, active marker, permanent multiple UserCAs, overlapping trusted identities

**UserCA Signing Material**:
The immutable Installed UserCA Pair certificate and matching private signer that always accompanies a usable UserCA Current State. Gateway Runtime retains it as part of that coherent state independently of selectors and supplies it to Proxy while HTTPS work is active; goproxy owns per-host leaf generation and its connection-local failures.
_Avoid_: HTTPS Certificate Provider, HTTPS Provider Source, list-bounded signer, selector certificate set, Gateway leaf generator

**MITM Proxy Generation**:
A goproxy handler bound to one UserCA Signing Material instance whose TLS identity and interception behavior remain fixed for its lifetime while it consults current HTTPS Facade forwarding at each decrypted request boundary. Gateway atomically replaces the handler behind its stable Proxy Listener for UserCA Current State and HTTPS Pipeline Required transitions; admitted connections may retain the previous proxy generation, while PAC changes only when HTTPS routes change.
_Avoid_: mutable in-place CA swap, proxy-listener rotation, CA-rotation PAC rewrite

**CA Material Integrity**:
A CA lifecycle invariant where current-user CA trust and the Installed UserCA Pair match; missing or mismatched material is treated as replacement-needed state.
_Avoid_: trusted cert without signing key, orphaned signing key, mismatched CA pair

**OS-Backed CA Installation**:
A CA lifecycle invariant where Installed User CA state requires current-user operating-system trust; local CA material alone is not installed trust.
_Avoid_: file-only installation, assuming trust from local material

**CA Permission Repair**:
A CA lifecycle behavior where otherwise-valid Installed User CA material with loose local file permissions is tightened in place without replacing trusted CA identity.
_Avoid_: permission-triggered CA rotation, loose CA key permissions

**HTTPS Deadline Signal**:
A signal emitted when Gateway Runtime's retained usable UserCA Current State reaches its reported expiry. Gateway performs one fresh assessment, accepts it only for the current state generation, and does not schedule retry after failure; the deadline is state invalidation rather than a background retry loop.
_Avoid_: cached expiry truth, certificate-generation expiry callback, signal-carried UserCA state, silent renewal

**HTTPS Pipeline Required**:
A Gateway-derived boolean that requires the internal HTTPS Pipeline while HTTPS CORS Demand exists or while an HTTP Origin Selector and usable UserCA Current State together activate HTTPS Facade. It is a mechanism requirement rather than user intent or a surface-visible feature state.
_Avoid_: HTTPS Intent, HTTPS Facade Demand, UserCA Usability, public pipeline state

**HTTPS Pipeline**:
The internal Gateway Runtime mechanism present only while HTTPS Pipeline Required holds, selecting a direct or UserCA-bound MITM Proxy configuration and deriving managed HTTPS PAC routes from retained module facts. Requirement transitions are exposed only by coherently switching the Served Traffic Projection, without exposing pipeline state through command results or status.
_Avoid_: user-facing feature, public pipeline state, UserCA lifecycle command, always-on interception

**Blocked HTTPS CORS Demand**:
The active blocking condition where HTTPS CORS Demand exists but UserCA Usability is either `not-usable` or unavailable because of a UserCA Assessment Issue. Established not-usable facts expose explicit install guidance; an Assessment Issue instead exposes its concrete cause without claiming install will repair it.
_Avoid_: Unmet HTTPS Intent, blocked gateway, failed gateway start, implicit UserCA installation, Host Selector HTTPS warning

**UserCA Assessment Issue**:
A current Gateway Runtime issue created when UserCA inspection fails and Gateway therefore cannot establish UserCA Usability. Gateway continues serving HTTP, exposes the concrete cause without install guidance, and reassesses after install, Gateway restart, or an adopted Upstream List update without running a timer-based retry loop.
_Avoid_: generic HTTPS warning, UserCA not-usable state, terminal error text, warning history, pipeline issue

**Managed HTTPS Routing**:
The PAC Routing consequence produced while HTTPS Pipeline Required holds and UserCA is usable. HTTPS CORS Demand contributes native HTTPS routes and HTTP Origin Selectors contribute HTTPS Facade routes; otherwise PAC Routing excludes HTTPS routes.
_Avoid_: pipeline requirement alone, unusable-UserCA routing, per-host proxy admission

**Trusted HTTPS Interception**:
A runtime behavior present only while HTTPS Pipeline Required holds and UserCA is usable. Proxy then asks goproxy to intercept every CONNECT reaching the loopback proxy and generate its leaf certificate from the retained UserCA Signing Material; connection-local signing or handshake failure does not change Gateway state.
_Avoid_: Upstream List proxy admission, list-bounded certificate signing, separate interception state, Config File HTTPS toggle

**Gateway-Gated HTTPS Interception**:
A lifecycle rule where HTTPS Pipeline Required and UserCA Usability together activate Trusted HTTPS Interception and Managed HTTPS Routing without a separate configuration toggle. Gateway derives both inputs from retained facts without installing, repairing, or substituting UserCA implicitly.
_Avoid_: HTTPS Intent, Config File HTTPS toggle, capability-as-demand, silent trust installation

**Upstream List Projection**:
The decoded and normalized interpretation of observed Upstream List contents, containing Host Selectors, Origin Selectors, and Upstream List Warnings. The Upstream List module owns projection formation without owning continuous observation, application path policy, file issues, rejection consequences, Traffic Projection composition, or PAC delivery policy.
_Avoid_: Upstream List Source, raw contents, file snapshot, PAC Route Set, semantic identity

**Rejected Upstream List Contents**:
Successfully read contents of one Upstream List that the Upstream List module reports cannot form a semantic projection, distinct from line-level warnings and observation failure. Rejection leaves that source's file observation current; Gateway records a source-specific Upstream List Projection Issue and independently applies its fail-closed policy by selecting an Empty Upstream List as that source's projection before forming the Effective Upstream List.
_Avoid_: Upstream List Sync Failure, upstreamlist-owned routing consequence, last-known-good routing, line warning, observation degradation

**Upstream List Fail-Closed Projection**:
The Gateway Runtime policy that selects the canonical Empty Upstream List for a source whenever that source's read contents are rejected, while independently preserving its Upstream List Projection Issue for presentation. The healthy source continues contributing to the Effective Upstream List, and every rejection follows the normal merge and adoption path and may therefore publish another PAC Projection.
_Avoid_: parser-returned empty success, last-known-good routing, semantic no-op suppression, Gateway-constructed projection

**Upstream List File Sync Issue**:
A source-specific optional Gateway Runtime-owned current problem whose kind is File Unreadable or Observation Stopped and which contains its presented cause. Its source, kind, and cause define issue identity; File Unreadable can recover, Observation Stopped requires Gateway restart, file observation privately rebuilds an uncertain watcher and rereads the complete file, and the Issue's appearance, change, and clearing must remain available for Inbound Adapter presentation without prescribing a synchronization interface.
_Avoid_: Upstream List Sync State, Upstream List Projection, content validity, parser state, PAC availability, watcher uncertainty, raw watcher error

**Upstream List Projection Issue**:
A source-specific optional Gateway Runtime-owned current problem containing the presented cause of Rejected Upstream List Contents. A successful source projection clears it, rejection selects the Empty Upstream List for that source, and its appearance, change, and clearing remain available for Inbound Adapter presentation independently from the resulting Effective Upstream List and Traffic Projection.
_Avoid_: Upstream List Projection Error State, combined Upstream List State, raw error identity, failure event history, file sync issue

**Gateway Control Command**:
A user-facing command that controls gateway-owned state or reports on it, including start, serve, stop, status, UserCA install, and UserCA uninstall.
_Avoid_: lifecycle operation, command service, control endpoint operation

**Start Sequence**:
The public Gateway Module start flow that verifies ownership, performs early ownership-aware Gateway Footprint Cleanup, establishes independent continuous observation and initial Gateway-owned state for both Upstream Lists even when a source is unavailable, forms the Effective Upstream List, establishes coherent UserCA Usability, derives Gateway Traffic Demands, and then attempts Gateway Activation. Direct start holds Gateway Ownership while removing stale state and publishing its discovery cache, while router-hosted start preserves the live owner cache; cleanup failure is returned as a structured start outcome identifying each failed cleanup subject.
_Avoid_: start-time CA installation, public raw activation, PAC-first start, cleanup-after-approval

**Gateway Activation**:
The internal operation that begins one Managed PAC Control Lifetime, discovers its automatically manageable Network Services, begins serving Gateway Runtime with its retained facts, demands, active outcomes, and current projections, performs Managed PAC Set, and then produces Start Guidance. It is invoked only through the Start Sequence so callers cannot bypass cleanup, fact establishment, Managed PAC Configuration Protection, or traffic-before-PAC ordering.
_Avoid_: public activation command, CA installation, CA Trust Consent, lifecycle activation, runtime startup, command rendering, lifecycle orchestration package

**Automatic Listeners**:
A lifecycle behavior where the gateway chooses available loopback ports for its proxy, PAC, and router endpoints at startup, then wires dependent gateway state in sequence.
_Avoid_: user-selected listener ports, fixed listener ports, manual listener addresses

**Loopback Default**:
A listener behavior where gateway endpoints bind to loopback.
_Avoid_: LAN-exposed proxy, user-selected bind address

**Proxy Listener**:
A host-local general proxy endpoint that accepts traffic independently of PAC Routing. The Upstream List controls Generated PAC selection rather than proxy admission or per-request interception scope; Proxy handles every request, intercepts every CONNECT only while the HTTPS Pipeline is active and ready, and direct-tunnels every CONNECT otherwise.
_Avoid_: Upstream-gated proxy, PAC-only proxy, per-host interception gate, LAN-exposed proxy, gatewayListen

**Proxy**:
The traffic-mechanics module that constructs goproxy-backed handlers for every request reaching the Proxy Listener, always adapting the product's fixed CORS Module policy and additionally adapting HTTPS Facade Projections and optional UserCA Signing Material into ordered HTTP hooks, direct or intercepted CONNECT behavior, and per-host certificate caching. Gateway Runtime owns handler-generation lifecycle and publication ordering; Proxy owns transport integration without owning CORS policy, HTTPS Facade projection, UserCA validity, Upstream List admission, or PAC Routing.
_Avoid_: CORS Proxy, active-generation owner, lifecycle manager, Upstream List admission module, PAC Routing module, generic pass-through proxy

**CORS Module**:
The traffic-policy module that owns the product's single fixed Local Preflight Answer and Response Repair semantics independently of proxy transport mechanics. Proxy invokes this policy directly for every request rather than accepting a configurable CORS seam, while Gateway and HTTPS Facade do not redefine it.
_Avoid_: CORS Proxy, goproxy hooks, per-upstream policy, Gateway-owned CORS semantics

**CORS Repair Scope**:
A traffic rule where every CORS-bearing request reaching the Proxy Listener is eligible for repair: the calling page's Origin determines reflected response values but never admission, while Upstream List destination selection happens earlier through PAC Routing.
_Avoid_: Allowed Origin, caller-origin allowlist, per-request Upstream List gate, CORS authorization

**Gateway Runtime Directory**:
The platform-native per-user XDG runtime location used by Gateway Coordination for the Gateway Ownership Lock and Gateway State Cache. It is selected once from the Gateway Coordination Environment, may be volatile, and is not persistent application data.
_Avoid_: Gateway Coordination Home, XDG State Home, configuration directory, persistent state directory, legacy `.seamless-cors/runtime`

**Gateway Coordination**:
A lifecycle behavior that owns the Gateway Ownership Lock, Gateway State Cache operations, Gateway State Verification, and Single User Instance decisions while allowing lifecycle cleanup paths to remove cache state through Gateway Footprint Cleanup.
_Avoid_: Runtime Coordination, cleanup module, process supervisor, daemon manager, file-exists-is-running

**Gateway Coordination Environment**:
The Gateway process-startup environment that selects one XDG runtime location for the Gateway Ownership Lock and Gateway State Cache. Commands using a different runtime environment occupy a different coordination namespace and do not discover or contend with that owner.
_Avoid_: cross-environment owner search, dynamic runtime relocation, legacy coordination fallback

**Installed CA Storage**:
The durable platform-native per-user application-state location for seamless-cors-owned Installed User CA material, selected by the UserCA Storage Environment and kept outside Gateway Footprint Cleanup.
_Avoid_: Gateway Runtime Directory, portable user data, runtime CA storage, temp CA files, stop-owned CA files

**UserCA Storage Environment**:
The Gateway Owner's process-startup environment that selects one authoritative Installed CA Storage location for owner-routed UserCA commands; an ownerless command uses the environment of the owner it establishes. Different storage environments are distinct UserCA identities and are not searched as alternative locations or compared by clients.
_Avoid_: global per-user storage search, dynamic storage relocation, fallback CA directory

**Gateway State Cache**:
A runtime coordination record that atomically publishes the active Gateway Router's ephemeral loopback listener and authentication token. Clients verify it through the authenticated health route; malformed or unreachable records are stale.
_Avoid_: Runtime State File, control state, pid-only lock file, configured control address, in-memory instance registry, source of truth

**Gateway Ownership Lock**:
The nonblocking OS-backed exclusive file lock held for the complete Gateway Owner lifetime. It is the authority for Single User Instance decisions; lock-file existence alone carries no ownership meaning.
_Avoid_: Gateway State Lease, cache ownership watcher, verify-then-claim, waiting owner queue

**Gateway State Verification**:
A read-only Gateway Coordination behavior where an existing Gateway State Cache is checked through the HTTP Router before the gateway treats another Gateway Owner as active.
_Avoid_: Runtime State Verification, file-exists-is-running, port-only lock, stale state as active instance, cleanup validation

**Ownership Marker**:
A stable property proving a machine resource belongs to seamless-cors and may be modified or removed by gateway lifecycle cleanup.
_Avoid_: heuristic ownership, name-only matching, user intent guess

**Marker-Based Cleanup**:
A cleanup behavior where the gateway scans current machine state and removes resources only when an Ownership Marker proves the resource belongs to seamless-cors.
_Avoid_: footprint-based cleanup, previous-state restoration, guessed ownership

**Network Service**:
A current-user operating-system service that can independently carry a PAC setting. Its visibility is established separately from whether its PAC setting can be observed or managed.
_Avoid_: viable service, PAC setting, successfully observed service

**PAC Setting Observation**:
The current PAC URL and enabled state observed as properties of one visible Network Service. A failed observation leaves the Network Service visible but does not establish manageable PAC state.
_Avoid_: service discovery, missing service, assumed-empty PAC state

**Managed PAC Observation Issue**:
A current per-service condition where the latest Network Service discovery included a service but its PAC Setting Observation failed. The failure's cause is not classified, no manageable PAC state is established for that service, and the issue is exposed through Start Guidance, runtime status, or fulfilled cleanup detail until a later discovery or observation replaces it or lifecycle teardown ends current state.
_Avoid_: inferred service presence, empty PAC setting, cause-classified observation error, Managed PAC Update Failure, silent service omission

**Managed PAC Active-State Cleanup**:
An idempotent Managed PAC operation that disables and verifies every successfully observed, currently enabled marker-owned PAC setting. Gateway calls ownerless cleanup only when it has no live Managed PAC control handle, while a control lifetime performs the same cleanup through Close; Managed PAC does not duplicate Gateway's lifecycle admission state. A visible Network Service whose PAC setting cannot be observed is outside the cleanup scope and does not prevent success; disabled owned URLs are inert retained configuration, platforms may represent inactive state differently, and observed foreign PAC state is always preserved.
_Avoid_: concurrent control cleanup, duplicated lifecycle admission, PAC URL erasure, disabled owned URL cleanup, exact-URL cleanup, previous-state restoration, unobservable-PAC cleanup failure

**Gateway Footprint Cleanup Status**:
A subject-level three-state result describing whether owned gateway footprint is `none`, `needed`, or `unknown`; `unknown` means the subject itself could not be discovered and must not be treated as clean. Successfully discovered Network Services with failed PAC Setting Observations remain outside PAC management rather than making the PAC subject unknown. The overall state is derived as `needed` when any subject is needed, otherwise `unknown` when any subject is unknown, and otherwise `none`.
_Avoid_: cleanup-needed boolean, assumed-clean inspection failure, suppressed cleanup inspection error

**Managed PAC Ownership Marker**:
The stable loopback HTTP PAC URL shape whose path ends in `seamless-cors.pac`, proving a current managed PAC setting belongs to seamless-cors without depending on a run-specific port.
_Avoid_: managed PAC footprint, run-specific PAC identity, port-based ownership, full-URL ownership, non-loopback PAC ownership

**Managed PAC Activation Assessment**:
A feature-owned activation report containing the visible Network Service presentation facts, the fixed Managed PAC Service Set selected from them, and current Managed PAC Observation Issues. Its manageability decisions are authoritative; Gateway uses the selected set to decide whether activation can proceed and does not reconstruct selection from PAC URL, enabled state, ownership, or observation failures.
_Avoid_: Gateway-authored service selection, generic PAC snapshot, caller-derived manageability, ownerless footprint report

**Managed PAC Control Lifetime**:
A Managed PAC-owned lifetime that fixes one Managed PAC Service Set when it begins, serializes Set and State operations under an internal lifetime context, and ends only when Gateway closes its control handle. The request context used to begin control governs establishment but does not own the established lifetime. Close rejects new work, cancels and quiesces in-flight work, performs independently bounded Managed PAC Active-State Cleanup, and then releases the lifetime.
_Avoid_: request-owned PAC lifetime, Gateway-owned service set, per-operation cancellation, background worker, Managed PAC lease, caller-assembled cleanup

**Managed PAC Service Set**:
The successfully observed empty or marker-owned Network Services automatically fixed when a Managed PAC Control Lifetime begins. Initially foreign or unobservable services remain outside the set; selected services remain members through later absence, drift, or observation issues and can recover on a later Managed PAC Set, while excluded or newly appearing services wait until another control lifetime.
_Avoid_: accepted service set, user-selected services, all visible services, initially foreign service, initially unobservable service, currently controlled service subset, live service discovery for expansion, implicit service expansion, removal-on-disappearance, removal-on-drift, removal-on-observation-issue

**Service-PAC State**:
A private Managed PAC record for one member of the fixed Managed PAC Service Set, containing its Network Service identity, latest observed PAC URL and enabled state, ownership, whether it currently points to the active PAC Endpoint, and its own warnings or observation issue. Managed PAC uses these records to produce purpose-built Control State and never exposes them across the Gateway seam.
_Avoid_: exported PAC snapshot, Gateway-authored PAC state, caller-interpreted ownership, index-correlated warning

**Managed PAC Control State**:
A feature-owned, freshly observed report for one Managed PAC Control Lifetime containing its fixed Managed PAC Service Set, Managed PAC Routes Current Endpoint, and service-identified Managed PAC Warnings and Observation Issues. Managed PAC derives it from private Service-PAC State; Gateway asks for it when producing status, forwards its diagnostics without reclassification, and does not retain a parallel cache. Its absence means the Gateway Runtime has no active Managed PAC control handle.
_Avoid_: Managed PAC Runtime State, raw service snapshot, Gateway-derived endpoint control, Gateway-owned warning cache, attempted PAC URL

**Managed PAC Routes Current Endpoint**:
A current Managed PAC boolean reporting that fresh private Service-PAC State establishes at least one fixed-set Network Service with an enabled, marker-owned PAC URL identifying the control lifetime's current PAC Endpoint. It ignores publication-generation differences and remains only one input to Gateway-owned Traffic Routing Ready.
_Avoid_: Traffic Routing Ready, proxy serving fact, Gateway URL comparison, warning-free delivery, all-services-controlled

**Managed PAC Active**:
A status fact derived from the presence of a Managed PAC control handle, meaning the Gateway Runtime retains a Managed PAC-owned fixed service set. It does not claim that every selected service is currently controlled; Managed PAC Control State reports the fixed set, Managed PAC Routes Current Endpoint, and current exceptions.
_Avoid_: all-services-controlled, live OS PAC verification, warning-free Managed PAC, Managed PAC lease held

**Managed PAC Mutation Sequence**:
Managed PAC's private ordering rule where Set, State, and Close execute one at a time independently from Upstream List observation and Gateway CA mutation serialization. Each Set makes exactly one attempt; no request queue, conflation, or background retry exists. Each service update succeeds or fails independently, successful services are not rolled back, and failures wait for a later Set. Close cancels and waits for any current operation before cleaning successfully observed marker-owned PAC state.
_Avoid_: caller-owned PAC lock, concurrent PAC writes, request conflation, background retry, refresh-cleanup race, post-close PAC Set, close racing an old writer, global lifecycle mutex

**Managed PAC Set**:
One synchronous Managed PAC delivery attempt requested by Gateway with the PAC Endpoint currently serving the Generated PAC. Managed PAC independently evaluates each member of the fixed Managed PAC Service Set: marker-owned and empty observed settings receive a newly generated URL, an update failure leaves that service's previous working setting unchanged, successful services are never rolled back because another service failed, foreign settings are preserved with a warning, and absent or unobservable services wait for a later Set. The attempt neither retries nor inspects or expands to services outside the fixed set.
_Avoid_: first PAC Set, PAC update operation, Managed PAC reconciliation loop, all-or-nothing refresh, idle watcher, new-service adoption, foreign PAC replacement, missing-service failure

**Managed PAC Delivery Request**:
The Gateway Runtime trigger that calls Managed PAC Set with the PAC Endpoint after Gateway switches a Served Traffic Projection. Managed PAC owns URL generation, one serial platform attempt, private Service-PAC State, purpose-built Control State, and per-service warnings without determining Traffic Projection Current or reinterpreting module facts and Gateway Traffic Demands.
_Avoid_: Traffic Projection publication, feature activation, background retry request, all-or-nothing rollout, duplicate PAC derivation, event history

**Managed PAC Publication Generation**:
The Managed PAC-owned monotonic generation allocated before each Managed PAC Set attempts to deliver a new Generated PAC URL to Network Services. A failed or partially successful attempt consumes its generation, so gaps and different service URLs are valid; a later Set allocates a new generation without determining Traffic Projection Current.
_Avoid_: Gateway PAC version, routing revision, rollback generation, transaction sequence, reclaimed failed version

**Managed PAC Drift**:
A nonfatal condition where a visible member of the fixed Managed PAC Service Set carries foreign PAC state during Set or State observation. The foreign setting is preserved, the Gateway Runtime continues, and Managed PAC reports a service-identified warning without exposing the private Service-PAC State that established it.
_Avoid_: Managed PAC Lease Lost, consent-stale warning, fatal PAC drift, forced PAC restoration, foreign PAC takeover, silent proxy escape

**Managed PAC Update Failure**:
A nonfatal service-specific condition where platform mutation of a selected service fails during Managed PAC Set. Managed PAC preserves the failure's diagnostic without classifying its cause, leaves that service's previous PAC setting unchanged, does not roll back successful services, and waits for a later Gateway-requested Set; it does not change Traffic Projection Current or feature activation. PAC Setting Observation failure is instead a Managed PAC Observation Issue.
_Avoid_: fatal PAC refresh, PAC URL rollback, whole-runtime failure, silent partial update

**Managed PAC Warning**:
A typed, surface-neutral, service-identified current diagnostic produced for the Network Service affected by Managed PAC Drift or Managed PAC Update Failure, independently from Managed PAC Observation Issues and source-specific HTTPS Pipeline issues. Managed PAC replaces delivery warnings after each Set and exposes them through the returned or freshly observed Control State; Gateway forwards them without reclassification and does not cache them separately.
_Avoid_: index-correlated warning, HTTPS Pipeline issue, terminal PAC error, warning history, Gateway warning cache, untyped PAC warning, silent per-service drift

**CA Ownership Marker**:
The strict seamless-cors-owned current-user CA trust identity used to identify Installed User CA trust for CA lifecycle management.
_Avoid_: CA footprint, name-contains matching, system-wide CA cleanup, user-authored CA identity

**Cleanup Retry Guidance**:
A user-facing cleanup behavior where failed cleanup explains that seamless-cors-owned state remains and tells the user to run `seamless-cors stop` again after resolving the OS or permission problem.
_Avoid_: silent cleanup failure, false cleanup success, manual OS instructions first

**Single User Instance**:
A gateway ownership rule where only one Gateway Owner may run in a Gateway Coordination Environment at a time, with the Gateway Ownership Lock as authority and the Gateway State Cache as verified discovery data.
_Avoid_: multi-instance gateway, competing PAC state, port-based instance detection

**Upstream List Creation**:
A Gateway-owned Start operation that assesses the Global Upstream List path and, after Upstream List Creation Consent, immediately and exclusively attempts creation of the missing file and required parent directories with the Upstream List module's exact default contents. Failure returns its actionable cause without preventing Start; Gateway subsequently establishes observation independently, and creation is neither available for the Directory Upstream List, deferred until Gateway Activation, nor rolled back when a later Start decision prevents activation.
_Avoid_: Configuration Bootstrap, silent file creation, init command, manual file scaffolding, read-time mutation, configurable Upstream List path, replacing invalid paths

**Upstream List Creation Warning**:
A surface-neutral, non-persistent Start warning containing the actionable cause of a failed authorized Upstream List Creation attempt. It appears only on the Start result produced by that attempt, is absent after successful creation, and remains independent from any Upstream List File Sync Issue observed afterward.
_Avoid_: runtime state, successful-creation notice, Upstream List File Sync Issue, merged creation and observation error, warning replay

**Upstream List Creation Consent**:
A fingerprint-bound user decision required when Gateway assesses the fixed path as missing, presented at most once per Start Sequence and authorizing immediate exclusive creation at the disclosed path with the Upstream List module's default contents and any disclosed missing parent directories, independently from Managed PAC Configuration Protection. The default contents are not rendered as part of the consent prompt. Declining preserves the missing path but allows that Start Sequence to continue degraded without asking again; a later Start reassesses, while runtime disappearance never requests consent or recreates the file or its parent.
_Avoid_: combined Start consent, CLI-invented consent, consent error, overwrite authorization, runtime bootstrap, implicit default creation

**Start Guidance**:
A start-time user-facing output behavior shown only after automatic PAC assessment and Managed PAC Set have been attempted and Gateway Runtime is serving. It reports active traffic outcomes, Blocked HTTPS CORS Demand, Traffic Projection Current, UserCA Assessment Issue, the fixed Managed PAC Service Set and its current exceptions, current Upstream List issues, excluded or unobservable service facts, and per-service Managed PAC delivery warnings when applicable. Guidance never exposes HTTPS Pipeline or presents service selection as proof that every selected service is currently configured.
_Avoid_: PAC consent preview, pre-activation PAC promise, pre-consent running message, listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

**Start Guidance Detail**:
A surface-neutral successful start result detail containing the user-relevant Upstream List and lifecycle state needed to render Start Guidance without exposing runtime listener endpoints.
_Avoid_: terminal start text, listener status detail, proxy setup instructions

**Already-Running Start**:
An idempotent fulfilled start result where executing start against an active Gateway Runtime reports only that the requested running postcondition is already satisfied without mutation. A different invoking working directory does not replace the active Directory Upstream List or add mismatch guidance; runtime-source visibility belongs to status.
_Avoid_: changed-means-fulfilled, duplicate runtime activation, start failure for active runtime, second owner, configuration mismatch warning, status-shaped start result

**Execute-Time Start Assessment**:
A start execution rule that presents Upstream List Creation Consent at most once, applies its accepted mutation immediately, and then automatically fixes the Managed PAC Service Set during Gateway Activation. Members that become absent or foreign before mutation remain selected but are skipped with Managed PAC Warnings, while excluded and newly appearing services do not join.
_Avoid_: PAC consent, combined consent, fulfilled assessment, successful start assessment, start plan, repeated consent loop, consent-time service expansion

**Single-Flight Start**:
A start behavior where a Gateway Owner accepts only one complete Start Sequence at a time, acquiring exclusivity before cleanup and holding it through Upstream List creation assessment and Source establishment, conditional HTTPS Pipeline assessment, PAC assessment, Gateway Activation, and the returned outcome. Concurrent attempts return already-running or start-already-mutating without duplicating lifecycle work.
_Avoid_: cross-command lifecycle lock, CA-command blocking, activation-only lock, queued start, duplicate mutation, competing activation, start plan reservation

**Stop-Preempted Start**:
A lifecycle precedence rule where `stop` cancels and supersedes an in-progress Start Sequence, waits for safe boundaries, then performs final Gateway Footprint Cleanup and ends ownership. Cancelled activation cannot later publish runtime or install PAC state.
_Avoid_: stop-busy result, start mutex wait, cleanup-before-cancellation, post-stop PAC install

**Stop-Cancelled Start**:
A surface-neutral expected start outcome returned to the original start caller after stop preemption reaches a safe boundary without treating cancellation as an infrastructure failure.
_Avoid_: context-canceled error, started result, stop failure

**Managed PAC Start Detail**:
A surface-neutral start result combining the Managed PAC Activation Assessment with the Managed PAC Set outcome: every initially visible Network Service, the fixed Managed PAC Service Set, foreign or unobservable services excluded during assessment, current Managed PAC Observation Issues and delivery warnings, and no-restoration cleanup behavior. It does not claim a live verified set of currently configured services.
_Avoid_: Managed PAC Consent Detail, Managed PAC Consent Fingerprint, service-selection UI, foreign PAC authorization, consent payload, PAC preview, prompt text

**Managed PAC Configuration Protection**:
A Gateway Activation boundary where empty and marker-owned PAC settings may be managed without confirmation, while foreign and unobservable settings remain excluded rather than being replaced. Because activation never displaces foreign configuration, routine Managed PAC setup requires no user consent.
_Avoid_: Managed PAC Consent, PAC Replacement Consent, per-service selection, foreign PAC takeover, confirmation for marker-owned residue

**No Manageable PAC Services**:
A terminal start outcome where every visible Network Service is foreign or unobservable, or no manageable service is visible, so Gateway presents the inspected service detail without starting Gateway Runtime. A direct start process exits because no managed routing can be provided, while a router-hosted attempt leaves its explicitly requested router-only Gateway Owner alive.
_Avoid_: empty Managed PAC service set, PAC consent, zero-service runtime, foreign service takeover, successful inactive start

**Independent PAC Lifecycle**:
A lifecycle boundary where automatic Managed PAC assessment and PAC Routing setup follow gateway start independently of whether the Upstream List currently has active entries.
_Avoid_: domain-gated PAC setup, delayed proxy ownership, route-count-based lifecycle

**CA Trust Consent**:
A platform approval moment required before adding or replacing Installed User CA trust for HTTPS interception, with gateway context shown only when the platform requires approval.
_Avoid_: implicit CA trust, repeated consent for unchanged trust, app-only trust prompt, Managed PAC Start Detail

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Installed User CA mutation occur only through explicit CA Lifecycle Commands rather than gateway start or the Upstream List. Gateway Runtime may be updated as a consequence, while runtime stop does not cancel admitted CA work and owner exit waits for that work to settle.
_Avoid_: start-time CA trust, stop-cancelled CA command, intent-triggered installation, route-dependent trust setup

**Start Sequence Order**:
A startup lifecycle order where Gateway Footprint Cleanup, the fixed Upstream List Creation Consent stage, immediate best-effort consented creation, Upstream List observation establishment, and settled UserCA assessment precede beginning a Managed PAC Control Lifetime. Gateway derives demands and HTTPS Pipeline Required from those facts, switches its initial Served Traffic Projection, then requests Managed PAC Set; failed initial delivery keeps Traffic Routing Ready false only when no working current-runtime PAC setting exists and recovery waits for a later delivery request.
_Avoid_: start-time CA installation, PAC-before-runtime serving, PAC-first start, cleanup-after-approval, start guidance before PAC Set

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and viewing gateway status while Gateway Runtime continuously observes and projects the Upstream List.
_Avoid_: command-heavy configuration, flag-driven operation

**CA Lifecycle Commands**:
Top-level user-facing commands that explicitly install, repair, or remove the Installed User CA outside the normal start/stop gateway loop. A live Gateway adopts their resulting UserCA Current State and recomputes demands, HTTPS Pipeline Required, and active outcomes; uninstall requires confirmation only when Trusted HTTPS Interception is active.
_Avoid_: nested CA command tree, hidden CA removal, per-start CA trust, config editing command, separate readiness command

**Upstream-Independent CA Install**:
A CA lifecycle command boundary where installing or repairing the Installed User CA does not read, require, create, or modify the Upstream List. A live Gateway adopts the returned UserCA Current State and independently recomputes selector consequences.
_Avoid_: install-time configuration bootstrap, intent-dependent install, separate readiness endpoint, restart-required recovery

**UserCA Install Reconciliation**:
An install behavior that reuses valid UserCA Signing Material, repairs its missing OS trust or local permissions, or completely removes and verifies invalid, expired, mismatched, ambiguous, or renewal-due state before installing one replacement pair. Gateway withdraws an active HTTPS Pipeline before reconciliation and restores it only from the successful returned UserCA Current State.
_Avoid_: proxy failure-triggered CA replacement, overlapping trusted roots, partial replacement, UserCA-owned runtime coordination

**Idempotent CA Install**:
A CA lifecycle command behavior where installing reuses valid UserCA trust without requesting platform approval or changing CA material, while Gateway withdraws and refreshes Trusted HTTPS Interception only when it is active.
_Avoid_: reinstalling valid CA, proxy failure-triggered rotation, noisy no-op install, repeated trust approval

**Active HTTPS Uninstall Consent**:
A confirmation required before UserCA uninstall disables active Trusted HTTPS Interception and removes the Installed UserCA Pair. Consent authorizes that identity-independent consequence rather than a certificate fingerprint; declining leaves all UserCA facts and active outcomes unchanged, and no confirmation is required when interception is already inactive.
_Avoid_: certificate-bound consent, active-runtime uninstall block, unconditional uninstall prompt, partial UserCA removal, implicit consent

**Live UserCA Uninstall**:
A confirmed UserCA uninstall behavior where Gateway first adopts and serves the no-HTTPS PAC Projection, then atomically installs direct-tunnel CONNECT behavior and cancels its deadline timer before UserCA removes owned CA material and OS trust. Successful verified removal establishes not-usable UserCA; failed assessment establishes a UserCA Assessment Issue, and neither case restores previous signing material automatically.
_Avoid_: trust removal before proxy deactivation, automatic signing-material restoration, partial-failure HTTPS recovery, uninstall-owned PAC coordination

**Upstream-Independent CA Uninstall**:
A CA lifecycle command boundary where removing the Installed User CA does not modify the Upstream List.
_Avoid_: uninstall editing Gateway Traffic Demand inputs, config-coupled removal

**Idempotent CA Uninstall**:
A CA lifecycle command behavior where uninstalling reports the absent postcondition as one fulfilled outcome whether or not seamless-cors-owned CA trust or local CA material was present. It does not change configuration or require repair.
_Avoid_: already-absent result kind, mutation-reporting uninstall outcome, missing-CA uninstall failure, forced repair before removal, noisy no-op uninstall

**Complete CA Uninstall**:
A CA lifecycle invariant where uninstall removes all seamless-cors-owned current-user CA trust and the complete Installed UserCA Pair, then reports success only after those facts are absent from the selected UserCA Storage Environment. Material in another or legacy storage environment is outside the command's discovery and cleanup scope.
_Avoid_: cross-environment CA search, false uninstall success, trusted CA without selected-environment material

**Foreground Start**:
A runtime behavior where `start` runs attached in the foreground rather than launching a background daemon. The first process cancellation executes Owner Stop, while a second cancellation may force immediate exit from cleanup.
_Avoid_: daemon mode, background start, signal-only cleanup path, indefinitely blocked forced exit

**Client Command**:
A command invocation that asks an existing Gateway Owner to perform user-facing gateway work and then exits without owning process lifetime or Gateway Footprint Cleanup.
_Avoid_: detached owner, fake foreground control, remote Ctrl-C ownership

**Owner-Routed CA Lifecycle Command**:
A CA Lifecycle Command behavior where work is sent to an existing Gateway Owner or publishes a Transient Gateway Owner when none exists. This keeps UserCA mutation available during a long-running gateway while the owner coordinates retained UserCA facts, demands, active outcomes, and Managed PAC consequences.
_Avoid_: bypassing owner command authority, ownerless local mutation, separate CA Mutation Lease, separate readiness endpoint, blanket active-runtime rejection

**Gateway Footprint Cleanup**:
A lifecycle behavior that asks Managed PAC to uninstall stale or intentionally released marker-owned PAC state and independently removes the appropriate Gateway State Cache while leaving Installed User CA state untouched. Direct start holds the Gateway Ownership Lock while cleaning stale cache and PAC state, router-hosted start preserves its live owner cache, and stop removes both when ending ownership.
_Avoid_: unconditional cache removal, live-owner eviction, runtime cleanup, status cleanup, serve-start cleanup, broad cleanup, CA removal, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Human Traffic Status**:
A compact Human Status rendering of HTTP CORS and HTTPS CORS as `active`, `blocked`, or `inactive`, and HTTPS Facade as `active` or `inactive`. Active outcomes describe the Served Traffic Projection through working managed routing; Traffic Projection Current separately reports whether it matches Gateway's latest desired projection, while Managed PAC Warnings report per-service delivery failures. Blocking detail comes from absent working managed routing, established not-usable UserCA guidance, UserCA Assessment Issue, or a demanded feature not yet served, without exposing HTTPS Pipeline.
_Avoid_: Human HTTPS Status, pipeline status, generic HTTPS active, internal state dump

**Read-Only Status**:
A status behavior that asks an active Managed PAC control for fresh Control State and reports gateway, cleanup-needed, Installed User CA, Human Traffic Status, Traffic Projection Current, per-service Managed PAC Warnings, UserCA Assessment Issue, and stale Gateway State Cache detection without changing proxy settings, CA trust, local CA material, runtime files, discovery state, or Gateway-retained Managed PAC state. Gateway combines Managed PAC Routes Current Endpoint with current PAC Endpoint and Proxy serving facts to derive Traffic Routing Ready for that response rather than latching or reconstructing it from raw PAC settings; Managed PAC Set performs the same observation before mutation without a polling loop.
_Avoid_: status-triggered cleanup, mutating status command

**Gateway Status State**:
A read-only gateway status vocabulary that describes whether the Gateway Owner and Gateway Runtime are absent, stale, router-only, ending, starting, or running without encoding Command Fulfillment, cleanup, traffic outcomes, or UserCA Usability. A Status Result keeps its Operation-Specific Result Kind separate from this state: `reported` is fulfilled for every reported state, while an ownership-transition result is unfulfilled and has no reported state.
_Avoid_: status-as-command-failure, cleanup status, UserCA state, start result, runtime state file truth

**UserCA Usability**:
A two-state module-owned fact where UserCA is `usable` only when one valid Installed UserCA Pair has matching current-user OS trust, and is otherwise `not-usable`; Gateway establishes and maintains it throughout every running Gateway Runtime independently of current selectors. An adopted Upstream List update reassesses only a current not-usable fact or UserCA Assessment Issue, while usable state waits for lifecycle work or its expiry deadline.
_Avoid_: public missing/expired/mismatched state taxonomy, unknown UserCA state, public cleanup state, mutation-as-UserCA-state

**UserCA Current State**:
One coherent current UserCA result exposing UserCA Usability, expiry, and renewal due and, exactly when usable, matching opaque UserCA Signing Material. UserCA freshly derives it from the Installed UserCA Pair and current-user OS trust; Gateway Runtime retains the complete result independently of selectors while UserCA never caches one.
_Avoid_: UserCA Snapshot, UserCA Assessment value, independently loaded status and signer, raw PEM, CA storage paths, cached UserCA state, live CA watcher, usable state without signing material

**Diagnostic Runtime Endpoint**:
An automatically selected listener address shown by status for troubleshooting, not for user proxy setup or configuration.
_Avoid_: setup address, configured listener, manual proxy instruction

**Upstream List**:
One user-managed newline-delimited source decoded by the Upstream List module into Host Selectors, Origin Selectors, and Upstream List Warnings for PAC Routing and HTTPS Facade Projection. An Upstream List never controls direct proxy admission, certificate scope, or whether Trusted HTTPS Interception is active; except for consented Global Upstream List Creation, seamless-cors only observes these ordinary-file sources and never repairs, rewrites, or recreates them.
_Avoid_: Domain List, Target List, symlinked list, automatic file repair, runtime recreation, network-filesystem observation guarantee, proxy admission list, proxy rules

**Global Upstream List**:
The user-wide Upstream List at `seamless-cors/upstreams.txt` under the platform-native user configuration home. It always contributes to the Effective Upstream List and is the only source eligible for consented Upstream List Creation; the former `~/.seamless-cors/upstreams.txt` is neither migrated nor used as a fallback.
_Avoid_: user Upstream List, default Upstream List, shared Upstream List

**Directory Upstream List**:
An optional `upstreams.txt` found only in the invoking client's exact absolute working directory captured when Gateway Runtime starts. Its absence is an empty source rather than degradation, it is never created by seamless-cors, and an Already-Running Start from another directory does not replace it.
_Avoid_: Local Upstream List, project Upstream List, ancestor Upstream List, recursively discovered Upstream List, dynamic working-directory list

**Effective Upstream List**:
The deduplicated union of the continuously observed Global Upstream List and Directory Upstream List. Both sources contribute without precedence, and a successful change to either source forms a new Effective Upstream List.
_Avoid_: overriding Upstream List, precedence-merged Upstream List, selected Upstream List, concatenated file

**Upstream List Comment**:
A full-line or inline note in the Upstream List that is ignored during matching.
_Avoid_: comment-as-entry

**Empty Upstream List**:
A valid Upstream List state with no active entries, including a file that contains only comments, blank lines, or invalid lines carrying Upstream List Warnings; the gateway keeps managed PAC Routing installed and matches no upstreams until valid Upstream List Entries are added.
_Avoid_: startup failure for no active entries, proxy-all fallback

**Upstream List Warning**:
A persistent line-level diagnostic for an invalid Upstream List line that is ignored while other valid Upstream List Entries remain active. Warning appearance, change, and clearing produce adopted Upstream List Projections; semantically unchanged Traffic Projections remain current without making PAC delivery status part of feature activation.
_Avoid_: silent invalid entry, fatal line error, transient log warning, semantic no-op, unpublished warning transition

**Upstream List Observation Failure**:
A concrete failure observing one source that Gateway records as a source-specific Upstream List File Sync Issue while retaining that source's current projection. The other source continues to be observed, projected, and merged; a read failure remains recoverable, while failure to rebuild one observation is terminal for that source and requires cause repair plus Gateway restart. Watcher uncertainty remains private recovery work rather than a Gateway-visible condition.
_Avoid_: projection rejection, sync-error-as-empty, fatal Gateway Runtime error, Gateway-visible watcher uncertainty, silent observation failure

**Upstream List Entry**:
A normalized routing value decoded by the Upstream List module as either a Host Selector or an Origin Selector. Internal consumers that construct entries directly are responsible for satisfying the same normalized value contract.
_Avoid_: source-text-bearing entry, rule, matcher expression

**Host Selector**:
An Upstream List Entry variant containing a lowercase ASCII hostname without a scheme or port, producing HTTP CORS Demand on any port and additionally producing HTTPS CORS Demand only while UserCA is usable, unless its source uses `*.` to select a Single-Label Wildcard. Wildcard syntax is interpreted only for this variant, and IP literal spelling is not canonicalized.
_Avoid_: Domain Selector, Hostname Selector, Hostname Shorthand, scheme-less origin, port-qualified domain

**PAC Route**:
A scheme-qualified effective match containing an exact or Single-Label Wildcard hostname and either any port or one normalized numeric port. Host Selectors always derive an any-port HTTP route and derive an any-port HTTPS route only while Managed HTTPS Routing is active; Origin Selectors derive exact-port routes for their normalized scheme and effective port, and HTTP Origin Selectors additionally derive HTTPS Facade Routes while Managed HTTPS Routing is active.
_Avoid_: Host Route, Origin Route, Domain Route, PAC-owned selector

**Origin Selector**:
An Upstream List Entry variant containing an HTTP(S) scheme, lowercase ASCII hostname without wildcard syntax, and normalized effective port from 1 through 65535, matched exactly. A source that omits its port uses the scheme's default port, so omitted and explicit-default spellings form the same Origin Selector; IP literal spelling is not canonicalized, so a valid Origin Selector is not guaranteed to identify a browser-reachable origin.
_Avoid_: Full Origin, URL selector, scheme-qualified domain, wildcard-bearing origin

**HTTPS Facade**:
The automatic browser-routing and TLS-terminating reverse-proxy ability for HTTP origins, active when Traffic Routing Ready holds, the Served Traffic Projection contains at least one unshadowed HTTPS Facade Route with matching forwarding behavior, and the current usable UserCA identity matches that projection's interception identity. It has no demand or blocked state; not-usable UserCA, UserCA Assessment Issue, or identity mismatch makes it inactive, while HTTP CORS remains independent. Intercepted browser HTTPS requests are sent to the selected HTTP upstream, with Traffic Projection Current and per-service PAC delivery warnings reported separately.
_Avoid_: HTTP Origin HTTPS Facade, HTTP selector HTTPS Intent, implicit HTTPS selector, HTTPS upstream, TLS passthrough, scheme alias

**HTTPS Facade Projection**:
The complete immutable HTTPS Facade interpretation of Origin Selectors, containing the unshadowed browser HTTPS origin to HTTP upstream mappings selected by HTTPS Routing Specificity independently of UserCA Usability. Gateway forms the projection once for PAC Routing and includes matching forwarding behavior in the same coherent traffic publication, so browser selection and intercepted forwarding cannot derive different mappings.
_Avoid_: Proxy-owned selector filtering, PAC-only façade routes, readiness-coupled projection, duplicated façade policy

**HTTPS Facade Route**:
An exact browser HTTPS origin and HTTP forwarding target derived from an HTTP Origin Selector while Managed HTTPS Routing is active. It keeps the normalized hostname, maps HTTP port 80 to browser HTTPS port 443, preserves every other port for the browser side, and always forwards to the selector's original normalized HTTP port.
_Avoid_: implicit HTTPS selector, same-port-only façade, source-spelling-dependent route, native HTTPS route

**HTTPS Facade Redirect Adaptation**:
A response behavior where an absolute `Location` targeting the selected HTTP origin is rewritten to the browser-facing origin of the admitting HTTPS Facade Route while preserving path, query, and fragment. Relative locations and other origins remain unchanged; the proxy does not rewrite response bodies, mutate cookie or security policy, or synthesize `Forwarded` or `X-Forwarded-Proto` request metadata, so the HTTP origin need not know about the façade.
_Avoid_: response-body rewriting, unrelated redirect rewriting, forwarded-proto dependency, automatic Secure cookie, injected HSTS

**HTTPS Routing Specificity**:
The precedence rule for selectors that cover the same browser HTTPS origin: an HTTPS Origin Selector selects its native HTTPS upstream over an HTTP Origin Selector's HTTPS Facade; among HTTP Origin Selectors, a façade preserving the selector's port takes precedence over the special HTTP-port-80 to HTTPS-port-443 translation; and an exact HTTPS Facade Route takes precedence over a broader Host Selector's native HTTPS route. Specificity is independent of Upstream List source and line order.
_Avoid_: first selector wins, source precedence, Host Selector override, ambiguous HTTPS forwarding

**Upstream List Routing Policy**:
A runtime interpretation owned by the PAC Routing module that decides whether normalized Upstream List Entries send a browser request to the Proxy Listener without revalidating them. Gateway Runtime supplies entries from the current effective Upstream List Projection rather than a source representation or diagnostic state.
_Avoid_: Gateway diagnostic-state dependency, proxy admission policy, raw string matching, duplicated PAC matchers, downstream Upstream List validation

**Line-Level Upstream Validation**:
An Upstream List behavior where each line is validated independently so valid Upstream List Entries are applied while invalid lines are ignored and reported with their line, active text, and a stable generic syntax diagnostic as Upstream List Warnings. Host Selectors and Origin Selectors use the same conservative DNS/IP hostname validation; only Host Selectors support Single-Label Wildcard matching.
_Avoid_: Line-Level Domain Validation, parser-reason diagnostic taxonomy, silent invalid entry, whole-list rejection, invalid line as active entry

**Upstream List Deduplication**:
An Upstream List module behavior applied both within each source projection and while merging source projections into the Effective Upstream List. Equivalent normalized entries are treated as one active entry, keeping the first occurrence in Global-then-Directory order and ignoring later duplicates without giving either source routing precedence. An omitted Origin Selector port and its scheme's explicit default port are equivalent; PAC Routing separately deduplicates equivalent derived PAC Routes.
_Avoid_: Gateway-owned selector equality, duplicate source selectors, line-count domains, source override, PAC-owned source deduplication

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

**Preflight Vary Declaration**:
A Local Preflight Answer behavior where `Vary` names Origin, requested method, requested headers, and requested private-network access because those request values determine the generated response.
_Avoid_: Origin-only preflight Vary, cache-enabled preflight

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

**Uncached Preflight Answer**:
A Local Preflight Answer behavior that sets `Access-Control-Max-Age: 0` so each browser preflight is independently answered from its request instead of being reused from the browser's separate CORS-preflight cache.
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
A Response Repair behavior where every existing upstream `Access-Control-*` header is removed before the gateway writes the Reflective DEV/QA Policy headers.
_Avoid_: CORS header merge, duplicate CORS headers

**Concrete Exposed Headers**:
A Response Repair behavior where `Access-Control-Expose-Headers` deterministically lists actual upstream response header names, excluding CORS and cookie-setting headers, and is omitted when no names remain.
_Avoid_: wildcard expose-headers

**HTTP CORS Scope**:
The product boundary that applies Origin-Gated Rewriting to HTTP responses, including upgrade responses, without rewriting request origins, changing upgraded protocol frames, or claiming to repair WebSocket origin policy.
_Avoid_: WebSocket CORS support, protocol frame rewriting, upgrade exemption

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

QA engineer: "Yes. Proxy handles every request reaching the Proxy Listener. Without an active ready HTTPS Pipeline it direct-tunnels CONNECT; with one, it intercepts every CONNECT rather than using the Upstream List for admission or interception scope. Current HTTPS Facade Projections affect only forwarding after interception."

Developer: "When will HTTPS domains route through the gateway?"

QA engineer: "An HTTPS Origin Selector admits the HTTPS Pipeline. Trust-Aware PAC Routing sends HTTPS Origin and Host Selector routes through the gateway only after that pipeline settles ready."

Developer: "Do I need to maintain the PAC file?"

QA engineer: "No, PAC Routing projects the current effective Upstream List, HTTPS Pipeline state, and runtime endpoint into the Generated PAC."

Developer: "How do Upstream List changes reach the operating system proxy?"

QA engineer: "The PAC Endpoint serves the current Generated PAC, and Managed PAC advances its publication generation for every adopted Upstream List Projection, including warning-only changes."

Developer: "Can I avoid changing my system proxy settings?"

QA engineer: "No, the gateway uses Managed System Proxy so application requests keep their original URLs."

Developer: "Will start ask before changing PAC settings?"

QA engineer: "No. Start automatically manages empty or seamless-cors-owned PAC settings and preserves foreign or unobservable settings by excluding them."

Developer: "Will the gateway configure Firefox or browser profile certificate stores?"

QA engineer: "No, OS Trust Only keeps certificate trust limited to the current user's operating-system trust store."

Developer: "What happens when HTTPS Readiness is not ready?"

QA engineer: "The admitted HTTPS Pipeline keeps CONNECT direct and omits HTTPS PAC routes, while its source-specific detail explains whether UserCA is normally not usable, assessment failed, or signing material was inconsistent."

Developer: "Will the first run automatically trust a CA?"

QA engineer: "No. Gateway start assesses HTTPS Readiness only when HTTPS Intent admits the pipeline and never mutates trust; `seamless-cors install` is the explicit operation that requests platform approval."

Developer: "Will the gateway keep reusing the same development CA?"

QA engineer: "Yes. Installed User CA reuses trusted CA material across trusted gateway starts until it is removed or replaced."

Developer: "What removes trusted CA material?"

QA engineer: "CA lifecycle commands remove seamless-cors-owned CA trust and local CA material."

Developer: "What happens if the gateway crashes before removing its CA?"

QA engineer: "Installed User CA remains available for the next trusted gateway start unless the user removes it."

Developer: "Will every operating system have the same managed setup in v1?"

QA engineer: "Every supported platform needs a managed PAC adapter; platforms without one are not supported yet."

Developer: "After I update the Upstream List, do I need to restart the gateway?"

QA engineer: "No, Gateway continuously observes the file, adopts every successfully decoded Upstream List Projection, switches to the resulting Traffic Projection, and asks Managed PAC to deliver its PAC URL independently to each managed Network Service."

Developer: "What happens if I save an invalid config file while the gateway is running?"

QA engineer: "Rejected Upstream List Contents produce an Upstream List Projection Issue while Gateway selects an Empty Upstream List, serves the resulting Traffic Projection, and continues observing for a valid correction."

Developer: "What if my config still has removed listener or managed-proxy settings?"

QA engineer: "Lenient Configuration Shape treats them like any other unknown settings, so they do not affect gateway behavior."

Developer: "Do I need a command for every setting?"

QA engineer: "No, the Minimal Command Surface keeps commands rare and lets configuration drive behavior while the gateway is running."

Developer: "Which commands exist in v1?"

QA engineer: "`start`, `stop`, and `status` manage the gateway runtime; CA Lifecycle Commands manage Installed User CA trust."

Developer: "Does `start` launch a background service?"

QA engineer: "No, Foreground Start keeps the gateway attached and lets Ctrl-C execute Owner Stop."

Developer: "Does Ctrl-C clean up the proxy and CA?"

QA engineer: "Ctrl-C executes Owner Stop, which closes traffic and attempts cleanup for seamless-cors-owned managed PAC settings and the Gateway State Cache. Installed User CA trust remains until a CA Lifecycle Command removes it."

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

QA engineer: "With an active HTTPS Pipeline, install settles the pipeline from its fresh assessment and can recover MITM and HTTPS PAC routing immediately. Without HTTPS Intent, install changes only UserCA facts."

Developer: "What happens if the gateway crashes after changing my proxy settings?"

QA engineer: "Gateway Footprint Cleanup removes leftover seamless-cors-owned managed PAC settings before a new start or stop finishes."

Developer: "What if I already use a corporate proxy?"

QA engineer: "Managed PAC Configuration Protection excludes foreign PAC settings and reports them without replacing them."

Developer: "What do I need to configure before starting?"

QA engineer: "Only the Upstream List: one Host Selector or Origin Selector per line."

Developer: "Where do config files live by default?"

QA engineer: "The Global Upstream List uses the platform-native XDG configuration home, Installed CA Storage uses XDG state, Gateway coordination uses the XDG runtime directory, and the Directory Upstream List uses the exact Start working directory."

Developer: "Where does the Gateway State Cache live?"

QA engineer: "The Gateway State Cache lives with the Gateway Ownership Lock in the Gateway Runtime Directory selected by the Gateway Coordination Environment."

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

Developer: "Does the gateway select how long browsers cache local preflight answers?"

QA engineer: "Yes. Uncached Preflight Answer sets the preflight cache lifetime to zero so each preflight reaches the gateway."

Developer: "Will the gateway rewrite `Origin` if the upstream rejects it?"

QA engineer: "No, No Request Header Rewriting keeps upstream application checks out of scope."

Developer: "What if the API already returns partial CORS headers?"

QA engineer: "CORS Header Replacement removes them first so the browser sees one consistent DEV/QA policy."

Developer: "How are response headers exposed to frontend code?"

QA engineer: "Concrete Exposed Headers lists the upstream response headers instead of using a wildcard."

Developer: "Will the gateway fix WebSocket origin behavior?"

QA engineer: "No. HTTP CORS Scope may repair the HTTP upgrade response, but it does not rewrite the request Origin or change WebSocket frames."

Developer: "What if the WebSocket upstream is in the Upstream List?"

QA engineer: "PAC Routing may send its handshake through Proxy like other selected HTTP traffic, while WebSocket origin policy and frames remain unchanged."

Developer: "Will the gateway rewrite cookies so login works?"

QA engineer: "No, Cookie Out of Scope leaves cookie and authentication behavior unchanged."
