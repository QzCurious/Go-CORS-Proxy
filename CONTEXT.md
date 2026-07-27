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
The foreground process-lifetime module that holds the Gateway Ownership Lease, publishes Gateway Router discovery state, provides router-only serving and direct-start owner startup, and, when active, owns Gateway Runtime and Gateway Footprint Cleanup responsibility.
_Avoid_: daemon supervisor, client command, detached runtime owner, terminal command renderer

**Gateway Runtime**:
The live traffic-serving engine that owns the proxy listener, proxy server, PAC listener, PAC server, live configuration, runtime close behavior, and fatal runtime error reporting without installing or unsetting OS PAC state.
_Avoid_: lifecycle facade, command router, OS proxy manager, cleanup owner

**Router-Only Serve**:
A command behavior where the command becomes the Gateway Owner and starts the Gateway Router as an HTTP client entry point without automatically starting Gateway Runtime, running Gateway Footprint Cleanup at serve startup, or changing managed OS state; it fails clearly when a Gateway Owner already exists and may claim stale Gateway State Cache only after verification finds no reachable owner.
_Avoid_: implicit gateway start, daemonized start, hidden lifecycle activation, stale-cache cleanup, OS PAC repair

**Router-Hosted Start**:
An HTTP start behavior where a client calls `POST /start` against an existing router-only Gateway Owner, renders PAC Replacement Consent when the result requires it, and retries with accepted consent to activate Gateway Runtime without creating a competing gateway process.
_Avoid_: start plan, CLI start delegation, terminal prompt in serve, duplicate router start, serve-blocked start, split-brain gateway

**Start-Hosted Router**:
A startup behavior where gateway start becomes the Gateway Owner by hosting the Gateway Router and Gateway Runtime while activation remains governed by the Start Sequence.
_Avoid_: router-only fallback, control endpoint replacement, implicit consent

**Router-Hosted Start Failure**:
A failed start behavior where direct `start` exits and removes owner visibility, while `/start` sent to an existing router-only Gateway Owner leaves that owner alive.
_Avoid_: surprise serve fallback, failed-start owner leak, serve shutdown on start rejection

**Owner-Blocked CLI Start**:
A CLI start behavior where `start` fails clearly when a valid Gateway Owner already exists instead of delegating activation to that owner.
_Avoid_: CLI start delegation, hidden HTTP start, remote foreground ownership

**Managed System Proxy**:
A traffic capture approach where the gateway configures the operating system or browser proxy settings on behalf of the user, so application requests keep their original URLs and no manual proxy setup is required.
_Avoid_: VPN, manual proxy, browser-only workaround

**Selective Managed Proxy**:
A Managed System Proxy behavior where only Domain List matches are routed through the gateway and all other traffic bypasses it.
_Avoid_: whole-system proxy, blanket proxying

**PAC Routing**:
A Selective Managed Proxy approach that uses a proxy auto-configuration file to route Domain List matches through the gateway while returning `DIRECT` for other traffic.
_Avoid_: global proxy, pass-through proxying

**Trust-Aware PAC Routing**:
A PAC Routing behavior where matched HTTPS traffic is routed through the gateway only when Trusted HTTPS Interception is enabled.
_Avoid_: routing unrepaired HTTPS, unnecessary HTTPS proxying

**Generated PAC**:
A runtime proxy auto-configuration artifact derived from Live Configuration and the Domain List, not edited directly by the user.
_Avoid_: user-authored PAC, manual PAC rules

**PAC Route Set**:
The Domain Routes and Origin Routes derived inside the PAC Routing module from normalized Domain List Entries and the current Trusted HTTPS Interception state, keeping the Generated PAC JavaScript mostly static.
_Avoid_: hand-built JavaScript rules, duplicated Domain List parsing, PAC-owned Domain List syntax

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
A stop behavior where `stop` and `/stop` tear down the Gateway Owner itself, including a router-only owner, and close Gateway Runtime before Gateway Footprint Cleanup so no new gateway traffic is accepted after runtime close completes.
_Avoid_: runtime-only stop, router-only survival, stop-as-status, start-survives-stop, accepted-before-cleanup, cleanup-before-runtime-close

**Owner Ending**:
A Gateway Owner lifecycle state that begins when Owner Stop takes precedence and lasts until stop succeeds or the process exits. A Retryable Stop Failure leaves the owner ending and teardown-only: stop retries and status remain available, while a Start Sequence is rejected as `stop-cancelled`.
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
A lifecycle behavior where an unavailable, invalid, expired, or near-expiry Installed User CA is replaced before trusted HTTPS interception starts.
_Avoid_: expired trust reuse, surprise HTTPS breakage, silent CA replacement

