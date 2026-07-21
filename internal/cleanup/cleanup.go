package cleanup

import (
	"fmt"
	"strings"

	"seamless-cors/internal/platform"
)

type Inspector interface {
	CurrentPACState() ([]platform.PACServiceState, error)
}

type Cleaner interface {
	ClearOwnedPAC() error
}

type Adapter interface {
	Inspector
	Cleaner
}

type Inspection struct {
	OwnedPAC bool
}

func Inspect(adapter Inspector) Inspection {
	inspection := Inspection{}
	if states, err := adapter.CurrentPACState(); err == nil && platform.HasOwnedPACState(states) {
		inspection.OwnedPAC = true
	}
	return inspection
}

func (i Inspection) Needed() bool {
	return i.OwnedPAC
}

func Clean(adapter Cleaner) error {
	if err := adapter.ClearOwnedPAC(); err != nil {
		return fmt.Errorf("managed PAC cleanup failed: %w", err)
	}
	return nil
}

type Error struct {
	Causes []error
}

func (e Error) Error() string {
	var parts []string
	for _, err := range e.Causes {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ") + "\nCleanup failed; resolve the OS or permission problem, then run `seamless-cors stop` again."
}
