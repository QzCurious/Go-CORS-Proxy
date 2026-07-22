//go:build !darwin && !windows

package platform

import (
	"context"
	"fmt"
	"runtime"
)

type UnsupportedAdapter struct{}

func currentAdapter() Adapter {
	return UnsupportedAdapter{}
}

func (UnsupportedAdapter) Capabilities() CapabilityReport {
	return CapabilityReport{
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		Supported:         false,
		PACManagement:     CapabilityUnsupported,
		CATrustManagement: CapabilityUnsupported,
		RuntimeCleanup:    CapabilityLimited,
	}
}

func (UnsupportedAdapter) ApplyPAC(string, []string) ([]PACServiceUpdate, error) {
	return nil, fmt.Errorf("managed PAC routing is unsupported on this platform")
}

func (UnsupportedAdapter) CurrentPACState() ([]PACServiceState, error) {
	return nil, nil
}

func (UnsupportedAdapter) ClearPACIfMatches([]PACServiceState) error {
	return nil
}

func (UnsupportedAdapter) TrustedCAs() ([]CARecord, error) {
	return nil, nil
}

func (UnsupportedAdapter) TrustCA(context.Context, []byte) error {
	return nil
}

func (UnsupportedAdapter) RemoveCAs(context.Context, []string) error {
	return nil
}