**CA Replacement Rule**:
A CA lifecycle rule where only fully usable Installed User CA state is reused, local permission issues are repaired in place, and every other unusable state is treated by removing owned CA state before installing fresh CA material.
_Avoid_: partial CA reuse, trusting stale local material, special-case invalid states

**CA Ensure**:
An independent lifecycle operation used by the Start Sequence and CA Lifecycle Commands to verify that usable Installed User CA trust exists, requesting platform approval only when trust must be added or replaced. Successful CA Ensure remains complete when later Gateway Activation is declined or fails.
_Avoid_: activation-owned CA setup, repeated trust prompt, start without usable trust, install-only trust repair, rollback after PAC decline

**Cancelled CA Ensure Reconciliation**:
A cancellation behavior where interrupted CA Ensure reinspects trust and local material, preserves and reports a fully usable Installed User CA, accepts fully absent state, and removes seamless-cors-owned partial or mismatched state before cancellation completes.
_Avoid_: assumed cancellation outcome, trusted CA without local key, orphaned key material, Gateway Footprint Cleanup of CA state

**CA Mutation Lease**:
A cross-process, cancellation-aware exclusivity rule covering CA Ensure, replacement, uninstall, and cancellation reconciliation for the single Installed User CA. It serializes only CA mutation, waits for an existing holder to settle before reinspecting state, and never gates cleanup, PAC work, or an entire Start Sequence.
_Avoid_: global lifecycle mutex, start lock, duplicate CA approval, concurrent CA replacement, stale lock file

**CA Ensure Result**:
A surface-neutral outcome reporting whether required CA Ensure installed or reused the Installed User CA and when that trust identity expires. Trusted Runtime Admission refreshes this result when it adopts a different currently usable Installed User CA; a Start Sequence with CA trust disabled skips CA inspection and carries no CA Ensure Result.
_Avoid_: Start Guidance, terminal acknowledgement, hidden partial start state, success-only CA detail

**Post-CA Start Failure**:
A Start Sequence infrastructure failure that occurs after successful CA Ensure and therefore reports both the failure and the completed CA Ensure Result. The Installed User CA remains usable and is not rolled back.
_Avoid_: total start rollback, hidden CA installation, CA success converted to failure, PAC consent result

**Single Installed CA State**:
A CA lifecycle invariant where multiple seamless-cors-owned CA trust identities are treated as repair-needed state and replaced by one usable Installed User CA.
_Avoid_: multiple active development CAs, ambiguous signing identity

**CA Material Integrity**:
A CA lifecycle invariant where current-user CA trust and the local signing key must match; missing or mismatched CA material is treated as repair-needed state.
_Avoid_: trusted cert without signing key, orphaned signing key, mismatched CA pair

**OS-Backed CA Installation**:
A CA lifecycle invariant where Installed User CA state requires current-user operating-system trust; local CA material alone is not installed trust.
_Avoid_: file-only installation, assuming trust from local material

**CA Permission Repair**:
A CA lifecycle behavior where otherwise-valid Installed User CA material with loose local file permissions is tightened in place without replacing trusted CA identity.
_Avoid_: permission-triggered CA rotation, loose CA key permissions

**Leaf Certificate Reuse**:
A runtime behavior where generated per-host HTTPS certificates may be reused within a gateway process until their generation age exceeds the cache reuse limit, but are not persisted as trusted identity across gateway runs.
_Avoid_: persistent leaf certificate inventory, per-request certificate churn, expiry-only cache policy

**Per-Host Leaf Certificate**:
A generated HTTPS server certificate for the specific upstream hostname being intercepted, signed by the Installed User CA on demand.
_Avoid_: Domain List-wide leaf certificate, wildcard-first certificate strategy, persisted leaf identity

**Trusted HTTPS Interception**:
A lifecycle behavior where HTTPS traffic that reaches the Proxy Listener is intercepted only when `ca-trusted` is enabled and the Installed User CA is trusted for the current user.
_Avoid_: untrusted HTTPS interception, broken MITM, Domain List gated interception

**Explicit Trusted HTTPS**:
A lifecycle default where generated configuration starts with `ca-trusted: false` and an omitted setting also resolves to false. HTTPS interception is enabled only by an explicit `ca-trusted: true`. Installed User CA trust is still added or replaced only through CA Ensure and required platform approval.
_Avoid_: default HTTPS interception, silent trust installation, config-only trust

**Live Configuration**:
A gateway module and code boundary that exclusively owns observing and reading user-editable configuration sources, interpreting the Config File, passing raw Domain List content to the Domain List module, and exposing validated semantic configuration at startup and when it changes. Hot-applicable request settings use the newest valid Config File and the valid entries from the newest readable Domain List without requiring the user to manually restart or reload the browser or gateway.
_Avoid_: external file-watcher boundary, raw filesystem event API, platform-specific watcher abstraction, consumer-owned config deduplication, runtime-only reload, manual reload, restart requirement, stale configuration, separate config package

