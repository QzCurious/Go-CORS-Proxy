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

type windowsSettings struct {
	runner commandRunner
	notify func() error
}

var _ platformSettings = (*windowsSettings)(nil)

func newPlatformSettings() platformSettings {
	return &windowsSettings{runner: execRunner{}, notify: notifyInternetSettingsChanged}
}

func (s *windowsSettings) list(context.Context) ([]string, error) {
	return []string{windowsPACServiceName}, nil
}

func (s *windowsSettings) lookup(ctx context.Context, serviceName string) (Setting, error) {
	if serviceName != windowsPACServiceName {
		return Setting{}, fmt.Errorf("get PAC setting for network service %q: unknown network service", serviceName)
	}

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
	out, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return Setting{}, fmt.Errorf("get PAC setting for network service %q: %w", serviceName, err)
	}

	// Translate PowerShell's JSON into the domain snapshot.
	var setting Setting
	if err := json.Unmarshal(bytes.TrimSpace(out), &setting); err != nil {
		return Setting{}, fmt.Errorf("parse PAC setting for network service %q: %w", serviceName, err)
	}
	return setting, nil
}

func (s *windowsSettings) setURL(ctx context.Context, serviceName, url string) error {
	if serviceName != windowsPACServiceName {
		return fmt.Errorf("set PAC URL for network service %q: unknown network service", serviceName)
	}
	script := fmt.Sprintf(`
$key = %s
New-Item -Path $key -Force | Out-Null
New-ItemProperty -Path $key -Name AutoConfigURL -PropertyType String -Value %s -Force | Out-Null
`, psQuote(windowsInternetSettingsKey), psQuote(url))
	_, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return fmt.Errorf("set PAC URL for network service %q: %w", serviceName, err)
	}
	return s.notifyInternetSettingsChanged()
}

func (s *windowsSettings) disable(ctx context.Context, serviceName string) error {
	if serviceName != windowsPACServiceName {
		return fmt.Errorf("disable PAC for network service %q: unknown network service", serviceName)
	}
	script := fmt.Sprintf(`
$key = %s
Remove-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
`, psQuote(windowsInternetSettingsKey))
	_, err := s.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return fmt.Errorf("disable PAC for network service %q: %w", serviceName, err)
	}
	return s.notifyInternetSettingsChanged()
}

func (s *windowsSettings) notifyInternetSettingsChanged() error {
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
