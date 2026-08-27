package managedpac

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/lib/pacsettings"
)

// pacSettings is Managed PAC's private seam to current-user OS PAC behavior.
// Fresh-observation policy, ownership classification, and mutation consequences
// remain Managed PAC policy.
type pacSettings interface {
	List(context.Context) ([]string, error)
	Lookup(context.Context, string) (pacsettings.Setting, error)
	SetURL(context.Context, string, string) error
	Disable(context.Context, string) error
}

var _ pacSettings = (*pacsettings.Settings)(nil)
