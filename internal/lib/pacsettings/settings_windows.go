//go:build windows

package pacsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
)

const (
	windowsPACServiceName      = "Windows Current User"
	windowsInternetSettingsKey = `HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// Settings accesses the current user's operating-system PAC settings.
type Settings struct {
	runner commandRunner
	notify func() error
}

// New returns the current user's PAC settings without inspecting or mutating them.
func New() *Settings {
	return &Settings{runner: execRunner{}, notify: notifyInternetSettingsChanged}
}

// List returns a fresh snapshot of visible PAC settings.
func (s *Settings) List(ctx context.Context) ([]Setting, error) {
	setting, err := s.get(ctx)
	if err != nil {
		return nil, err
	}
	return []Setting{setting}, nil
}

// SetURL sets and enables url only when the setting still matches observed.
func (s *Settings) SetURL(ctx context.Context, observed Setting, url string) (MutationResult, error) {
	if observed.ServiceName != windowsPACServiceName {
		return MutationResult{}, nil
	}
	current, err := s.get(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if current != observed {
		return changedMutation(current), nil
	}
	script := fmt.Sprintf(`
$key = %s
New-Item -Path $key -Force | Out-Null
New-ItemProperty -Path $key -Name AutoConfigURL -PropertyType String -Value %s -Force | Out-Null
`, psQuote(windowsInternetSettingsKey), psQuote(url))
	if _, err := s.powershell(ctx, script); err != nil {
		return MutationResult{}, err
	}
	if err := s.notifyInternetSettingsChanged(); err != nil {
		return MutationResult{Applied: true}, err
	}
	return MutationResult{Applied: true}, nil
}

// Disable disables PAC use only when the setting still matches observed.
func (s *Settings) Disable(ctx context.Context, observed Setting) (MutationResult, error) {
	if observed.ServiceName != windowsPACServiceName {
		return MutationResult{}, nil
	}
	current, err := s.get(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	if current != observed {
		return changedMutation(current), nil
	}
	script := fmt.Sprintf(`
$key = %s
Remove-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
`, psQuote(windowsInternetSettingsKey))
	if _, err := s.powershell(ctx, script); err != nil {
		return MutationResult{}, err
	}
	if err := s.notifyInternetSettingsChanged(); err != nil {
		return MutationResult{Applied: true}, err
	}
	return MutationResult{Applied: true}, nil
}

func (s *Settings) get(ctx context.Context) (Setting, error) {
	script := fmt.Sprintf(`
$key = %s
$value = ''
$prop = Get-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
if ($null -ne $prop -and $null -ne $prop.AutoConfigURL) {
	$value = [string]$prop.AutoConfigURL
}
[pscustomobject]@{
	ServiceName = %s
	URL = $value
	Enabled = ($value.Length -gt 0)
} | ConvertTo-Json -Compress
`, psQuote(windowsInternetSettingsKey), psQuote(windowsPACServiceName))
	out, err := s.powershell(ctx, script)
	if err != nil {
		return Setting{}, err
	}
	var setting Setting
	if err := json.Unmarshal(bytes.TrimSpace(out), &setting); err != nil {
		return Setting{}, fmt.Errorf("parse Windows PAC state: %w", err)
	}
	return setting, nil
}

func (s *Settings) powershell(ctx context.Context, script string) ([]byte, error) {
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (s *Settings) notifyInternetSettingsChanged() error {
	if s.notify == nil {
		return nil
	}
	return s.notify()
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
