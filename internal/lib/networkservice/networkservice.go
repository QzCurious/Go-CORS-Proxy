package networkservice

import (
	"context"
	"os/exec"
)

// PACSetting is one freshly observed PAC setting for a Network Service.
type PACSetting struct {
	URL     string
	Enabled bool
}

// Service exposes one current-user Network Service and its PAC operations.
type Service interface {
	Name() string
	PAC(context.Context) (PACSetting, error)
	SetPAC(context.Context, string) error
	DisablePAC(context.Context) error
}

var _ func(context.Context) ([]Service, error) = List

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
