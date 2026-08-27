package pacsettings

import (
	"context"
	"os/exec"
)

// Settings accesses the current user's operating-system PAC settings.
type Settings struct {
	platform platformSettings
}

// New returns the current user's PAC settings without inspecting or mutating them.
func New() *Settings {
	return &Settings{platform: newPlatformSettings()}
}

// Setting is one freshly observed operating-system PAC setting.
type Setting struct {
	URL     string
	Enabled bool
}

// List returns the visible network service names.
func (s *Settings) List(ctx context.Context) ([]string, error) {
	return s.platform.list(ctx)
}

// Lookup returns a fresh PAC setting for serviceName.
func (s *Settings) Lookup(ctx context.Context, serviceName string) (Setting, error) {
	return s.platform.lookup(ctx, serviceName)
}

// SetURL sets and enables url for serviceName.
func (s *Settings) SetURL(ctx context.Context, serviceName, url string) error {
	return s.platform.setURL(ctx, serviceName, url)
}

// Disable disables PAC use for serviceName without changing its retained URL.
func (s *Settings) Disable(ctx context.Context, serviceName string) error {
	return s.platform.disable(ctx, serviceName)
}

type platformSettings interface {
	list(context.Context) ([]string, error)
	lookup(context.Context, string) (Setting, error)
	setURL(context.Context, string, string) error
	disable(context.Context, string) error
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
