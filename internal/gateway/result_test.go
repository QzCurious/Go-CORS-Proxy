package gateway

import "testing"

func TestEveryCommandResultKindHasItsDomainFulfillment(t *testing.T) {
	tests := []struct {
		name   string
		result interface{ Fulfillment() CommandFulfillment }
		want   CommandFulfillment
	}{
		{"start started", Started{}, CommandFulfilled},
		{"start already running", AlreadyRunning{}, CommandFulfilled},
		{"start owner transition", StartOwnerTransition{}, CommandUnfulfilled},
		{"start upstream-list creation consent required", StartUpstreamListCreationConsentRequired{}, CommandUnfulfilled},
		{"start consent required", StartConsentRequired{}, CommandUnfulfilled},
		{"start consent declined", StartConsentDeclined{}, CommandUnfulfilled},
		{"start no manageable PAC services", StartNoManageablePACServices{}, CommandUnfulfilled},
		{"start managed PAC installation failed", StartManagedPACInstallationFailed{}, CommandUnfulfilled},
		{"start already mutating", StartAlreadyMutating{}, CommandUnfulfilled},
		{"start stop cancelled", StartStopCancelled{}, CommandUnfulfilled},
		{"start cleanup failed", StartCleanupFailed{}, CommandUnfulfilled},
		{"stop stopped", StopResult{Kind: StopResultStopped}, CommandFulfilled},
		{"stop not running", StopResult{Kind: StopResultNotRunning}, CommandFulfilled},
		{"stop cleanup failed", StopResult{Kind: StopResultCleanupFailed}, CommandUnfulfilled},
		{"stop not running cleanup failed", StopResult{Kind: StopResultNotRunningCleanupFailed}, CommandUnfulfilled},
		{"install installed", InstallResult{Kind: InstallResultInstalled}, CommandFulfilled},
		{"install already mutating", InstallResult{Kind: InstallResultAlreadyMutating}, CommandUnfulfilled},
		{"install owner ending", InstallResult{Kind: InstallResultOwnerEnding}, CommandUnfulfilled},
		{"install owner transition", InstallResult{Kind: InstallResultOwnerTransition}, CommandUnfulfilled},
		{"uninstall uninstalled", UninstallResult{Kind: UninstallResultUninstalled}, CommandFulfilled},
		{"uninstall consent required", UninstallResult{Kind: UninstallResultConsentRequired}, CommandUnfulfilled},
		{"uninstall already mutating", UninstallResult{Kind: UninstallResultAlreadyMutating}, CommandUnfulfilled},
		{"uninstall owner ending", UninstallResult{Kind: UninstallResultOwnerEnding}, CommandUnfulfilled},
		{"uninstall owner transition", UninstallResult{Kind: UninstallResultOwnerTransition}, CommandUnfulfilled},
		{"uninstall incomplete", UninstallResult{Kind: UninstallResultIncomplete}, CommandUnfulfilled},
		{"status reported", StatusResult{Kind: StatusResultReported}, CommandFulfilled},
		{"status owner transition", StatusResult{Kind: StatusResultOwnerTransition}, CommandUnfulfilled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.Fulfillment(); got != test.want {
				t.Fatalf("fulfillment = %s, want %s", got, test.want)
			}
		})
	}
}
