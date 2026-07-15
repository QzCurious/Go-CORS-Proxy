# Reliable Config File Change Observation

## Question

What is the smallest reliable way for Live Configuration to notice user-edited Config File and Domain List content on macOS and Windows, including editor atomic saves, without retaining polling as a fallback?

This note uses only official documentation and first-party source code. It records implementation evidence, not a new domain decision.

## Primary-source findings

### `fsnotify`: observe the parent directory and treat events as hints

`fsnotify` supports macOS through `kqueue` and Windows through `ReadDirectoryChangesW` ([support table](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/README.md#L1-L18)). Its own guidance says not to watch an individual file: editors commonly save by writing a temporary file and renaming it over the target, which replaces the watched file. It recommends watching the parent directory and filtering `Event.Name` ([FAQ](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/README.md#L122-L133)).

The portable operations are `Create`, `Write`, `Remove`, `Rename`, and `Chmod`; an event is a bitmask and may contain multiple operations ([API source](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/fsnotify.go#L97-L120)). One user action may emit multiple writes, `Rename` reports the old path, and `Chmod` is platform-dependent: `kqueue` can report truncation as `Chmod`, while Windows never emits it ([operation definitions](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/fsnotify.go#L133-L171)). `fsnotify` generally advises ignoring `Chmod` because macOS Spotlight and other software can generate many attribute events ([FAQ](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/README.md#L109-L117)).

Consequences for this project:

- Watch each unique parent directory, then compare the cleaned event path with only the current Config File and active Domain List paths.
- Do not infer content state from an operation flag. An exact-target event means “re-observe this path.” Accepting all exact-target operations is conservative; per-file debounce and raw-content dedup discard `Chmod` and duplicate noise.
- `ErrEventOverflow` means events were lost on Linux or Windows ([API source](https://github.com/fsnotify/fsnotify/blob/20b1e15ef3c70caeb37ea2bd184f48ef8382669e/fsnotify.go#L207-L215)); the safe recovery is to schedule re-observation of every current source, not to declare a config error.

### Viper: a small cross-platform parent-directory implementation

Viper's `WatchConfig` explicitly watches the entire directory “to pick up renames/atomic saves in a cross-platform way,” cleans the configured path, and only reloads when the event path matches with `Write` or `Create` (plus a symlink-retargeting case) ([source](https://github.com/spf13/viper/blob/528f7416c4b56a4948673984b190bf8713f0c3c4/viper.go#L282-L328)). A reload parse error is logged rather than made authoritative, while removal ends its watch loop ([source](https://github.com/spf13/viper/blob/528f7416c4b56a4948673984b190bf8713f0c3c4/viper.go#L322-L340)).

Viper demonstrates that no OS-specific abstraction is needed around `fsnotify`, but it does not debounce events, deduplicate contents, or robustly recover from target removal. It is useful evidence for the boundary and directory/filtering choice, not a complete reliability model for Live Configuration.

### Prometheus Operator and Thanos: settling delay plus raw SHA-256 dedup

Prometheus Operator deliberately adds the config file's parent directory to its watched directories because direct file watches can be broken by atomic writes ([source](https://github.com/prometheus-operator/prometheus-operator/blob/abea50da329d3575fb6736e7fe966546eb4dc968/pkg/operator/config_reloader.go#L301-L315)). Its reloader configuration defaults to a one-second delay after detecting a change, described as enough time for Kubernetes to update projected config files ([source](https://github.com/prometheus-operator/prometheus-operator/blob/abea50da329d3575fb6736e7fe966546eb4dc968/cmd/prometheus-config-reloader/main.go#L45-L70)).

The underlying Thanos reloader implements that delay as a trailing-edge debounce: each notification cancels the previous delay and starts another, then emits one reload signal after the quiet interval ([source](https://github.com/thanos-io/thanos/blob/0d7a3536be5385d8dec8d36b77ad5179c800348c/pkg/reloader/reloader.go#L819-L860)). It hashes config content with SHA-256 and returns without triggering reload when the hashes are unchanged ([source](https://github.com/thanos-io/thanos/blob/0d7a3536be5385d8dec8d36b77ad5179c800348c/pkg/reloader/reloader.go#L402-L419), [deduplication](https://github.com/thanos-io/thanos/blob/0d7a3536be5385d8dec8d36b77ad5179c800348c/pkg/reloader/reloader.go#L516-L540)). If reading, hashing, or applying fails, the outer watch loop logs the error and continues rather than terminating on the first failed observation ([source](https://github.com/thanos-io/thanos/blob/0d7a3536be5385d8dec8d36b77ad5179c800348c/pkg/reloader/reloader.go#L335-L357)).

This is direct precedent for the proposed event coalescing and raw-content layer. Prometheus also retains periodic rereads, but seamless-cors has explicitly chosen not to retain polling; that part is not adopted.

### Kubernetes API server: validate before publication and retry failed observations

Kubernetes' dynamic CA-file controller stores the last successfully read non-empty content, compares bytes before doing work, validates the new CA, and only then stores it and notifies listeners ([source](https://github.com/kubernetes/kubernetes/blob/e3531b8e831e14d53c04a5d439f4cd0a686882f0/staging/src/k8s.io/apiserver/pkg/server/dynamiccertificates/dynamic_cafile_content.go#L50-L64), [load path](https://github.com/kubernetes/kubernetes/blob/e3531b8e831e14d53c04a5d439f4cd0a686882f0/staging/src/k8s.io/apiserver/pkg/server/dynamiccertificates/dynamic_cafile_content.go#L103-L148)). Its single-item work queue exists specifically for error backoff/retry; failed reads or validation are requeued with rate limiting instead of replacing the cached value or stopping immediately ([source](https://github.com/kubernetes/kubernetes/blob/e3531b8e831e14d53c04a5d439f4cd0a686882f0/staging/src/k8s.io/apiserver/pkg/server/dynamiccertificates/dynamic_cafile_content.go#L223-L244)).

Kubernetes retains the previous valid value indefinitely, which conflicts with seamless-cors's established Fatal Config Error and Fatal Domain List Error semantics. The transferable principle is narrower: the first failed observation after a filesystem event is not reliable proof of a persistently invalid source.

## Recommendation for seamless-cors

Keep the mechanism inside Live Configuration and use one `fsnotify.Watcher`; no OS-specific adapter or file-watcher interface is needed.

For each current source path:

1. Watch its parent directory and accept only events whose cleaned path exactly equals that source path. Any portable operation on that exact path schedules observation; sibling and temporary-file events are ignored.
   Require an ordinary file and reject a symlink at load time. Native change observation assumes a local filesystem; NFS and SMB notification behavior is outside the supported contract.
2. Give each source its own trailing-edge 100 ms debounce. An event resets only that source's timer.
3. After the timer settles, read the source and compute its raw SHA-256 fingerprint. If the last successfully accepted fingerprint is identical, stop.
4. A changed raw fingerprint invalidates the parsed source. Parse and validate, rebuild the complete Live Configuration snapshot, then compare the parsed snapshot semantically. Update the remembered raw fingerprint after successful parsing even when the semantic snapshot is equal. Publish only a semantic change; a Domain List path-only change remains observable metadata and therefore publishes a new snapshot.
5. Do **not** make the first failed read or parse authoritative. Arm a per-source one-second invalid-confirmation timer. Any new exact-target event cancels it and returns to the normal 100 ms debounce. If no event arrives, reread once when the confirmation timer fires. A valid reread continues normally; the second failed observation produces the existing fatal error and cleanup behavior.
6. On `ErrEventOverflow`, schedule ordinary debounced observation for both current sources. A closed watcher or unrecoverable directory-watch failure is an observation-system failure, distinct from invalid config content.

The confirmation timer is the smallest adaptation that preserves both properties the repo requires: transient remove/create gaps and partial writes do not falsely stop the gateway, while a source that remains invalid after the writer has gone quiet still becomes fatal. It avoids an open-ended retry subsystem and does not retain stale configuration indefinitely.

The one-second confirmation interval is intentionally separate from the 100 ms event debounce. The debounce coalesces one save operation; confirmation establishes that an observed invalid state persisted. Both should be constants owned by Live Configuration and covered with deterministic timer-driven tests.