**Live Configuration Snapshot**:
The validated immutable current-state configuration value exposed by Live Configuration, including runtime-effective settings, whether Trusted HTTPS Interception intent is pending until the next runtime, and diagnostic source metadata. Its identity is based on parsed meaning rather than source-file representation, and change delivery may coalesce unconsumed intermediate snapshots in favor of the latest value.
_Avoid_: configuration event history, raw YAML config, file-change event, content fingerprint, watcher notification

**Domain List Entries Revision**:
A run-local, non-persistent monotonic identity exposed with Live Configuration, beginning with the initial validated Domain List Entries and advancing only when the normalized, deduplicated entry set changes. It allows consumers to apply the newest coalesced routing input without treating Domain List path changes or representation-only edits as routing changes.
_Avoid_: persisted Domain List revision, Domain List file revision, PAC content comparison, source-content fingerprint, path-change revision

**Config File**:
The user-editable YAML source read by Live Configuration for gateway settings such as Domain List location and Trusted HTTPS Interception intent; it is an ordinary file addressed by a cleaned absolute path and is not a separate code boundary from Live Configuration. Live change observation is supported only when the file is hosted on a local filesystem.
_Avoid_: symlinked config, network-filesystem observation guarantee, Explicit Configuration package, public raw config model, config subsystem

**All-Or-Nothing Config**:
A configuration behavior where a valid config file is applied as a whole and an invalid config file is rejected without partially changing gateway behavior.
_Avoid_: partial config application, half-applied config

**Lenient Configuration Shape**:
A clean-break configuration behavior where only current settings affect gateway behavior and unknown settings are ignored without aliases, migration, or old-version-specific handling.
_Avoid_: backward-compatible alias, migration, obsolete-setting special case

**Fatal Config Error**:
A configuration behavior where an invalid, missing, or unreadable config file causes the gateway to report the validation problem, perform Gateway Footprint Cleanup, and stop.
_Avoid_: silent config fallback, stale config after invalid edit

**Gateway Control Command**:
A user-facing command that controls gateway-owned state or reports on it, including start, serve, stop, status, UserCA install, and UserCA uninstall.
_Avoid_: lifecycle operation, command service, control endpoint operation

**Start Sequence**:
The public Gateway Module start flow that verifies ownership, performs early ownership-aware Gateway Footprint Cleanup, loads valid configuration, completes required CA Ensure only when effective configuration enables CA trust, and then attempts Gateway Activation. Direct start removes stale owner state before claiming ownership, while router-hosted start preserves the live owner cache; cleanup failure is returned as a structured start outcome identifying each failed cleanup subject. The sequence composes independent CA and PAC lifecycle operations without merging their consent and is the only public route to Gateway Activation.
_Avoid_: monolithic activation, public raw activation, combined consent, PAC-first start, cleanup-after-approval

**Gateway Activation**:
The internal post-CA-Ensure operation that assesses PAC Replacement Consent, admits and begins serving Gateway Runtime, installs managed PAC state, and then produces Start Guidance. It is invoked only through the Start Sequence so callers cannot bypass cleanup, configuration validation, CA readiness, or traffic-before-PAC ordering.
_Avoid_: public activation command, CA installation, CA Trust Consent, lifecycle activation, runtime startup, command rendering, lifecycle orchestration package

**Automatic Listeners**:
A lifecycle behavior where the gateway chooses available loopback ports for its proxy, PAC, and router endpoints at startup, then wires dependent gateway state in sequence.
_Avoid_: user-selected listener ports, fixed listener ports, manual listener addresses

**Loopback Default**:
A listener behavior where gateway endpoints bind to loopback.
_Avoid_: LAN-exposed proxy, user-selected bind address

**Proxy Listener**:
A local proxy endpoint where traffic that reaches it is eligible for CORS repair, normally reached through PAC Routing for Domain List matches.
_Avoid_: manual proxy endpoint, browser setup address, generic listen, gatewayListen

**CORS Proxy**:
The gateway module behind the Proxy Listener that owns CORS repair, Local Preflight Answer, Response Repair, and Trusted HTTPS Interception behavior for traffic that reaches it.
_Avoid_: Domain List admission module, PAC Routing module, generic proxy

**Explicit Configuration**:
Deprecated term for Config File. Prefer Config File when referring to the user-editable YAML source and Live Configuration when referring to the resolved runtime-effective configuration.
_Avoid_: hidden user settings, flag-only configuration, listener address configuration

**Path Expansion**:
A configuration behavior where path fields support home-directory and environment-variable expansion.
_Avoid_: arbitrary string expansion

