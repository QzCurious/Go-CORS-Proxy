// Package managedpac classifies current-user PAC settings and exposes separate
// activation/control and ownerless-footprint capabilities backed by one
// implementation. It owns fixed-set control lifetimes, serialized PAC
// delivery, purpose-built reports, and complete marker-owned cleanup while
// driving platform PAC mechanics through networkservice.
package managedpac
