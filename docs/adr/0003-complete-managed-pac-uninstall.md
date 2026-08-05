# Completely uninstall marker-owned Managed PAC state

`Uninstall` is Managed PAC's complete teardown barrier. It closes reconciliation admission, cancels and discards pending work, waits until the current writer can perform no later platform write, and then inspects current OS proxy settings.

Uninstall removes every setting carrying the seamless-cors Managed PAC Ownership Marker, regardless of enabled state, publication generation, or whether its exact value changed after installation. Ownership identity authorizes removal; an earlier settings snapshot does not. Foreign settings are never removed.

The operation verifies that no marker-owned setting remains before succeeding. Requests arriving after teardown begins are discarded until a later successful `Install` reopens reconciliation admission. Gateway clears Managed PAC Runtime State to absence only after successful uninstall, so no separate `Close` operation, stateful session, or external PAC mutex is needed.
