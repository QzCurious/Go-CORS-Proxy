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
	Changed  bool                 `json:"changed"`
	Guidance *StartGuidanceDetail `json:"guidance,omitempty"`
}

type startFailureDetails struct {
	ManagedPACConsent  *ManagedPACConsentDetail  `json:"managedPacConsent,omitempty"`
	ManagedPACWarnings []ManagedPACWarningDetail `json:"managedPacWarnings,omitempty"`
	Diagnostic         string                    `json:"diagnostic,omitempty"`
	CleanupFailures    []CleanupFailureDetail    `json:"cleanupFailures,omitempty"`
}

type stopSuccessBody struct {
	Changed  bool             `json:"changed"`
	Warnings []CommandWarning `json:"warnings,omitempty"`
}

type stopFailureDetails struct {
	Warnings        []CommandWarning       `json:"warnings,omitempty"`
	CleanupFailures []CleanupFailureDetail `json:"cleanupFailures,omitempty"`
}

type installSuccessBody struct {
	Changed            bool      `json:"changed"`
	InstalledCAExpires time.Time `json:"installedCAExpires"`
}

type installFailureDetails struct {
	InstalledCAExpires time.Time            `json:"installedCAExpires,omitempty"`
	Warnings           []HTTPSWarningDetail `json:"warnings,omitempty"`
}

type uninstallSuccessBody struct {
	Changed  bool                 `json:"changed"`
	Warnings []HTTPSWarningDetail `json:"warnings,omitempty"`
}

type uninstallFailureDetails struct {
	ConsentFingerprint string               `json:"consentFingerprint,omitempty"`
	Warnings           []HTTPSWarningDetail `json:"warnings,omitempty"`
}

func startSuccessBodyFrom(result StartResult) startSuccessBody {
	return startSuccessBody{Changed: result.Kind == StartResultStarted, Guidance: result.Guidance}
}

func (dto startSuccessBody) semantic() StartResult {
	kind := StartResultAlreadyRunning
	if dto.Changed {
		kind = StartResultStarted
	}
	return StartResult{Kind: kind, Guidance: dto.Guidance}
}

func startFailureDetailsFrom(result StartResult) startFailureDetails {
	return startFailureDetails{
		ManagedPACConsent:  result.ManagedPACConsent,
		ManagedPACWarnings: result.ManagedPACWarnings,
		Diagnostic:         result.Diagnostic,
		CleanupFailures:    result.CleanupFailures,
	}
}

func (dto startFailureDetails) semantic(kind StartResultKind) StartResult {
	return StartResult{
		Kind:               kind,
		ManagedPACConsent:  dto.ManagedPACConsent,
		ManagedPACWarnings: dto.ManagedPACWarnings,
		Diagnostic:         dto.Diagnostic,
		CleanupFailures:    dto.CleanupFailures,
	}
}

func stopSuccessBodyFrom(result StopResult) stopSuccessBody {
	return stopSuccessBody{Changed: result.Kind == StopResultStopped, Warnings: result.Warnings}
}

func (dto stopSuccessBody) semantic() StopResult {
	kind := StopResultNotRunning
	if dto.Changed {
		kind = StopResultStopped
	}
	return StopResult{Kind: kind, Warnings: dto.Warnings}
}

func stopFailureDetailsFrom(result StopResult) stopFailureDetails {
	return stopFailureDetails{result.Warnings, result.CleanupFailures}
}

func (dto stopFailureDetails) semantic(kind StopResultKind) StopResult {
	return StopResult{Kind: kind, Warnings: dto.Warnings, CleanupFailures: dto.CleanupFailures}
}

func installSuccessBodyFrom(result InstallResult) installSuccessBody {
	return installSuccessBody{Changed: result.Kind == InstallResultInstalled, InstalledCAExpires: result.InstalledCAExpires}
}

func (dto installSuccessBody) semantic() InstallResult {
	kind := InstallResultAlreadyUsable
	if dto.Changed {
		kind = InstallResultInstalled
	}
	return InstallResult{Kind: kind, InstalledCAExpires: dto.InstalledCAExpires}
}

func installFailureDetailsFrom(result InstallResult) installFailureDetails {
	return installFailureDetails{result.InstalledCAExpires, result.Warnings}
}

func (dto installFailureDetails) semantic(kind InstallResultKind) InstallResult {
	return InstallResult{Kind: kind, InstalledCAExpires: dto.InstalledCAExpires, Warnings: dto.Warnings}
}

func uninstallSuccessBodyFrom(result UninstallResult) uninstallSuccessBody {
	return uninstallSuccessBody{Changed: result.Kind == UninstallResultUninstalled, Warnings: result.Warnings}
}

func (dto uninstallSuccessBody) semantic() UninstallResult {
	kind := UninstallResultAlreadyAbsent
	if dto.Changed {
		kind = UninstallResultUninstalled
	}
	return UninstallResult{Kind: kind, Warnings: dto.Warnings}
}

func uninstallFailureDetailsFrom(result UninstallResult) uninstallFailureDetails {
	return uninstallFailureDetails{result.ConsentFingerprint, result.Warnings}
}

func (dto uninstallFailureDetails) semantic(kind UninstallResultKind) UninstallResult {
	return UninstallResult{Kind: kind, ConsentFingerprint: dto.ConsentFingerprint, Warnings: dto.Warnings}
}
