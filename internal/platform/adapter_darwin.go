//go:build darwin

package platform

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func currentAdapter() Adapter {
	return NewDarwinAdapter()
}

type DarwinAdapter struct {
	runner       commandRunner
	certPath     string
	certSHA1     string
	keychainPath string
}

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func NewDarwinAdapter() *DarwinAdapter {
	return &DarwinAdapter{runner: execRunner{}}
}

func (a *DarwinAdapter) Capabilities() CapabilityReport {
	return CapabilityReport{
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		Supported:         true,
		PACManagement:     CapabilitySupported,
		CATrustManagement: CapabilitySupported,
		RuntimeCleanup:    CapabilitySupported,
	}
}

func (a *DarwinAdapter) ApplyPAC(url string, services []string) ([]PACServiceUpdate, error) {
	updates := make([]PACServiceUpdate, 0, len(services))
	for _, service := range services {
		out, err := a.networksetup("-setautoproxyurl", service, url)
		if err != nil && isMissingNetworkService(out, err) {
			updates = append(updates, PACServiceUpdate{ServiceName: service, Outcome: PACApplyOutcomeAbsent})
			continue
		}
		if err != nil {
			return updates, err
		}
		updates = append(updates, PACServiceUpdate{ServiceName: service, Outcome: PACApplyOutcomeApplied})
	}
	return updates, nil
}

func isMissingNetworkService(out []byte, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(message, "not a recognized network service") ||
		strings.Contains(message, "network service was not found")
}

func (a *DarwinAdapter) CurrentPACState() ([]PACServiceState, error) {
	services, err := a.listServices()
	if err != nil {
		return nil, err
	}
	states := make([]PACServiceState, 0, len(services))
	for _, service := range services {
		state, err := a.getAutoProxy(service)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (a *DarwinAdapter) ClearOwnedPAC() error {
	states, err := a.CurrentPACState()
	if err != nil {
		return err
	}
	var firstErr error
	for _, state := range states {
		if !state.Enabled || !IsManagedPACFootprint(state.URL) {
			continue
		}
		if _, err := a.networksetup("-setautoproxystate", state.Name, "off"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *DarwinAdapter) TrustCA(ctx context.Context, certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("CA certificate PEM is invalid")
	}
	sum := sha1.Sum(block.Bytes)
	a.certSHA1 = strings.ToUpper(hex.EncodeToString(sum[:]))
	dir, err := os.MkdirTemp("", "seamless-cors-ca-*")
	if err != nil {
		return err
	}
	a.certPath = filepath.Join(dir, "root-ca.pem")
	if err := os.WriteFile(a.certPath, certPEM, 0o600); err != nil {
		return err
	}
	keychain := a.keychain()
	_, err = a.securityContext(ctx, "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", keychain, a.certPath)
	if isTrustApprovalDenied(err) {
		return fmt.Errorf("%w: %w", ErrTrustApprovalDenied, err)
	}
	return err
}

func (a *DarwinAdapter) TrustedCAs() ([]CARecord, error) {
	return a.caFootprintsContext(context.Background())
}

func (a *DarwinAdapter) RemoveCAs(ctx context.Context, fingerprints []string) error {
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
		if _, err := a.securityContext(ctx, "delete-certificate", "-Z", fingerprint, a.keychain()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.certPath != "" {
		if err := os.RemoveAll(filepath.Dir(a.certPath)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *DarwinAdapter) listServices() ([]string, error) {
	out, err := a.networksetup("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var services []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func (a *DarwinAdapter) getAutoProxy(service string) (PACServiceState, error) {
	out, err := a.networksetup("-getautoproxyurl", service)
	if err != nil {
		return PACServiceState{}, err
	}
	state := PACServiceState{Name: service}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "URL":
			state.URL = value
		case "Enabled":
			state.Enabled = strings.EqualFold(value, "Yes")
		}
	}
	return state, nil
}

func (a *DarwinAdapter) caFootprintsContext(ctx context.Context) ([]CARecord, error) {
	out, err := a.securityContext(ctx, "find-certificate", "-a", "-c", installedCACommonName, "-p", "-Z", a.keychain())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not be found") ||
			strings.Contains(strings.ToLower(string(out)), "could not be found") {
			return nil, nil
		}
		return nil, err
	}
	return strictCAFootprintRecords(out), nil
}

func strictCAFootprintSHA1s(out []byte) []string {
	records := strictCAFootprintRecords(out)
	fingerprints := make([]string, 0, len(records))
	for _, record := range records {
		fingerprints = append(fingerprints, record.SHA1)
	}
	return fingerprints
}

func strictCAFootprintRecords(out []byte) []CARecord {
	var records []CARecord
	var currentSHA1 string
	var pemLines []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(trimmed, "SHA-1 hash: "); ok {
			if record, ok := caRecordFromFingerprint(currentSHA1, pemLines); ok {
				records = append(records, record)
			}
			currentSHA1 = strings.TrimSpace(value)
			pemLines = nil
		} else if currentSHA1 != "" {
			pemLines = append(pemLines, line)
		}
	}
	if record, ok := caRecordFromFingerprint(currentSHA1, pemLines); ok {
		records = append(records, record)
	}
	return records
}

func fingerprintMatchesStrictCA(fingerprint string, pemLines []string) bool {
	_, ok := caRecordFromFingerprint(fingerprint, pemLines)
	return ok
}

func caRecordFromFingerprint(fingerprint string, pemLines []string) (CARecord, bool) {
	if fingerprint == "" || len(pemLines) == 0 {
		return CARecord{}, false
	}
	certPEM := []byte(strings.Join(pemLines, "\n"))
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return CARecord{}, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CARecord{}, false
	}
	if !isStrictCAFootprint(cert) {
		return CARecord{}, false
	}
	return CARecord{SHA1: fingerprint, CertPEM: certPEM, NotAfter: cert.NotAfter}, true
}

func (a *DarwinAdapter) networksetup(args ...string) ([]byte, error) {
	out, err := a.runner.run(context.Background(), "networksetup", args...)
	if err != nil {
		return out, fmt.Errorf("networksetup %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (a *DarwinAdapter) security(args ...string) ([]byte, error) {
	return a.securityContext(context.Background(), args...)
}

func (a *DarwinAdapter) securityContext(ctx context.Context, args ...string) ([]byte, error) {
	out, err := a.runner.run(ctx, "security", args...)
	if err != nil {
		return out, fmt.Errorf("security %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}

func (a *DarwinAdapter) keychain() string {
	if a.keychainPath != "" {
		return a.keychainPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "login.keychain-db"
	}
	return filepath.Join(home, "Library", "Keychains", "login.keychain-db")
}

func isTrustApprovalDenied(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "authorization was canceled") ||
		strings.Contains(text, "authorization was cancelled") ||
		strings.Contains(text, "authorization has been denied") ||
		strings.Contains(text, "user canceled") ||
		strings.Contains(text, "user cancelled")
}
