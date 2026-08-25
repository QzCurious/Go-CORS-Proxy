//go:build darwin

package pacsettings

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// Settings accesses the current user's operating-system PAC settings.
type Settings struct {
	runner commandRunner
}

// New returns the current user's PAC settings without inspecting or mutating them.
func New() *Settings {
	return &Settings{runner: execRunner{}}
}

// List returns a fresh snapshot of visible PAC settings.
func (s *Settings) List(ctx context.Context) ([]Setting, error) {
	serviceNames, err := s.listServices(ctx)
	if err != nil {
		return nil, err
	}
	settings := make([]Setting, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		setting, available, err := s.get(ctx, serviceName)
		if err != nil {
			return nil, err
		}
		if available {
			settings = append(settings, setting)
		}
	}
	return settings, nil
}

// SetURL sets and enables url only when the setting still matches observed.
func (s *Settings) SetURL(ctx context.Context, observed Setting, url string) (MutationResult, error) {
	current, available, err := s.get(ctx, observed.ServiceName)
	if err != nil {
		return MutationResult{}, err
	}
	if !available {
		return MutationResult{}, nil
	}
	if current != observed {
		return changedMutation(current), nil
	}
	out, err := s.networksetup(ctx, "-setautoproxyurl", observed.ServiceName, url)
	if err != nil && isMissingNetworkService(out, err) {
		return MutationResult{}, nil
	}
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Applied: true}, nil
}

// Disable disables PAC use only when the setting still matches observed.
func (s *Settings) Disable(ctx context.Context, observed Setting) (MutationResult, error) {
	current, available, err := s.get(ctx, observed.ServiceName)
	if err != nil {
		return MutationResult{}, err
	}
	if !available {
		return MutationResult{}, nil
	}
	if current != observed {
		return changedMutation(current), nil
	}
	out, err := s.networksetup(ctx, "-setautoproxystate", observed.ServiceName, "off")
	if err != nil && isMissingNetworkService(out, err) {
		return MutationResult{}, nil
	}
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Applied: true}, nil
}

func (s *Settings) listServices(ctx context.Context) ([]string, error) {
	out, err := s.networksetup(ctx, "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var serviceNames []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line != "" {
			serviceNames = append(serviceNames, line)
		}
	}
	return serviceNames, nil
}

func (s *Settings) get(ctx context.Context, serviceName string) (Setting, bool, error) {
	out, err := s.networksetup(ctx, "-getautoproxyurl", serviceName)
	if err != nil && isMissingNetworkService(out, err) {
		return Setting{}, false, nil
	}
	if err != nil {
		return Setting{}, false, err
	}
	setting := Setting{ServiceName: serviceName}
	for _, line := range strings.Split(string(out), "\n") {
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
	return setting, true, nil
}

func (s *Settings) networksetup(ctx context.Context, args ...string) ([]byte, error) {
	out, err := s.runner.run(ctx, "networksetup", args...)
	if err != nil {
		return out, fmt.Errorf("networksetup %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}

func isMissingNetworkService(out []byte, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(message, "not a recognized network service") ||
		strings.Contains(message, "network service was not found")
}
