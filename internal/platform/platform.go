package platform

import (
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
)

var ErrTrustApprovalDenied = errors.New("certificate trust approval denied")

const (
	PACFootprintFileName  = "seamless-cors.pac"
	installedCACommonName = "seamless-cors Installed User CA"
)

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityLimited     CapabilityStatus = "limited"
	CapabilityUnknown     CapabilityStatus = "unknown"
)

type CapabilityReport struct {
	Platform          string
	Supported         bool
	PACManagement     CapabilityStatus
	CATrustManagement CapabilityStatus
	RuntimeCleanup    CapabilityStatus
}

type CARecord struct {
	SHA1     string
	CertPEM  []byte
	NotAfter time.Time
}

type PACServiceState struct {
	Name    string
	URL     string
	Enabled bool
}

type Adapter interface {
	Capabilities() CapabilityReport
	InstallPAC(url string, services []string) ([]string, error)
	RefreshPAC(url string, services []string) error
	CurrentPACState() ([]PACServiceState, error)
	ClearOwnedPAC() error
	TrustedCAs() ([]CARecord, error)
	TrustCA(certPEM []byte) error
	RemoveCAs(fingerprints []string) error
}

var CurrentAdapter Adapter = currentAdapter()

func IsManagedPACFootprint(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return path.Base(u.EscapedPath()) == PACFootprintFileName
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && path.Base(u.EscapedPath()) == PACFootprintFileName
}

func HasOwnedPACState(states []PACServiceState) bool {
	for _, state := range states {
		if state.Enabled && IsManagedPACFootprint(state.URL) {
			return true
		}
	}
	return false
}

func HasForeignEnabledPACState(states []PACServiceState) bool {
	for _, state := range states {
		if state.Enabled && state.URL != "" && state.URL != "(null)" && !IsManagedPACFootprint(state.URL) {
			return true
		}
	}
	return false
}

func isStrictCAFootprint(cert *x509.Certificate) bool {
	if cert.Subject.CommonName != installedCACommonName {
		return false
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		return false
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 || cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return false
	}
	return cert.CheckSignatureFrom(cert) == nil
}
