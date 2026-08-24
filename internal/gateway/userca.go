package gateway

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/QzCurious/seamless-cors/internal/userca"
)

// userCAState is the Gateway-owned assessment shape used for HTTPS coordination.
// Only the system adapter translates UserCA-owned State into this shape.
type userCAState struct {
	Usable     bool
	ExpiresAt  time.Time
	RenewalDue bool

	signingMaterial *tls.Certificate
}

func (s userCAState) SigningMaterial() *tls.Certificate { return s.signingMaterial }

// userCAModule is the Gateway-owned behavioral seam for UserCA facts and
// mutations needed by Gateway lifecycle coordination.
type userCAModule interface {
	Inspect(context.Context) (userCAState, error)
	Install(context.Context) (userCAState, error)
	Uninstall(context.Context) error
}

type systemUserCA struct {
	ca *userca.CA
}

func (u systemUserCA) Inspect(ctx context.Context) (userCAState, error) {
	current, err := u.ca.Inspect(ctx)
	return adaptUserCAState(current), err
}

func (u systemUserCA) Install(ctx context.Context) (userCAState, error) {
	current, err := u.ca.Install(ctx)
	return adaptUserCAState(current), err
}

func (u systemUserCA) Uninstall(ctx context.Context) error {
	return u.ca.Uninstall(ctx)
}

func adaptUserCAState(current userca.State) userCAState {
	return userCAState{
		Usable:          current.Usable,
		ExpiresAt:       current.ExpiresAt,
		RenewalDue:      current.RenewalDue,
		signingMaterial: current.SigningMaterial,
	}
}

func openSystemUserCA() userCAModule {
	return systemUserCA{ca: userca.New()}
}