**Home Config Directory**:
The default seamless-cors location at `.seamless-cors` under the user's home directory. Live Configuration owns configuration sources within it, while Gateway Coordination and UserCA independently own their state in dedicated subdirectories.
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
A process-lifetime, operating-system-backed exclusive lease that is the authoritative Single User Instance ownership signal. A contender must acquire it before verification, cleanup, or publication and fails immediately when another process holds it; the Gateway State Cache remains discovery data rather than ownership authority.
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
A start-only behavior where a missing default Config File and any missing configured Domain List are created automatically before validation continues, including required parent directories. The same start command continues with an Empty Domain List, while passive configuration reads remain non-mutating and an existing unsafe, non-file, or unreadable Domain List remains an error.
_Avoid_: init command, manual file scaffolding, read-time mutation, default-path-only Domain List creation, replacing invalid paths

**Commented Default Config**:
A Configuration Bootstrap behavior where generated configuration includes short comments only for meaningful user-editable settings.
_Avoid_: opaque default config, verbose manual, runtime listener settings

**Start Guidance**:
A start-time user-facing output behavior shown only after required consent and approval have succeeded, Gateway Runtime is serving, and Managed PAC installation has reached at least one selected service. It points to editable configuration and managed state instead of runtime listener endpoints.
_Avoid_: pre-approval running message, listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

**Start Guidance Detail**:
A surface-neutral successful start result detail containing the user-relevant configuration and lifecycle state needed to render Start Guidance without exposing runtime listener endpoints.
_Avoid_: CA Ensure Result, terminal start text, listener status detail, proxy setup instructions

**Already-Running Start**:
An idempotent start result where executing start against an active Gateway Runtime reports that the gateway is already running without treating the command as a failure.
_Avoid_: duplicate runtime activation, start failure for active runtime, second owner

**Execute-Time Start Assessment**:
A start execution rule where every `ExecuteStart` attempt computes current PAC Replacement Consent conditions before mutating, returning a structured consent-required result when supplied confirmation is missing, refers to different foreign PAC state, or is otherwise no longer sufficient.
_Avoid_: start plan, prior-result authorization, mutation-before-assessment

**Single-Flight Start**:
A start behavior where a Gateway Owner accepts only one complete Start Sequence at a time, acquiring exclusivity before cleanup and holding it through configuration, CA Ensure, PAC assessment, Gateway Activation, and the returned outcome. Concurrent attempts return already-running or start-already-mutating without duplicating lifecycle work.
_Avoid_: cross-command lifecycle lock, CA-command blocking, activation-only lock, queued start, duplicate CA approval, duplicate mutation, competing activation, start plan reservation

**Stop-Preempted Start**:
A lifecycle precedence rule where `stop` cancels and supersedes an in-progress Start Sequence, waits for CA Ensure cancellation reconciliation and other safe boundaries, then performs final Gateway Footprint Cleanup and ends ownership. Completed CA Ensure remains durable, while cancelled activation cannot later publish runtime or install PAC state.
_Avoid_: stop-busy result, start mutex wait, cleanup-before-cancellation, post-stop PAC install, CA rollback

**Stop-Cancelled Start**:
A surface-neutral expected start outcome returned to the original start caller after stop preemption reaches a safe boundary. It preserves any completed CA Ensure Result without treating the cancellation as an infrastructure failure.
_Avoid_: context-canceled error, started result, hidden CA completion, stop failure

**Trusted Runtime Admission**:
A pre-traffic Gateway Runtime rule where a runtime using trusted HTTPS is published as active, then validates that active configuration still requires CA trust and loads the currently usable Installed User CA before serving. If that identity differs from the authority prepared earlier, admission adopts it and refreshes the CA Ensure Result without requesting platform approval; unusable state withdraws the runtime and fails Gateway Activation.
_Avoid_: start mutex, identity-change failure, admission-time approval, pre-publication-only CA check, traffic-before-validation, continuous CA polling, untrusted degraded runtime

**Admitting Gateway Runtime**:
A published but not-yet-serving Gateway Runtime state used during Trusted Runtime Admission. Status reports it as starting, and the Active Trusted Runtime CA Guard applies, while failed admission withdraws it without ever accepting traffic.
_Avoid_: running status, undiscoverable trust use, traffic-serving runtime, CA mutation window

**Admission CA Adoption**:
A Trusted Runtime Admission behavior that replaces the prepared authority with a different currently trusted Installed User CA and matching local key before traffic begins. It is validation and adoption of completed CA lifecycle work, not another CA Ensure or a hot runtime trust swap.
_Avoid_: admission-time CA install, second trust prompt, stale CA Ensure Result, post-traffic authority rotation

