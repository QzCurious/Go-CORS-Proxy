//go:build darwin

package networkservice

import (
	"context"
	"fmt"
	"strings"
)

type services struct {
	runner commandRunner
}

type service struct {
	owner *services
	name  string
}

var _ Service = (*service)(nil)

// List returns the visible Network Services without observing their PAC settings.
func List(ctx context.Context) ([]Service, error) {
	return (&services{runner: execRunner{}}).list(ctx)
}

func (s *services) list(ctx context.Context) ([]Service, error) {
	out, err := s.runner.run(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, fmt.Errorf("list network services: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	services := make([]Service, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line != "" {
			services = append(services, &service{owner: s, name: line})
		}
	}

	return services, nil
}

func (s *service) Name() string { return s.name }

// PAC returns this Network Service's fresh PAC setting.
func (s *service) PAC(ctx context.Context) (PACSetting, error) {
	// Read the operating system's current state.
	out, err := s.owner.runner.run(ctx, "networksetup", "-getautoproxyurl", s.name)
	if err != nil {
		return PACSetting{}, fmt.Errorf("get PAC setting for network service %q: %w", s.name, err)
	}

	// Translate networksetup's text format into the domain snapshot.
	var setting PACSetting
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "URL":
			if value != "(null)" {
				setting.URL = value
			}
		case "Enabled":
			setting.Enabled = strings.EqualFold(value, "Yes")
		}
	}
	return setting, nil
}

// SetPAC sets and enables url as this Network Service's PAC setting.
func (s *service) SetPAC(ctx context.Context, url string) error {
	_, err := s.owner.runner.run(ctx, "networksetup", "-setautoproxyurl", s.name, url)
	if err != nil {
		return fmt.Errorf("set PAC URL for network service %q: %w", s.name, err)
	}
	return nil
}

// DisablePAC disables PAC use for this Network Service without changing its retained URL.
func (s *service) DisablePAC(ctx context.Context) error {
	_, err := s.owner.runner.run(ctx, "networksetup", "-setautoproxystate", s.name, "off")
	if err != nil {
		return fmt.Errorf("disable PAC for network service %q: %w", s.name, err)
	}
	return nil
}
