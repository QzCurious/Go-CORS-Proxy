//go:build darwin

package pacsettings

import (
	"context"
	"fmt"
	"strings"
)

type darwinSettings struct {
	runner commandRunner
}

var _ platformSettings = (*darwinSettings)(nil)

func newPlatformSettings() platformSettings {
	return &darwinSettings{runner: execRunner{}}
}

func (s *darwinSettings) list(ctx context.Context) ([]string, error) {
	out, err := s.runner.run(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, fmt.Errorf("list network services: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	serviceNames := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line != "" {
			serviceNames = append(serviceNames, line)
		}
	}

	return serviceNames, nil
}

func (s *darwinSettings) lookup(ctx context.Context, serviceName string) (Setting, error) {
	// Read the operating system's current state.
	out, err := s.runner.run(ctx, "networksetup", "-getautoproxyurl", serviceName)
	if err != nil {
		return Setting{}, fmt.Errorf("get PAC setting for network service %q: %w", serviceName, err)
	}

	// Translate networksetup's text format into the domain snapshot.
	var setting Setting
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

func (s *darwinSettings) setURL(ctx context.Context, serviceName, url string) error {
	_, err := s.runner.run(ctx, "networksetup", "-setautoproxyurl", serviceName, url)
	if err != nil {
		return fmt.Errorf("set PAC URL for network service %q: %w", serviceName, err)
	}
	return nil
}

func (s *darwinSettings) disable(ctx context.Context, serviceName string) error {
	_, err := s.runner.run(ctx, "networksetup", "-setautoproxystate", serviceName, "off")
	if err != nil {
		return fmt.Errorf("disable PAC for network service %q: %w", serviceName, err)
	}
	return nil
}