**Platform-Approval-Denied Start**:
A start execution outcome where required platform-owned trust approval is denied during CA Ensure, so Gateway Activation is not attempted and managed PAC state is not activated.
_Avoid_: consent-required start, infrastructure error, partial trusted start

**Pending Lifecycle Change**:
A restart-applied Config File change detected while the gateway is running and reported to the user without being applied automatically.
_Avoid_: surprise permission prompt, implicit restart

**Restart-Applied Lifecycle**:
A lifecycle behavior where Pending Lifecycle Changes take effect only after the gateway is restarted.
_Avoid_: apply-lifecycle command, hot lifecycle swap

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
A lifecycle boundary where PAC Replacement Consent and PAC Routing setup follow gateway start independently of whether the Domain List currently has active entries.
_Avoid_: domain-gated PAC setup, delayed proxy ownership, route-count-based lifecycle

**CA Trust Consent**:
A platform approval moment required before adding or replacing Installed User CA trust for HTTPS interception, with gateway context shown only when the platform requires approval.
_Avoid_: implicit CA trust, repeated consent for unchanged trust, app-only trust prompt, PAC Replacement Consent Detail

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Installed User CA availability follow `ca-trusted` independently of whether the Domain List currently has active entries.
_Avoid_: domain-gated CA trust, implicit CA delay, route-dependent trust setup

**Start Sequence Order**:
A startup lifecycle order where Gateway Footprint Cleanup and configuration validation precede required CA Ensure, or skip CA lifecycle work entirely when effective `ca-trusted` is false; PAC Replacement Consent assessment then precedes Trusted Runtime Admission; admitted Gateway Runtime begins serving before Managed PAC installation; and Start Guidance follows successful installation on at least one selected service.
_Avoid_: PAC-before-runtime serving, PAC-first start, cleanup-after-approval, combined consent, traffic-before-trust validation, degraded trusted mode, CA rollback after PAC decline, start guidance before PAC installation

**All-Service PAC Management**:
A Managed System Proxy behavior where Gateway Activation selects every network service the supported platform adapter manages at activation time, so PAC Routing is consistent across the Managed PAC Service Set for that Gateway Owner run.
_Avoid_: active-service-only PAC, partial network setup, silent service expansion

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and viewing gateway status while runtime behavior follows Live Configuration.
_Avoid_: command-heavy configuration, flag-driven operation

**CA Lifecycle Commands**:
Top-level user-facing commands that explicitly install, repair, or remove the Installed User CA outside the normal start/stop gateway loop. An active trusted runtime permits idempotent install but blocks CA replacement and uninstall; an absent or untrusted runtime permits both lifecycle changes.
_Avoid_: nested CA command tree, hidden CA removal, per-start CA trust, config editing command, runtime CA rotation, active-trusted-runtime CA mutation, extra command confirmation

**Config-Independent CA Install**:
A CA lifecycle command boundary where installing or repairing the Installed User CA does not require, create, or modify the Config File, though existing configuration may be read for guidance.
_Avoid_: install-time config bootstrap, install changing `ca-trusted`, config-required CA repair

**Idempotent CA Install**:
A CA lifecycle command behavior where installing reports existing usable CA trust as success without requesting platform approval or changing CA material, including while a trusted Gateway Runtime is active.
_Avoid_: reinstalling usable CA, runtime CA rotation, noisy no-op install, repeated trust approval

**Active Trusted Runtime CA Guard**:
A CA lifecycle rule where an admitting or serving runtime using CA trust permits idempotent UserCA install but blocks CA replacement and UserCA uninstall until that runtime stops. A runtime whose effective configuration does not use CA trust does not hold this guard.
_Avoid_: blanket active-runtime block, runtime CA rotation, cached-leaf re-signing, active trusted CA removal

**Config-Independent CA Uninstall**:
A CA lifecycle command boundary where removing the Installed User CA does not modify the Config File; future trusted starts may reinstall trust when configuration still requests it.
_Avoid_: uninstall changing `ca-trusted`, disabling desired HTTPS interception, config-coupled removal

**Idempotent CA Uninstall**:
A CA lifecycle command behavior where uninstalling reports already-absent seamless-cors-owned CA trust and local CA material as success without changing configuration or requiring repair.
_Avoid_: missing-CA uninstall failure, forced repair before removal, noisy no-op uninstall

**Complete CA Uninstall**:
A CA lifecycle invariant where uninstall reports success only after seamless-cors-owned current-user CA trust and local CA material are both absent.
_Avoid_: false uninstall success, trusted CA without local material, leftover private key

**Foreground Start**:
A v1 runtime behavior where `start` runs attached in the foreground rather than launching an official background daemon.
_Avoid_: daemon mode, background start

