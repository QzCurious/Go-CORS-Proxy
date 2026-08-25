package pacsettings

import (
	"context"
	"os/exec"
)

// Setting is one freshly observed operating-system PAC setting. Callers must
// treat values returned by List and MutationResult as read-only facts.
type Setting struct {
	ServiceName string
	URL         string
	Enabled     bool
}

// MutationResult reports a conditional mutation outcome. When Applied is false,
// Current is the conflicting setting or nil when the service was unavailable.
type MutationResult struct {
	Applied bool
	Current *Setting
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func changedMutation(current Setting) MutationResult {
	return MutationResult{Current: &current}
}
