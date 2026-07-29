# Do not rotate CA in an active runtime

An active Gateway Runtime using Trusted HTTPS Interception permits idempotent UserCA install but blocks CA replacement and uninstall until the runtime stops; an absent runtime or one without active interception permits those mutations. Hot rotation would require overlapping trusted identities, atomic signer replacement, CA-generation-aware leaf-certificate caching, and crash recovery, which is not justified for this local DEV/QA lifecycle. Trusted Runtime Admission may still adopt a different usable CA before traffic begins because no connections or cached leaves exist yet.

Enabling `ca-trusted` in Live Configuration may hot-activate interception by adopting the one already-usable Installed User CA; this is admission from an untrusted runtime state, not rotation of an active authority. Live activation never installs or repairs UserCA and instead leaves interception inactive with a warning when no usable authority is available.