**Client Command**:
A command invocation that asks an existing Gateway Owner to perform user-facing gateway work and then exits without owning process lifetime or Gateway Footprint Cleanup.
_Avoid_: detached owner, fake foreground control, remote Ctrl-C ownership

**Owner-Routed CA Lifecycle Command**:
A CA Lifecycle Command behavior where `install` and `uninstall` are sent to an existing Gateway Owner when one exists, including a router-only owner. Active runtime trust usage determines whether CA mutation is allowed, while idempotent install remains available.
_Avoid_: bypassing owner command authority, router-only local mutation, runtime CA rotation, active-trusted-runtime CA mutation, blanket active-runtime rejection

**Gateway Footprint Cleanup**:
An idempotent, ownership-aware lifecycle behavior that removes only stale or intentionally released seamless-cors-owned managed PAC and Gateway State Cache subjects while leaving Installed User CA state untouched. Direct start holds the Gateway Ownership Lease while removing stale cache and owned PAC, router-hosted start removes owned PAC while preserving its live owner cache, and stop removes both when ending ownership.
_Avoid_: unconditional cache removal, live-owner eviction, runtime cleanup, status cleanup, serve-start cleanup, broad cleanup, CA removal, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Gateway Footprint Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Read-Only Status**:
A status behavior that reports gateway, cleanup-needed, and Installed User CA state, including stale Gateway State Cache detection, without changing proxy settings, CA trust, local CA material, or runtime files.
_Avoid_: status-triggered cleanup, mutating status command

**Gateway Status State**:
A read-only gateway status vocabulary that describes whether the Gateway Owner and Gateway Runtime are absent, stale, router-only, starting through Trusted Runtime Admission, or running without encoding cleanup or Installed User CA health.
_Avoid_: cleanup status, CA health status, start result, runtime state file truth

**CA Health Status**:
A read-only status vocabulary for Installed User CA state using stable values such as usable, missing, expired, expiring-soon, invalid, multiple, mismatched-material, and unknown.
_Avoid_: prose-only CA state, status-triggered CA repair

**Diagnostic Runtime Endpoint**:
An automatically selected listener address shown by status for troubleshooting, not for user proxy setup or configuration.
_Avoid_: setup address, configured listener, manual proxy instruction

**Domain List**:
The user-managed newline-delimited configuration decoded by the Domain List module into Domain Selectors, Origin Selectors, and Domain List Warnings for PAC Routing; Live Configuration reads and observes its ordinary-file source at a cleaned absolute path. Live change observation is supported only when the file is hosted on a local filesystem.
_Avoid_: symlinked list, network-filesystem observation guarantee, proxy admission list, interception rules, proxy rules

**Domain List Comment**:
A full-line or inline note in the Domain List that is ignored during matching.
_Avoid_: comment-as-entry

**Empty Domain List**:
A valid Domain List state with no active entries, including a file that contains only comments, blank lines, or invalid lines carrying Domain List Warnings; the gateway keeps managed PAC Routing installed and matches no upstreams until valid Domain List Entries are added.
_Avoid_: startup failure for no active entries, proxy-all fallback

**Domain List Warning**:
A persistent line-level diagnostic for an invalid Domain List line that is ignored while other valid Domain List Entries remain active. Warning appearance, change, and clearing publish a new Live Configuration Snapshot for successful startup and runtime status rather than asynchronous notification; they do not advance Domain List Entries Revision unless the valid entry set also changes.
_Avoid_: silent invalid entry, fatal line error, transient log warning, asynchronous warning event, routing revision warning

**Fatal Domain List Error**:
A live configuration behavior where a missing, unreadable, or structurally undecodable Domain List reports the source problem, performs Gateway Footprint Cleanup, and stops the gateway. Individual invalid lines are Domain List Warnings rather than fatal errors.
_Avoid_: stale valid routing, unreadable-as-empty, silent source failure

**Domain List Entry**:
A normalized routing value decoded by the Domain List module as either a Domain Selector or an Origin Selector. Internal consumers that construct entries directly are responsible for satisfying the same normalized value contract.
_Avoid_: source-text-bearing entry, rule, matcher expression

**Domain Selector**:
A Domain List Entry variant containing a lowercase ASCII hostname and Hostname Match without a scheme or port, selecting matching hosts across HTTP and HTTPS on any port. Domain Selector wildcard syntax is interpreted only for this variant, and IP literal spelling is not canonicalized.
_Avoid_: Hostname Shorthand, scheme-less origin, port-qualified domain

**Domain Route**:
A scheme-qualified hostname match derived from a Domain Selector for the PAC Route Set. A Domain Selector derives an HTTP Domain Route and, when Trusted HTTPS Interception is enabled, an HTTPS Domain Route; each route matches its hostname pattern on any port.
_Avoid_: Origin Route, port-qualified domain, PAC-owned selector

