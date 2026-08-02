//go:build darwin

package managedpac

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type darwinSystemSettings struct {
	runner commandRunner
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func newSystemSettings() systemSettings {
	return &darwinSystemSettings{runner: execRunner{}}
}

var _ systemSettings = (*darwinSystemSettings)(nil)

func (s *darwinSystemSettings) Apply(ctx context.Context, pacURL string, serviceNames []string) (applyResult, error) {
	result := applyResult{AppliedServices: make([]string, 0, len(serviceNames))}
	for _, serviceName := range serviceNames {
		out, err := s.networksetup(ctx, "-setautoproxyurl", serviceName, pacURL)
		if err != nil && isMissingNetworkService(out, err) {
			continue
		}
		if err != nil {
			return result, err
		}
		result.AppliedServices = append(result.AppliedServices, serviceName)
	}
	return result, nil
}

func (s *darwinSystemSettings) Snapshot(ctx context.Context) ([]serviceSnapshot, error) {
	serviceNames, err := s.listServices(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]serviceSnapshot, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		snapshot, err := s.getAutoProxy(ctx, serviceName)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (s *darwinSystemSettings) ClearOwned(ctx context.Context, serviceNames []string) error {
	var firstErr error
	for _, serviceName := range serviceNames {
		current, err := s.getAutoProxy(ctx, serviceName)
		if err != nil && isMissingNetworkService(nil, err) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !isOwnedURL(current.PACURL) {
			continue
		}
		if _, err := s.networksetup(ctx, "-setautoproxyurl", serviceName, ""); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if _, err := s.networksetup(ctx, "-setautoproxystate", serviceName, "off"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *darwinSystemSettings) listServices(ctx context.Context) ([]string, error) {
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
		if line == "" {
			continue
		}
		serviceNames = append(serviceNames, line)
	}
	return serviceNames, nil
}

func (s *darwinSystemSettings) getAutoProxy(ctx context.Context, serviceName string) (serviceSnapshot, error) {
	out, err := s.networksetup(ctx, "-getautoproxyurl", serviceName)
	if err != nil {
		return serviceSnapshot{}, err
	}
	snapshot := serviceSnapshot{ServiceName: serviceName}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "URL":
			snapshot.PACURL = value
		case "Enabled":
			snapshot.Enabled = strings.EqualFold(value, "Yes")
		}
	}
	return snapshot, nil
}

func (s *darwinSystemSettings) networksetup(ctx context.Context, args ...string) ([]byte, error) {
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
