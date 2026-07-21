// Package gateway owns the complete gateway lifecycle behind operation-specific
// commands. Callers do not need to know whether an operation is executed in the
// current process or forwarded to an existing foreground owner.
//
// The implementation is organized by responsibility inside this package:
// discovery owns the state-cache lease, transport provides authenticated local
// HTTP, foreground supervises process lifetime, lifecycle coordinates commands,
// the start sequence governs activation, and traffic serves PAC and proxy
// requests. Those are implementation details rather than caller-visible seams.
package gateway
