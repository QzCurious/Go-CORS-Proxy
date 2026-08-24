package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestCleanupStatusPreservesMixedNeededAndUnknownSubjectStates(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	settings := &lifecycleTestSystemSettings{stateErr: errors.New("PAC inspection denied")}

	status := inspectGatewayFootprint(context.Background(), settings, coord, true, false, stateCache{})

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
	settings := &lifecycleTestSystemSettings{stateErr: errors.New("PAC inspection denied")}

	status := inspectGatewayFootprint(context.Background(), settings, coord, false, false, stateCache{})

	if status.State != CleanupStatusUnknown {
		t.Fatalf("overall cleanup state = %s, want %s", status.State, CleanupStatusUnknown)
	}
}

func TestCleanupStatusDoesNotInspectManagedPACWhileRuntimeIsActive(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	settings := &lifecycleTestSystemSettings{stateErr: errors.New("PAC inspection must not run")}

	status := inspectGatewayFootprint(context.Background(), settings, coord, false, true, stateCache{})

	if status.State != CleanupStatusNone {
		t.Fatalf("cleanup status = %#v", status)
	}
	if status.Subjects[1].State != CleanupStatusNone || status.Subjects[1].Diagnostic != "" {
		t.Fatalf("Managed PAC cleanup subject = %#v", status.Subjects[1])
	}
}

func TestCleanupStatusIgnoresDisabledOwnedPACURL(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{{
		Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: false, Ownership: managedpac.OwnershipOwned,
	}}}

	status := inspectGatewayFootprint(context.Background(), settings, coord, false, false, stateCache{})

	if status.State != CleanupStatusNone || status.Subjects[1].State != CleanupStatusNone {
		t.Fatalf("cleanup status = %#v, want no cleanup needed", status)
	}
}

func TestCleanupStatusReportsManagedPACWithoutGatewayStateCache(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	settings := &lifecycleTestSystemSettings{services: []managedpac.Service{{
		Name: "Wi-Fi", URL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true, Ownership: managedpac.OwnershipOwned,
	}}}

	status := inspectGatewayFootprint(context.Background(), settings, coord, false, false, stateCache{})

	if status.State != CleanupStatusNeeded {
		t.Fatalf("cleanup status = %#v, want cleanup needed", status)
	}
	if status.Subjects[0].Subject != CleanupSubjectGatewayStateCache || status.Subjects[0].State != CleanupStatusNone {
		t.Fatalf("Gateway State Cache status = %#v", status.Subjects[0])
	}
	if status.Subjects[1].Subject != CleanupSubjectManagedPAC || status.Subjects[1].State != CleanupStatusNeeded {
		t.Fatalf("Managed PAC status = %#v", status.Subjects[1])
	}
}
