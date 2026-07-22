package platform

import (
	"context"
	"crypto/x509"
	"errors"
	"time"
)

var ErrTrustApprovalDenied = errors.New("certificate trust approval denied")

const (
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

type PACApplyOutcome string

const (
	PACApplyOutcomeApplied PACApplyOutcome = "applied"
	PACApplyOutcomeAbsent  PACApplyOutcome = "absent"
)

type PACServiceUpdate struct {
	ServiceName string
	Outcome     PACApplyOutcome
}

type Adapter interface {
	Capabilities() CapabilityReport
	ApplyPAC(url string, services []string) ([]PACServiceUpdate, error)
	CurrentPACState() ([]PACServiceState, error)
	ClearPACIfMatches(expected []PACServiceState) error
	TrustedCAs() ([]CARecord, error)
	TrustCA(ctx context.Context, certPEM []byte) error
	RemoveCAs(ctx context.Context, fingerprints []string) error
}

var CurrentAdapter Adapter = currentAdapter()

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
