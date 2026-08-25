package userca

import (
	"context"
	"errors"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/lib/truststore"
)

type trustStore interface {
	List(ctx context.Context) ([]truststore.Certificate, error)
	Add(ctx context.Context, certificatePath string) error
	Remove(ctx context.Context, fingerprints []string) error
}

var _ trustStore = (*truststore.Store)(nil)

func userCAAddError(err error) error {
	if err == nil {
		return nil
	}
	var denied *truststore.ApprovalDeniedError
	if errors.As(err, &denied) {
		return fmt.Errorf("%w: %w", ErrApprovalDenied, err)
	}
	return err
}
