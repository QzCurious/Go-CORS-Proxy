# Stop preempts an in-progress start

`stop` cancels and supersedes an in-progress start instead of waiting on a global mutation mutex or returning busy. Start phases observe lifecycle cancellation and cannot publish a new runtime or begin Managed PAC installation after cancellation.

Stop closes traffic, invokes Complete Managed PAC Uninstall, removes discovery state, and shuts down the router. Uninstall closes reconciliation admission, discards queued PAC work, preempts an in-progress PAC operation, waits for its writer to become quiescent, and then disables all active marker-owned PAC settings. Independently admitted UserCA work is not cancelled by stop; owner shutdown waits for it before releasing Gateway Ownership.