**Origin Selector**:
A Domain List Entry variant containing an HTTP(S) scheme, lowercase ASCII hostname, and optional normalized explicit port, matched exactly without applying Domain Selector wildcard semantics. Port presence is part of selector identity, so an omitted port and the scheme's explicit default port remain distinct Origin Selectors; accepted explicit ports are not range-validated, and IP literal spelling is not canonicalized, so a valid Origin Selector is not guaranteed to identify a browser-reachable origin.
_Avoid_: Full Origin, URL selector, scheme-qualified domain

**Origin Route**:
An exact origin representation derived from an Origin Selector for the PAC Route Set. PAC Routing owns default-port interpretation: an HTTP selector always derives Origin Routes, while an HTTPS selector derives them only when Trusted HTTPS Interception is enabled; a selector with an omitted or explicit default port derives both implicit-port and explicit-port Origin Routes, while any other port derives one Origin Route. Equivalent derived routes are deduplicated.
_Avoid_: duplicate Origin Selector, URL rule, inferred PAC port

**Hostname Match**:
The explicit Domain Selector meaning that selects an exact hostname, exactly one leading subdomain label, or one-or-more leading subdomain labels without encoding that meaning in the normalized hostname.
_Avoid_: wildcard-bearing hostname, consumer-parsed wildcard

**Domain List Routing Policy**:
A runtime interpretation owned by the PAC Routing module that decides whether normalized Domain List Entries send a browser request to the Proxy Listener without revalidating them. Gateway Runtime supplies entries selected from the Live Configuration Snapshot rather than the snapshot itself.
_Avoid_: whole Live Configuration Snapshot dependency, proxy admission policy, raw string matching, duplicated PAC matchers, downstream Domain List validation

**Line-Level Domain Validation**:
A Domain List behavior where each line is validated independently so valid Domain List Entries are applied while invalid lines are ignored and reported precisely as Domain List Warnings.
_Avoid_: silent invalid entry, whole-list rejection, invalid line as active entry

**Domain List Deduplication**:
A Domain List module behavior where equivalent normalized source-level entries are treated as one active entry, keeping the first occurrence and ignoring later duplicates. Port presence is part of Origin Selector identity; PAC Routing separately deduplicates equivalent derived Origin Routes.
_Avoid_: duplicate source selectors, line-count domains, PAC-owned source deduplication

**Exact Domain Match**:
A Host Match that selects only the named hostname.
_Avoid_: implicit subdomain match, broad domain match

**Single-Label Wildcard**:
A Host Match written as `*.example.com` that selects exactly one leading subdomain label and does not select the parent domain or deeper subdomains.
_Avoid_: recursive wildcard, parent-domain wildcard, wildcard-bearing hostname

**Recursive Wildcard**:
A Host Match written as `**.example.com` that selects one or more leading subdomain labels at any depth without selecting the parent domain.
_Avoid_: zero-label wildcard, parent-domain wildcard, wildcard-bearing hostname

**Local Target**:
A localhost, loopback, private IP, or plain HTTP upstream entry that may be included in the Domain List for DEV/QA work.
_Avoid_: public-domain-only target, DNS-only target

**Bracketed IPv6 Selector**:
A Domain List Entry source form for an IPv6 target that uses bracketed host syntax with an optional HTTP(S) scheme and port.
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

QA engineer: "No, Selective Managed Proxy uses PAC Routing so only Domain List matches go through the gateway."

Developer: "Will HTTPS domains still route through the gateway when `ca-trusted` is false?"

QA engineer: "No, Trust-Aware PAC Routing sends those HTTPS requests direct because they cannot be repaired."

Developer: "Do I need to maintain the PAC file?"

QA engineer: "No, Generated PAC is derived from Live Configuration and the Domain List."

Developer: "How do Domain List changes reach the operating system proxy?"

QA engineer: "The PAC Endpoint serves the current Generated PAC, and the gateway refreshes the owned PAC URL Version when supported PAC clients need a new URL to observe the update."

Developer: "Can I avoid changing my system proxy settings?"

QA engineer: "No, the gateway uses Managed System Proxy so application requests keep their original URLs."

Developer: "What if I decline the PAC Replacement Consent prompt?"

QA engineer: "Start stops without changing machine proxy settings because there is no manual proxy fallback."

Developer: "Will the gateway configure Firefox or browser profile certificate stores?"

QA engineer: "No, OS Trust Only keeps certificate trust limited to the current user's operating-system trust store."

Developer: "What happens when `ca-trusted` is false?"

QA engineer: "Trust-Aware PAC Routing does not send matched HTTPS traffic to the Proxy Listener without Trusted HTTPS Interception."

Developer: "Will the first run automatically trust a CA?"

