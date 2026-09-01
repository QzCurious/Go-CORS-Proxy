//go:build windows

package networkservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
)

const (
	windowsServiceName         = "Windows Current User"
	windowsInternetSettingsKey = `HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

type services struct {
	runner commandRunner
	notify func() error
}

type service struct {
	owner *services
	name  string
}

var _ Service = (*service)(nil)

// List returns the current-user Network Service without observing its PAC setting.
func List(ctx context.Context) ([]Service, error) {
	return (&services{runner: execRunner{}, notify: notifyInternetSettingsChanged}).list(ctx)
}

func (s *services) list(context.Context) ([]Service, error) {
	return []Service{&service{owner: s, name: windowsServiceName}}, nil
}

func (s *service) Name() string { return s.name }

// PAC returns this Network Service's fresh PAC setting.
func (s *service) PAC(ctx context.Context) (PACSetting, error) {
	// Read the operating system's current state as JSON.
	script := fmt.Sprintf(`
$key = %s
$value = ''
$prop = Get-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
if ($null -ne $prop -and $null -ne $prop.AutoConfigURL) {
	$value = [string]$prop.AutoConfigURL
}
[pscustomobject]@{
	URL = $value
	Enabled = ($value.Length -gt 0)
} | ConvertTo-Json -Compress
`, psQuote(windowsInternetSettingsKey))
	out, err := s.owner.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return PACSetting{}, fmt.Errorf("get PAC setting for network service %q: %w", s.name, err)
	}

	// Translate PowerShell's JSON into the domain snapshot.
	var setting PACSetting
	if err := json.Unmarshal(bytes.TrimSpace(out), &setting); err != nil {
		return PACSetting{}, fmt.Errorf("parse PAC setting for network service %q: %w", s.name, err)
	}
	return setting, nil
}

// SetPAC sets and enables url as this Network Service's PAC setting.
func (s *service) SetPAC(ctx context.Context, url string) error {
	script := fmt.Sprintf(`
$key = %s
New-Item -Path $key -Force | Out-Null
New-ItemProperty -Path $key -Name AutoConfigURL -PropertyType String -Value %s -Force | Out-Null
`, psQuote(windowsInternetSettingsKey), psQuote(url))
	_, err := s.owner.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return fmt.Errorf("set PAC URL for network service %q: %w", s.name, err)
	}
	return s.owner.notifyInternetSettingsChanged()
}

// DisablePAC disables PAC use for this Network Service.
func (s *service) DisablePAC(ctx context.Context) error {
	script := fmt.Sprintf(`
$key = %s
Remove-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
`, psQuote(windowsInternetSettingsKey))
	_, err := s.owner.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return fmt.Errorf("disable PAC for network service %q: %w", s.name, err)
	}
	return s.owner.notifyInternetSettingsChanged()
}

func (s *services) notifyInternetSettingsChanged() error {
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
