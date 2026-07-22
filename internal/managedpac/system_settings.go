package managedpac

import "context"

type SystemSettings interface {
	Apply(ctx context.Context, pacURL string, serviceNames []string) (ApplyResult, error)
	Snapshot(ctx context.Context) ([]ServiceSnapshot, error)
	ClearIfUnchanged(ctx context.Context, expected []ServiceSnapshot) error
}

type ApplyResult struct {
	AppliedServices []string
}

type ServiceSnapshot struct {
	ServiceName string
	PACURL      string
	Enabled     bool
}

func NewSystemSettings() SystemSettings {
	return newSystemSettings()
}
