package userca

import (
	"context"
	"crypto/tls"
	"fmt"
	"path/filepath"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/truststore"
	"github.com/adrg/xdg"
)

// CA maintains the current user's seamless-cors development authority as one
// coherent capability. It does not cache assessment results.
type CA struct {
	dir        string
	trustStore trustStore
	now        func() time.Time
}

// State is one coherent observation of UserCA facts and, exactly when
// usable, the matching opaque signing capability. Its zero value is not usable.
type State struct {
	Usable          bool
	ExpiresAt       time.Time
	RenewalDue      bool
	SigningMaterial *tls.Certificate
}

// New resolves private storage and platform trust integration without
// inspecting or mutating either.
func New() (*CA, error) {
	trustStore, err := truststore.New()
	if err != nil {
		return nil, fmt.Errorf("open operating-system trust store: %w", err)
	}
	return &CA{
		dir:        filepath.Join(xdg.StateHome, "seamless-cors", "userca"),
		trustStore: trustStore,
		now:        time.Now,
	}, nil
}

// Inspect freshly derives one coherent State from the local pair and
// current-user OS trust.
func (u *CA) Inspect(ctx context.Context) (State, error) {
	state, err := u.inspect(ctx)
	return state.current, err
}

// Install establishes one usable authority pair. A valid pair is reused and
// repaired; renewal, expiry, and invalid or ambiguous state are replaced.
// Gateway serializes this mutation and owns its runtime consequences.
func (u *CA) Install(ctx context.Context) (State, error) {
	return u.install(ctx)
}

// Uninstall removes and verifies every owned trust and local authority fact.
func (u *CA) Uninstall(ctx context.Context) error {
	return u.uninstall(ctx)
}
