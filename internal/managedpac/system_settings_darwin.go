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

func newSystemSettings() SystemSettings {
	return &darwinSystemSettings{runner: execRunner{}}
}

var _ SystemSettings = (*darwinSystemSettings)(nil)

func (s *darwinSystemSettings) Apply(ctx context.Context, pacURL string, serviceNames []string) (ApplyResult, error) {
	result := ApplyResult{AppliedServices: make([]string, 0, len(serviceNames))}
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

func isMissingNetworkService(out []byte, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(message, "not a recognized network service") ||
		strings.Contains(message, "network service was not found")
}

func (s *darwinSystemSettings) Snapshot(ctx context.Context) ([]ServiceSnapshot, error) {
	serviceNames, err := s.listServices(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]ServiceSnapshot, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		snapshot, err := s.getAutoProxy(ctx, serviceName)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (s *darwinSystemSettings) ClearIfUnchanged(ctx context.Context, expected []ServiceSnapshot) error {
	currentSnapshots, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	current := make(map[string]ServiceSnapshot, len(currentSnapshots))
	for _, snapshot := range currentSnapshots {
		current[snapshot.ServiceName] = snapshot
	}
	var firstErr error
	for _, snapshot := range expected {
		if actual, ok := current[snapshot.ServiceName]; !ok || actual != snapshot {
			continue
		}
		if _, err := s.networksetup(ctx, "-setautoproxystate", snapshot.ServiceName, "off"); err != nil && firstErr == nil {
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
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		serviceNames = append(serviceNames, line)
	}
	return serviceNames, nil
}

func (s *darwinSystemSettings) getAutoProxy(ctx context.Context, serviceName string) (ServiceSnapshot, error) {
	out, err := s.networksetup(ctx, "-getautoproxyurl", serviceName)
	if err != nil {
		return ServiceSnapshot{}, err
	}
	snapshot := ServiceSnapshot{ServiceName: serviceName}
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
