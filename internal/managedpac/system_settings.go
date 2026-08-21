package managedpac

import "context"

type systemSettings interface {
	Apply(ctx context.Context, pacURL string, serviceNames []string) (applyResult, error)
	Snapshot(ctx context.Context) ([]serviceSnapshot, error)
	DisableOwned(ctx context.Context, serviceNames []string) error
}

type applyResult struct {
	AppliedServices []string
}

type serviceSnapshot struct {
	ServiceName string
	PACURL      string
	Enabled     bool
}
