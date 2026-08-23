package gateway

import (
	"context"

	"github.com/QzCurious/seamless-cors/internal/userca"
)

// userCAModule is the Gateway-owned behavioral seam. UserCA owns every
// semantic value crossing it.
type userCAModule interface {
	Inspect(context.Context) (userca.Assessment, error)
	Install(context.Context) (userca.MutationResult, error)
	Uninstall(context.Context) (userca.MutationResult, error)
}

func openSystemUserCA() userCAModule {
	return userca.Open()
}
