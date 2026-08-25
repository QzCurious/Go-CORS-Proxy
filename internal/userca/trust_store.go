package userca

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/lib/truststore"
)

type trustStore interface {
	List(ctx context.Context) ([]truststore.Certificate, error)
	Add(ctx context.Context, certificatePath string) error
	Remove(ctx context.Context, fingerprints []string) error
}

var _ trustStore = (*truststore.Store)(nil)