QA engineer: "Explicit Trusted HTTPS keeps HTTPS interception disabled in generated configuration until the user enables it, and CA Ensure still requires platform approval before adding user trust."

Developer: "Will the gateway keep reusing the same development CA?"

QA engineer: "Yes. Installed User CA reuses trusted CA material across trusted gateway starts until it is removed or replaced."

Developer: "What removes trusted CA material?"

QA engineer: "CA lifecycle commands remove seamless-cors-owned CA trust and local CA material."

Developer: "What happens if the gateway crashes before removing its CA?"

QA engineer: "Installed User CA remains available for the next trusted gateway start unless the user removes it."

Developer: "Will every operating system have the same managed setup in v1?"

QA engineer: "Every supported platform needs a managed PAC adapter; platforms without one are not supported yet."

Developer: "After I update the Domain List, do I need to restart the gateway?"

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

Developer: "Can editing configuration unexpectedly trigger an OS permission prompt?"

QA engineer: "No, lifecycle settings live in the Config File but become a Pending Lifecycle Change while the gateway is running."

Developer: "What happens if a default listener port is already in use?"

QA engineer: "Automatic Listeners choose available loopback ports at startup."

Developer: "Can other machines use my gateway by default?"

QA engineer: "No, Loopback Default keeps listener endpoints local."

Developer: "Do stop and status go through the proxy listener?"

QA engineer: "No, Gateway Router keeps command traffic separate from browser proxy traffic."

Developer: "Do I need to know which listener browsers use?"

QA engineer: "No, PAC Routing connects browsers to the Proxy Listener through managed proxy settings."

Developer: "How do pending lifecycle changes take effect?"

QA engineer: "Restart-Applied Lifecycle means they apply on the next gateway start."

Developer: "What happens if the gateway crashes after changing my proxy settings?"

QA engineer: "Gateway Footprint Cleanup removes leftover seamless-cors-owned managed PAC settings before a new start or stop finishes."

Developer: "What if I already use a corporate proxy?"

QA engineer: "PAC Replacement Consent shows the current managed PAC state and asks before replacing existing PAC settings for this run."

Developer: "What do I need to configure before starting?"

QA engineer: "The Config File contains the meaningful user settings, while the Domain List stays as the easy one-domain-per-line file."

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

Developer: "Can config paths use `~` or environment variables?"

QA engineer: "Yes, Path Expansion applies to path fields only."

Developer: "Can the gateway start with no domains?"

QA engineer: "Yes, Empty Domain List is valid, so the gateway runs while PAC Routing matches no upstreams until valid Domain List Entries are added."

Developer: "Do I need to run an init command first?"

QA engineer: "No, Configuration Bootstrap creates a missing default Config File and any missing configured Domain List, and Empty Domain List lets that same start command keep running with no matched upstreams yet."

Developer: "Will generated config explain the settings?"

QA engineer: "Yes, Commented Default Config keeps the generated file short but understandable."

Developer: "Can I write just `api.dev.example.com`?"

QA engineer: "Yes, that Domain List Entry uses Hostname Shorthand; add an HTTP(S) scheme or port only when it should constrain matching."

Developer: "Can I annotate domains in the Domain List?"

QA engineer: "Yes, Domain List Comment supports full-line and inline comments."

Developer: "Does hostname shorthand include custom ports?"

QA engineer: "Yes, Hostname Shorthand may include a port that selects that port across HTTP and HTTPS."

Developer: "What if one Domain List line is wrong?"

QA engineer: "Line-Level Domain Validation ignores that line, reports a Domain List Warning, and continues routing with the valid Domain List Entries."

Developer: "What if I save an invalid Domain List while the gateway is running?"

QA engineer: "Invalid lines produce Domain List Warnings while valid entries are applied; Fatal Domain List Error is reserved for a missing, unreadable, or structurally undecodable source."

Developer: "Does `api.dev.example.com` include its subdomains?"

QA engineer: "No, Exact Domain Match requires an explicit wildcard when subdomains should be included."

Developer: "Does `*.example.com` match `deep.api.example.com`?"

QA engineer: "No, Single-Label Wildcard matches only one subdomain label; use Recursive Wildcard when deeper subdomains should also match."

Developer: "Can I include my local API or a LAN staging service?"

QA engineer: "Yes, Local Targets are allowed."

Developer: "Can I write IPv6 shorthand in the Domain List?"

QA engineer: "Yes, Bracketed IPv6 Selector accepts scheme-less forms such as `[::1]:3000`."

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

Developer: "What if the WebSocket domain is in the Domain List?"

QA engineer: "WebSocket Skip still forwards it without gateway repair."

Developer: "Will the gateway rewrite cookies so login works?"

QA engineer: "No, Cookie Out of Scope leaves cookie and authentication behavior unchanged."
