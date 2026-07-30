---
status: superseded by ADR-0017
---

# Admit a trusted runtime before serving

UserCA uninstall remains independent from Single-Flight Start: it is allowed when Gateway Runtime is absent or active without CA trust, and denied when an active runtime uses trusted HTTPS. To close the race where CA state changes after CA Ensure but before traffic starts, trusted Gateway Runtime is published as active and then loads the currently usable Installed User CA before serving; uninstall before publication is caught by validation, while uninstall after publication is denied. If another CA command already replaced the prepared identity with a different usable CA and matching local key, admission adopts that authority and refreshes the CA Ensure Result without requesting approval; unusable state fails admission. This avoids a cross-command start mutex at the cost of a brief active-but-not-serving admission phase that must be withdrawn on validation failure.
