package gateway

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	errorCodeUnauthorized   = "unauthorized"
	errorCodeInvalidRequest = "invalid-request"
	errorCodeInternal       = "internal-error"
)

type gatewayErrorResponse struct {
	ErrorBody gatewayErrorBody `json:"error"`
	status    int
}

type gatewayErrorBody struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *gatewayErrorResponse) Error() string  { return e.ErrorBody.Message }
func (e *gatewayErrorResponse) GetStatus() int { return e.status }

func newGatewayError(status int, code, message string, details any) *gatewayErrorResponse {
	var raw json.RawMessage
	if details != nil {
		raw, _ = json.Marshal(details)
	}
	return &gatewayErrorResponse{
		ErrorBody: gatewayErrorBody{Code: code, Message: message, Details: raw},
		status:    status,
	}
}

func newRouterError(status int, message string, errs ...error) *gatewayErrorResponse {
	code := errorCodeInternal
	if status == http.StatusUnauthorized {
		code = errorCodeUnauthorized
	} else if status >= 400 && status < 500 {
		code = errorCodeInvalidRequest
	}
	diagnostics := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			diagnostics = append(diagnostics, err.Error())
		}
	}
	if len(diagnostics) == 0 {
		return newGatewayError(status, code, message, nil)
	}
	return newGatewayError(status, code, message, struct {
		Diagnostics []string `json:"diagnostics"`
	}{Diagnostics: diagnostics})
}

type startSuccessBody struct {
	Changed                     bool                               `json:"changed"`
	Guidance                    *StartGuidance                     `json:"guidance,omitempty"`
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail `json:"upstreamListCreationWarning,omitempty"`
}

type startFailureDetails struct {
	UpstreamListCreationConsent *UpstreamListCreationConsent       `json:"upstreamListCreationConsent,omitempty"`
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail `json:"upstreamListCreationWarning,omitempty"`
	CleanupFailures             []CleanupFailure                   `json:"cleanupFailures,omitempty"`
}

type stopSuccessBody struct {
	Changed            bool               `json:"changed"`
	Warnings           []CommandWarning   `json:"warnings,omitempty"`
	CleanupFulfillment CommandFulfillment `json:"cleanupFulfillment"`
	SystemPACCleanup   SystemPACReport    `json:"systemPacCleanup"`
	CleanupFailures    []CleanupFailure   `json:"cleanupFailures,omitempty"`
}

type installSuccessBody struct {
	InstalledCAExpires time.Time `json:"installedCAExpires"`
}

type installFailureDetails struct {
	InstalledCAExpires time.Time `json:"installedCAExpires,omitempty"`
}

type uninstallSuccessBody struct{}

type uninstallFailureDetails struct {
	ConsentFingerprint string              `json:"consentFingerprint,omitempty"`
	CleanupIssue       *UserCACleanupIssue `json:"cleanupIssue,omitempty"`
}

func startSuccessBodyFrom(result StartResult) startSuccessBody {
	switch typed := result.(type) {
	case Started:
		guidance := typed.Guidance
		return startSuccessBody{Changed: true, Guidance: &guidance, UpstreamListCreationWarning: typed.UpstreamListCreationWarning}
	case AlreadyRunning:
		return startSuccessBody{}
	default:
		return startSuccessBody{}
	}
}

func (dto startSuccessBody) semantic() StartResult {
	if !dto.Changed {
		return AlreadyRunning{}
	}
	var guidance StartGuidance
	if dto.Guidance != nil {
		guidance = *dto.Guidance
	}
	return Started{Guidance: guidance, UpstreamListCreationWarning: dto.UpstreamListCreationWarning}
}

func startFailureDetailsFrom(result StartResult) startFailureDetails {
	details := startFailureDetails{UpstreamListCreationWarning: result.UpstreamListCreationWarningDetail()}
	switch typed := result.(type) {
	case StartUpstreamListCreationConsentRequired:
		consent := typed.Consent
		details.UpstreamListCreationConsent = &consent
	case StartCleanupFailed:
		details.CleanupFailures = typed.Failures
	}
	return details
}

func (dto startFailureDetails) semantic(kind StartKind) StartResult {
	switch kind {
	case StartResultUpstreamListCreationConsentRequired:
		if dto.UpstreamListCreationConsent == nil {
			return nil
		}
		return StartUpstreamListCreationConsentRequired{Consent: *dto.UpstreamListCreationConsent}
	case StartResultOwnerTransition:
		return StartOwnerTransition{}
	case StartResultStartAlreadyMutating:
		return StartAlreadyMutating{UpstreamListCreationWarning: dto.UpstreamListCreationWarning}
	case StartResultStopCancelled:
		return StartStopCancelled{UpstreamListCreationWarning: dto.UpstreamListCreationWarning}
	case StartResultCleanupFailed:
		return StartCleanupFailed{Failures: dto.CleanupFailures, UpstreamListCreationWarning: dto.UpstreamListCreationWarning}
	default:
		return nil
	}
}

func stopSuccessBodyFrom(result StopResult) stopSuccessBody {
	return stopSuccessBody{Changed: result.Kind == StopResultStopped, Warnings: result.Warnings, CleanupFulfillment: result.CleanupFulfillment, SystemPACCleanup: result.SystemPACCleanup, CleanupFailures: result.CleanupFailures}
}

func (dto stopSuccessBody) semantic() StopResult {
	kind := StopResultNotRunning
	if dto.Changed {
		kind = StopResultStopped
	}
	return StopResult{Kind: kind, Warnings: dto.Warnings, CleanupFulfillment: dto.CleanupFulfillment, SystemPACCleanup: dto.SystemPACCleanup, CleanupFailures: dto.CleanupFailures}
}

func installSuccessBodyFrom(result InstallResult) installSuccessBody {
	return installSuccessBody{InstalledCAExpires: result.InstalledCAExpires}
}

func (dto installSuccessBody) semantic() InstallResult {
	return InstallResult{Kind: InstallResultInstalled, InstalledCAExpires: dto.InstalledCAExpires}
}

func installFailureDetailsFrom(result InstallResult) installFailureDetails {
	return installFailureDetails{InstalledCAExpires: result.InstalledCAExpires}
}

func (dto installFailureDetails) semantic(kind InstallResultKind) InstallResult {
	return InstallResult{Kind: kind, InstalledCAExpires: dto.InstalledCAExpires}
}

func uninstallSuccessBodyFrom(result UninstallResult) uninstallSuccessBody {
	return uninstallSuccessBody{}
}

func (dto uninstallSuccessBody) semantic() UninstallResult {
	return UninstallResult{Kind: UninstallResultUninstalled}
}

func uninstallFailureDetailsFrom(result UninstallResult) uninstallFailureDetails {
	return uninstallFailureDetails{result.ConsentFingerprint, result.CleanupIssue}
}

func (dto uninstallFailureDetails) semantic(kind UninstallResultKind) UninstallResult {
	return UninstallResult{Kind: kind, ConsentFingerprint: dto.ConsentFingerprint, CleanupIssue: dto.CleanupIssue}
}
