//go:build windows

package managedpac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

const (
	windowsPACServiceName      = "Windows Current User"
	windowsInternetSettingsKey = `HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

type windowsSystemSettings struct {
	runner commandRunner
	notify func() error
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func newSystemSettings() SystemSettings {
	return &windowsSystemSettings{runner: execRunner{}, notify: notifyInternetSettingsChanged}
}

var _ SystemSettings = (*windowsSystemSettings)(nil)

func (s *windowsSystemSettings) Apply(ctx context.Context, pacURL string, serviceNames []string) (ApplyResult, error) {
	if !containsString(serviceNames, windowsPACServiceName) {
		return ApplyResult{}, nil
	}
	script := fmt.Sprintf(`
$key = %s
New-Item -Path $key -Force | Out-Null
New-ItemProperty -Path $key -Name AutoConfigURL -PropertyType String -Value %s -Force | Out-Null
`, psQuote(windowsInternetSettingsKey), psQuote(pacURL))
	if _, err := s.powershell(ctx, script); err != nil {
		return ApplyResult{}, err
	}
	if err := s.notifyInternetSettingsChanged(); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{AppliedServices: []string{windowsPACServiceName}}, nil
}

func (s *windowsSystemSettings) Snapshot(ctx context.Context) ([]ServiceSnapshot, error) {
	script := fmt.Sprintf(`
$key = %s
$value = ''
$prop = Get-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
if ($null -ne $prop -and $null -ne $prop.AutoConfigURL) {
	$value = [string]$prop.AutoConfigURL
}
[pscustomobject]@{
	ServiceName = %s
	PACURL = $value
	Enabled = ($value.Length -gt 0)
} | ConvertTo-Json -Compress
`, psQuote(windowsInternetSettingsKey), psQuote(windowsPACServiceName))
	out, err := s.powershell(ctx, script)
	if err != nil {
		return nil, err
	}
	var snapshot ServiceSnapshot
	if err := json.Unmarshal(bytes.TrimSpace(out), &snapshot); err != nil {
		return nil, fmt.Errorf("parse Windows PAC state: %w", err)
	}
	return []ServiceSnapshot{snapshot}, nil
}

func (s *windowsSystemSettings) ClearIfUnchanged(ctx context.Context, expected []ServiceSnapshot) error {
	snapshots, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	if len(snapshots) != 1 {
		return nil
	}
	matched := false
	for _, snapshot := range expected {
		if snapshot == snapshots[0] {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	script := fmt.Sprintf(`
$key = %s
Remove-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
`, psQuote(windowsInternetSettingsKey))
	if _, err := s.powershell(ctx, script); err != nil {
		return err
	}
	return s.notifyInternetSettingsChanged()
}

func (s *windowsSystemSettings) powershell(ctx context.Context, script string) ([]byte, error) {
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (s *windowsSystemSettings) notifyInternetSettingsChanged() error {
	if s.notify == nil {
		return nil
	}
	return s.notify()
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func notifyInternetSettingsChanged() error {
	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	for _, option := range []uintptr{39, 37} {
		ret, _, err := internetSetOption.Call(0, option, 0, 0)
		if ret == 0 && err != syscall.Errno(0) {
			return err
		}
	}
	return nil
}
