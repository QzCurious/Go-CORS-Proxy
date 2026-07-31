package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"io"
	"net/http"
	"strings"
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
	var result StartResult
	err := c.callJSON(ctx, http.MethodPost, "/start", request, &result)
	var responseErr *responseError
	if errors.As(err, &responseErr) {
		var failure struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(responseErr.body, &failure) == nil && failure.Detail != "" {
			return result, &StartError{Diagnostic: failure.Detail}
		}
	}
	return result, err
}

func (c *client) Stop(ctx context.Context) (StopResult, error) {
	var result StopResult
	err := c.callJSON(ctx, http.MethodPost, "/stop", nil, &result)
	return result, err
}

func (c *client) Status(ctx context.Context) (StatusResult, error) {
	var result StatusResult
	err := c.callJSON(ctx, http.MethodGet, "/status", nil, &result)
	return result, err
}

func (c *client) Install(ctx context.Context) (InstallResult, error) {
	var result InstallResult
	err := c.callJSON(ctx, http.MethodPost, "/install", nil, &result)
	return result, err
}

func (c *client) Uninstall(ctx context.Context, request UninstallRequest) (UninstallResult, error) {
	var result UninstallResult
	err := c.callJSON(ctx, http.MethodPost, "/uninstall", request, &result)
	return result, err
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
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		text := strings.TrimSpace(string(data))
		if strings.Contains(text, managedpac.ErrManagedPACLeaseLost.Error()) {
			return managedpac.ErrManagedPACLeaseLost
		}
		for _, coordinationErr := range []error{errCAOperationInProgress, errGatewayOwnerEnding, errOwnerTransition} {
			if strings.Contains(text, coordinationErr.Error()) {
				return coordinationErr
			}
		}
		return &responseError{path: path, status: resp.Status, body: data, text: text}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type responseError struct {
	path   string
	status string
	body   []byte
	text   string
}

func (e *responseError) Error() string {
	return fmt.Sprintf("%s returned %s: %s", e.path, e.status, e.text)
}
