package pacsettings

import (
	"context"
	"os/exec"
)

// Setting is one freshly observed operating-system PAC setting.
type Setting struct {
	URL     string
	Enabled bool
}

// Settings exposes current-user operating-system PAC settings as
// platform-neutral facts and mutations.
type Settings interface {
	List(context.Context) ([]string, error)
	Lookup(context.Context, string) (Setting, error)
	SetURL(context.Context, string, string) error
	Disable(context.Context, string) error
}

var _ func() Settings = New

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
