package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const tokenHeader = "X-Seamless-CORS-Token"

type client struct {
	cache      stateCache
	httpClient *http.Client
}

type targetKind string

const (
	targetMissing targetKind = "missing"
	targetStale   targetKind = "stale"
	targetActive  targetKind = "active"
)

type target struct {
	kind   targetKind
	cache  stateCache
	client *client
}

func discover() (target, error) {
	coord, err := defaultCoordinator()
	if err != nil {
		return target{}, err
	}
	verification := coord.Verify()
	switch verification.Status {
	case stateActive:
		return target{
			kind:   targetActive,
			cache:  verification.Cache,
			client: newClient(verification.Cache),
		}, nil
	case stateStale:
		return target{kind: targetStale, cache: verification.Cache}, nil
	default:
		return target{kind: targetMissing}, nil
	}
}

func newClient(cache stateCache) *client {
	return &client{
		cache:      cache,
		httpClient: &http.Client{},
	}
}

func (c *client) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	var success startSuccessBody
	err := c.callJSON(ctx, http.MethodPost, "/start", request, &success)
	if err == nil {
		if success.Changed && success.Guidance == nil {
			return nil, fmt.Errorf("/start returned a started result without guidance")
		}
		if !success.Changed && success.Guidance != nil {
			return nil, fmt.Errorf("/start returned already-running with guidance")
		}
		return success.semantic(), nil
	}
	result, decodeErr := decodeCommandFailure(err, knownStartFailureKind, startFailureDetails.semantic)
	if decodeErr == nil && result == nil {
		return nil, fmt.Errorf("/start returned an incomplete failure result")
	}
	return result, decodeErr
}

func (c *client) Stop(ctx context.Context) (StopResult, error) {
	var success stopSuccessBody
	err := c.callJSON(ctx, http.MethodPost, "/stop", nil, &success)
	if err == nil {
		return success.semantic(), nil
	}
	return decodeCommandFailure(err, knownStopFailureKind, stopFailureDetails.semantic)
}

func (c *client) Status(ctx context.Context) (StatusResult, error) {
	var success StatusReport
	err := c.callJSON(ctx, http.MethodGet, "/status", nil, &success)
	if err == nil {
		return StatusResult{Kind: StatusResultReported, StatusReport: success}, nil
	}
	var remote *remoteGatewayError
	if errors.As(err, &remote) && remote.body.Code == string(StatusResultOwnerTransition) {
		return StatusResult{Kind: StatusResultOwnerTransition}, nil
	}
	return StatusResult{}, err
}

func (c *client) Install(ctx context.Context) (InstallResult, error) {
	var success installSuccessBody
	err := c.callJSON(ctx, http.MethodPost, "/install", nil, &success)
	if err == nil {
		return success.semantic(), nil
	}
	return decodeCommandFailure(err, knownInstallFailureKind, installFailureDetails.semantic)
}

func (c *client) Uninstall(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	var success uninstallSuccessBody
	err := c.callJSON(ctx, http.MethodPost, "/uninstall", request, &success)
	if err == nil {
		return success.semantic(), nil
	}
	return decodeCommandFailure(err, knownUninstallFailureKind, uninstallFailureDetails.semantic)
}

func (c *client) callJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.cache.HTTPRouterListen+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set(tokenHeader, c.cache.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return readErr
		}
		var envelope gatewayErrorResponse
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.ErrorBody.Code == "" || envelope.ErrorBody.Message == "" {
			return &responseError{path: path, status: resp.Status, body: data}
		}
		return &remoteGatewayError{path: path, status: resp.StatusCode, body: envelope.ErrorBody}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type responseError struct {
	path   string
	status string
	body   []byte
}

func (e *responseError) Error() string {
	return fmt.Sprintf("%s returned %s with an invalid Gateway error response: %s", e.path, e.status, string(e.body))
}

type remoteGatewayError struct {
	path   string
	status int
	body   gatewayErrorBody
}

func (e *remoteGatewayError) Error() string {
	return fmt.Sprintf("%s returned %d %s: %s", e.path, e.status, e.body.Code, e.body.Message)
}

func decodeRemoteDetails(remote *remoteGatewayError, out any) error {
	if len(remote.body.Details) == 0 {
		return nil
	}
	if err := json.Unmarshal(remote.body.Details, out); err != nil {
		return fmt.Errorf("decode %s details: %w", remote.body.Code, err)
	}
	return nil
}

func decodeCommandFailure[K ~string, D, R any](
	err error,
	known func(K) bool,
	semantic func(D, K) R,
) (R, error) {
	var zero R
	var remote *remoteGatewayError
	if !errors.As(err, &remote) {
		return zero, err
	}
	kind := K(remote.body.Code)
	if !known(kind) {
		return zero, err
	}
	var details D
	if err := decodeRemoteDetails(remote, &details); err != nil {
		return zero, err
	}
	return semantic(details, kind), nil
}

func knownStartFailureKind(kind StartKind) bool {
	switch kind {
	case StartResultOwnerTransition,
		StartResultConsentRequired,
		StartResultConsentDeclined,
		StartResultNoManageablePACServices,
		StartResultManagedPACInstallationFailed,
		StartResultStartAlreadyMutating,
		StartResultStopCancelled,
		StartResultCleanupFailed:
		return true
	default:
		return false
	}
}

func knownStopFailureKind(kind StopResultKind) bool {
	return kind == StopResultCleanupFailed || kind == StopResultNotRunningCleanupFailed
}

func knownInstallFailureKind(kind InstallResultKind) bool {
	switch kind {
	case InstallResultApprovalDenied,
		InstallResultAlreadyMutating,
		InstallResultOwnerEnding,
		InstallResultOwnerTransition:
		return true
	default:
		return false
	}
}

func knownUninstallFailureKind(kind UninstallResultKind) bool {
	switch kind {
	case UninstallResultConsentRequired,
		UninstallResultAlreadyMutating,
		UninstallResultOwnerEnding,
		UninstallResultOwnerTransition,
		UninstallResultIncomplete:
		return true
	default:
		return false
	}
}
