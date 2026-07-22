package gateway

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanupStatusPreservesMixedNeededAndUnknownSubjectStates(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	adapter := &lifecycleTestAdapter{stateErr: errors.New("PAC inspection denied")}

	status := inspectGatewayFootprint(adapter, coord, true, false, stateCache{})

	if status.State != CleanupStatusNeeded {
		t.Fatalf("overall cleanup state = %s, want %s", status.State, CleanupStatusNeeded)
	}
	if len(status.Subjects) != 2 {
		t.Fatalf("subject statuses = %#v", status.Subjects)
	}
	if status.Subjects[0].Subject != CleanupSubjectGatewayStateCache || status.Subjects[0].State != CleanupStatusNeeded {
		t.Fatalf("Gateway State Cache status = %#v", status.Subjects[0])
	}
	if status.Subjects[1].Subject != CleanupSubjectManagedPAC || status.Subjects[1].State != CleanupStatusUnknown ||
		!strings.Contains(status.Subjects[1].Diagnostic, "PAC inspection denied") {
		t.Fatalf("Managed PAC status = %#v", status.Subjects[1])
	}
}

func TestCleanupStatusIsUnknownWhenNoSubjectIsKnownNeeded(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	adapter := &lifecycleTestAdapter{stateErr: errors.New("PAC inspection denied")}

	status := inspectGatewayFootprint(adapter, coord, false, false, stateCache{})

	if status.State != CleanupStatusUnknown {
		t.Fatalf("overall cleanup state = %s, want %s", status.State, CleanupStatusUnknown)
	}
}
