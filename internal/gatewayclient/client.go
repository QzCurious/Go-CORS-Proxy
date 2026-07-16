package gatewayclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"seamless-cors/internal/gatewaycoord"
	"seamless-cors/internal/gatewayfacade"
	"seamless-cors/internal/managedpac"
)

const tokenHeader = "X-Seamless-CORS-Token"

type Client struct {
	cache      gatewaycoord.GatewayStateCache
	httpClient *http.Client
}

type TargetKind string

const (
	TargetMissing TargetKind = "missing"
	TargetStale   TargetKind = "stale"
	TargetActive  TargetKind = "active"
)

type Target struct {
	Kind   TargetKind
	Cache  gatewaycoord.GatewayStateCache
	Client *Client
}

func Discover() (Target, error) {
	coord, err := gatewaycoord.Default()
	if err != nil {
		return Target{}, err
	}
	verification := coord.Verify()
	switch verification.Status {
	case gatewaycoord.Active:
		return Target{
			Kind:   TargetActive,
			Cache:  verification.Cache,
			Client: New(verification.Cache),
		}, nil
	case gatewaycoord.Stale:
		return Target{Kind: TargetStale, Cache: verification.Cache}, nil
	default:
		return Target{Kind: TargetMissing}, nil
	}
}

func New(cache gatewaycoord.GatewayStateCache) *Client {
	return &Client{
		cache:      cache,
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
}

func (c *Client) Start(request gatewayfacade.StartRequest) (gatewayfacade.StartResult, error) {
	var result gatewayfacade.StartResult
	err := c.callJSON(http.MethodPost, "/start", request, &result)
	var responseErr *responseError
	if errors.As(err, &responseErr) {
		var failure struct {
			Detail   string                        `json:"detail"`
			CAEnsure *gatewayfacade.CAEnsureResult `json:"caEnsure"`
		}
		if json.Unmarshal(responseErr.body, &failure) == nil && failure.Detail != "" {
			result.CAEnsure = failure.CAEnsure
			return result, &gatewayfacade.StartError{Diagnostic: failure.Detail, CAEnsure: failure.CAEnsure}
		}
	}
	return result, err
}

func (c *Client) Stop() (gatewayfacade.StopResult, error) {
	var result gatewayfacade.StopResult
	err := c.callJSON(http.MethodPost, "/stop", nil, &result)
	return result, err
}

func (c *Client) Status() (gatewayfacade.StatusResult, error) {
	var result gatewayfacade.StatusResult
	err := c.callJSON(http.MethodGet, "/status", nil, &result)
	return result, err
}

func (c *Client) Install() (gatewayfacade.InstallResult, error) {
	var result gatewayfacade.InstallResult
	err := c.callJSON(http.MethodPost, "/install", nil, &result)
	return result, err
}

func (c *Client) Uninstall() (gatewayfacade.UninstallResult, error) {
	var result gatewayfacade.UninstallResult
	err := c.callJSON(http.MethodPost, "/uninstall", nil, &result)
	return result, err
}

func (c *Client) callJSON(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://"+c.cache.HTTPRouterListen+path, reader)
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
