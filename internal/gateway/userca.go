package gateway

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/QzCurious/seamless-cors/internal/userca"
)

type userCASnapshot struct {
	usable      bool
	certificate tls.Certificate
	expiresAt   time.Time
	renewalDue  bool
}

type userCAInstallResult struct {
	current userCASnapshot
	changed bool
}

type userCAUninstallResult struct {
	current userCASnapshot
	changed bool
}

// userCAModule is the Gateway-owned seam. Production adapts the standalone
// UserCA module; Gateway tests use an in-memory fake with the same semantics.
type userCAModule interface {
	Inspect(context.Context) (userCASnapshot, error)
	Install(context.Context) (userCAInstallResult, error)
	Uninstall(context.Context) (userCAUninstallResult, error)
}

type systemUserCA struct {
	module *userca.UserCA
}

func openSystemUserCA() (userCAModule, error) {
	module, err := userca.Open()
	if err != nil {
		return nil, err
	}
	return systemUserCA{module: module}, nil
}

func (a systemUserCA) Inspect(ctx context.Context) (userCASnapshot, error) {
	snapshot, err := a.module.Inspect(ctx)
	return adaptUserCASnapshot(snapshot), err
}

func (a systemUserCA) Install(ctx context.Context) (userCAInstallResult, error) {
	result, err := a.module.Install(ctx)
	if err != nil {
		return userCAInstallResult{}, err
	}
	return userCAInstallResult{
		current: adaptUserCASnapshot(result.Current()),
		changed: result.Changed(),
	}, nil
}

func (a systemUserCA) Uninstall(ctx context.Context) (userCAUninstallResult, error) {
	result, err := a.module.Uninstall(ctx)
	if err != nil {
		return userCAUninstallResult{}, err
	}
	return userCAUninstallResult{
		current: adaptUserCASnapshot(result.Current()),
		changed: result.Changed(),
	}, nil
}

func adaptUserCASnapshot(snapshot userca.Snapshot) userCASnapshot {
	certificate, usable := snapshot.TLSCertificate()
	return userCASnapshot{
		usable:      usable,
		certificate: certificate,
		expiresAt:   snapshot.ExpiresAt(),
		renewalDue:  snapshot.RenewalDue(),
	}
}
