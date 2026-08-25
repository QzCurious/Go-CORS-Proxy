package managedpac

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/lib/pacsettings"
)

// pacSettings is Managed PAC's private seam to current-user OS PAC mechanics.
// Ownership classification and mutation consequences remain Managed PAC policy.
type pacSettings interface {
	List(context.Context) ([]pacsettings.Setting, error)
	SetURL(context.Context, pacsettings.Setting, string) (pacsettings.MutationResult, error)
	Disable(context.Context, pacsettings.Setting) (pacsettings.MutationResult, error)
}

var _ pacSettings = (*pacsettings.Settings)(nil)
