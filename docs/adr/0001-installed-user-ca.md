# Installed User CA

The gateway keeps one long-lived seamless-cors-owned development CA in the current user's operating-system trust store instead of generating a new CA for each trusted gateway run.

This avoids repeated platform trust approval during normal `start` and `stop` cycles. The trade-off is that seamless-cors stores local CA material, including the signing key, as current-user-readable product state protected by file permissions rather than encrypting it at rest.

Encrypting the key would either reintroduce unlock prompts or require a local secret-store dependency, which would undermine the low-friction local development flow this CA is meant to support.
