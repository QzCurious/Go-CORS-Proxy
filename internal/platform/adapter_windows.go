//go:build windows

package platform

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	windowsPACServiceName      = "Windows Current User"
	windowsInternetSettingsKey = `HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

func currentAdapter() Adapter {
	return NewWindowsAdapter()
}

type WindowsAdapter struct {
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

func NewWindowsAdapter() *WindowsAdapter {
	return &WindowsAdapter{runner: execRunner{}, notify: notifyInternetSettingsChanged}
}

func (a *WindowsAdapter) Capabilities() CapabilityReport {
	return CapabilityReport{
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		Supported:         true,
		PACManagement:     CapabilitySupported,
		CATrustManagement: CapabilitySupported,
		RuntimeCleanup:    CapabilitySupported,
	}
}

func (a *WindowsAdapter) ApplyPAC(url string, services []string) ([]PACServiceUpdate, error) {
	if !containsString(services, windowsPACServiceName) {
		return absentPACUpdates(services), nil
	}
	script := fmt.Sprintf(`
$key = %s
New-Item -Path $key -Force | Out-Null
New-ItemProperty -Path $key -Name AutoConfigURL -PropertyType String -Value %s -Force | Out-Null
`, psQuote(windowsInternetSettingsKey), psQuote(url))
	if _, err := a.powershell(script); err != nil {
		return nil, err
	}
	if err := a.notifyInternetSettingsChanged(); err != nil {
		return nil, err
	}
	updates := make([]PACServiceUpdate, 0, len(services))
	for _, service := range services {
		outcome := PACApplyOutcomeAbsent
		if service == windowsPACServiceName {
			outcome = PACApplyOutcomeApplied
		}
		updates = append(updates, PACServiceUpdate{ServiceName: service, Outcome: outcome})
	}
	return updates, nil
}

func absentPACUpdates(services []string) []PACServiceUpdate {
	updates := make([]PACServiceUpdate, 0, len(services))
	for _, service := range services {
		updates = append(updates, PACServiceUpdate{ServiceName: service, Outcome: PACApplyOutcomeAbsent})
	}
	return updates
}

func (a *WindowsAdapter) CurrentPACState() ([]PACServiceState, error) {
	script := fmt.Sprintf(`
$key = %s
$value = ''
$prop = Get-ItemProperty -Path $key -Name AutoConfigURL -ErrorAction SilentlyContinue
if ($null -ne $prop -and $null -ne $prop.AutoConfigURL) {
	$value = [string]$prop.AutoConfigURL
}
[pscustomobject]@{
	Name = %s
	URL = $value
	Enabled = ($value.Length -gt 0)
} | ConvertTo-Json -Compress
`, psQuote(windowsInternetSettingsKey), psQuote(windowsPACServiceName))
	out, err := a.powershell(script)
	if err != nil {
		return nil, err
	}
	var state PACServiceState
	if err := json.Unmarshal(bytes.TrimSpace(out), &state); err != nil {
		return nil, fmt.Errorf("parse Windows PAC state: %w", err)
	}
	return []PACServiceState{state}, nil
}

func (a *WindowsAdapter) ClearPACIfMatches(expected []PACServiceState) error {
	states, err := a.CurrentPACState()
	if err != nil {
		return err
	}
	if len(states) != 1 {
		return nil
	}
	matched := false
	for _, state := range expected {
		if state == states[0] {
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
	if _, err := a.powershell(script); err != nil {
		return err
	}
	return a.notifyInternetSettingsChanged()
}

func (a *WindowsAdapter) TrustCA(ctx context.Context, certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("CA certificate PEM is invalid")
	}
	dir, err := os.MkdirTemp("", "seamless-cors-ca-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	certPath := filepath.Join(dir, "root-ca.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return err
	}
	script := fmt.Sprintf(`
Import-Certificate -FilePath %s -CertStoreLocation Cert:\CurrentUser\Root | Out-Null
`, psQuote(certPath))
	_, err = a.powershellContext(ctx, script)
	return err
}

func (a *WindowsAdapter) TrustedCAs() ([]CARecord, error) {
	return a.caFootprintsContext(context.Background())
}

func (a *WindowsAdapter) RemoveCAs(ctx context.Context, fingerprints []string) error {
	if len(fingerprints) == 0 {
		records, err := a.caFootprintsContext(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			fingerprints = append(fingerprints, record.SHA1)
		}
	}
	var firstErr error
	for _, fingerprint := range fingerprints {
		script := fmt.Sprintf(`
Remove-Item -Path %s -ErrorAction SilentlyContinue
`, psQuote(`Cert:\CurrentUser\Root\`+fingerprint))
		if _, err := a.powershellContext(ctx, script); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *WindowsAdapter) caFootprintsContext(ctx context.Context) ([]CARecord, error) {
	script := fmt.Sprintf(`
$records = @(
	Get-ChildItem -Path Cert:\CurrentUser\Root |
		Where-Object { $_.Subject -eq %s } |
		ForEach-Object {
			[pscustomobject]@{
				SHA1 = $_.Thumbprint.ToUpperInvariant()
				DER = [Convert]::ToBase64String($_.RawData)
				NotAfter = $_.NotAfter.ToUniversalTime().ToString('o')
			}
		}
)
ConvertTo-Json -Compress -InputObject $records
`, psQuote("CN="+installedCACommonName))
	out, err := a.powershellContext(ctx, script)
	if err != nil {
		return nil, err
	}
	return windowsCARecordsFromJSON(out)
}

func (a *WindowsAdapter) powershell(script string) ([]byte, error) {
	return a.powershellContext(context.Background(), script)
}

func (a *WindowsAdapter) powershellContext(ctx context.Context, script string) ([]byte, error) {
	out, err := a.runner.run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		return out, fmt.Errorf("powershell failed: %s: %w", bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (a *WindowsAdapter) notifyInternetSettingsChanged() error {
	if a.notify == nil {
		return nil
	}
	return a.notify()
}

func windowsCARecordsFromJSON(out []byte) ([]CARecord, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	type rawCARecord struct {
		SHA1     string
		DER      string
		NotAfter string
	}
	var raw []rawCARecord
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var single rawCARecord
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, fmt.Errorf("parse Windows CA record: %w", err)
		}
		raw = []rawCARecord{single}
	} else if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("parse Windows CA records: %w", err)
	}
	records := make([]CARecord, 0, len(raw))
	for _, item := range raw {
		der, err := base64.StdEncoding.DecodeString(item.DER)
		if err != nil {
			return nil, fmt.Errorf("decode Windows CA certificate: %w", err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parse Windows CA certificate: %w", err)
		}
		if !isStrictCAFootprint(cert) {
			continue
		}
		notAfter, err := itemNotAfter(item.NotAfter, cert.NotAfter)
		if err != nil {
			return nil, err
		}
		sum := sha1.Sum(der)
		sha1Hex := strings.ToUpper(hex.EncodeToString(sum[:]))
		if item.SHA1 != "" {
			sha1Hex = strings.ToUpper(strings.ReplaceAll(item.SHA1, " ", ""))
		}
		records = append(records, CARecord{
			SHA1:     sha1Hex,
			CertPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			NotAfter: notAfter,
		})
	}
	return records, nil
}

func itemNotAfter(raw string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Windows CA expiration: %w", err)
	}
	return value, nil
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
