package gateway

import "testing"

func TestCommandFulfillmentIsOwnedByGatewayResultKinds(t *testing.T) {
	tests := []struct {
		name string
		got  CommandFulfillment
		want CommandFulfillment
	}{
		{"start started", Started{}.Fulfillment(), CommandFulfilled},
		{"start already running", AlreadyRunning{}.Fulfillment(), CommandFulfilled},
		{"start transition", StartOwnerTransition{}.Fulfillment(), CommandUnfulfilled},
		{"stop stopped", StopResult{Kind: StopResultStopped}.Fulfillment(), CommandFulfilled},
		{"stop not running", StopResult{Kind: StopResultNotRunning}.Fulfillment(), CommandFulfilled},
		{"stop cleanup failed", StopResult{Kind: StopResultCleanupFailed}.Fulfillment(), CommandUnfulfilled},
		{"install installed", InstallResult{Kind: InstallResultInstalled}.Fulfillment(), CommandFulfilled},
		{"install already usable", InstallResult{Kind: InstallResultAlreadyUsable}.Fulfillment(), CommandFulfilled},
		{"install partial", InstallResult{Kind: InstallResultRuntimeAdoptionFailed}.Fulfillment(), CommandUnfulfilled},
		{"uninstall removed", UninstallResult{Kind: UninstallResultUninstalled}.Fulfillment(), CommandFulfilled},
		{"uninstall absent", UninstallResult{Kind: UninstallResultAlreadyAbsent}.Fulfillment(), CommandFulfilled},
		{"uninstall incomplete", UninstallResult{Kind: UninstallResultIncomplete}.Fulfillment(), CommandUnfulfilled},
		{"status reported", StatusResult{Kind: StatusResultReported}.Fulfillment(), CommandFulfilled},
		{"status transition", StatusResult{Kind: StatusResultOwnerTransition}.Fulfillment(), CommandUnfulfilled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("fulfillment = %s, want %s", test.got, test.want)
			}
		})
	}
}
