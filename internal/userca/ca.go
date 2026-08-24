package userca

import (
	"errors"
	"os"
	"time"
)

const (
	commonName    = "seamless-cors Installed User CA"
	certFileName  = "certificate.pem"
	keyFileName   = "private-key.pem"
	validity      = 5 * 365 * 24 * time.Hour
	renewalWindow = 90 * 24 * time.Hour
)

var (
	errInvalidAuthority = errors.New("UserCA authority material is invalid")
	readFile            = os.ReadFile
)

// CA maintains the current user's seamless-cors development authority as one
// coherent capability. It does not cache assessment results.
type CA struct {
	dir   string
	store trustStore
	now   func() time.Time
}

// Open resolves private storage and platform trust integration without
// inspecting or mutating either.
func Open() *CA {
	return openAt(defaultDir(), newTrustStore(), time.Now)
}

func openAt(dir string, store trustStore, now func() time.Time) *CA {
	if now == nil {
		now = time.Now
	}
	return &CA{dir: dir, store: store, now: now}
}
