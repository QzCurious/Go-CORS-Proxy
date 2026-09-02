# Automatic Managed PAC activation

This decision is superseded by [ADR-0012](./0012-dynamic-system-pac-lifecycle.md). There is no activation-scoped Managed PAC Service Set or terminal No Manageable PAC Services outcome.

Gateway Activation automatically fixes every successfully observed empty or marker-owned Network Service as its Managed PAC Service Set, while foreign and unobservable settings remain excluded and are rechecked before mutation. Because activation never displaces foreign PAC configuration, Managed PAC requires no user consent; this supersedes the consent and user-accepted service-set portion of ADR-0002, removes the PAC fingerprint/retry protocol, and keeps activation inside one continuous Single-Flight Start. Upstream List Creation Consent, nonfatal per-service Set failures, recovery on a later Gateway-requested Set, and No Manageable PAC Services behavior remain unchanged.
