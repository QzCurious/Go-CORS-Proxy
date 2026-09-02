package gateway

import "testing"

func TestCleanupStatusPreservesSystemPACUncertaintyBesideNeededCache(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	report := SystemPACReport{Issues: []SystemPACIssue{{Kind: SystemPACIssueDiscovery, Cause: "denied"}}}
	status := inspectGatewayFootprint(coord, true, stateCache{}, report)
	if status.State != CleanupStatusNeeded || status.Subjects[1].State != CleanupStatusUnknown {
		t.Fatalf("status = %#v", status)
	}
}

func TestCleanupStatusTreatsDisabledOwnedPACAsClean(t *testing.T) {
	coord := newCoordinator(t.TempDir())
	report := SystemPACReport{Services: []SystemPACServiceState{{Name: "Wi-Fi", Ownership: "owned", Enabled: false}}}
	status := inspectGatewayFootprint(coord, false, stateCache{}, report)
	if status.State != CleanupStatusNone {
		t.Fatalf("status = %#v", status)
	}
}
