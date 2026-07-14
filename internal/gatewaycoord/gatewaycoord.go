package gatewaycoord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"seamless-cors/internal/liveconfig"
)

const StateFileName = "gateway-state-cache.json"

type StateStatus string

const (
	Missing StateStatus = "missing"
	Active  StateStatus = "active"
	Stale   StateStatus = "stale"
)

type GatewayStateCache struct {
	HTTPRouterListen string `json:"httpRouterListen"`
	Token            string `json:"token"`
}

type Verification struct {
	Status StateStatus
	Cache  GatewayStateCache
}

type OwnerVerifier func(GatewayStateCache) bool

func Default() (*Coordinator, error) {
	runtimeDir, err := liveconfig.RuntimeDir()
	if err != nil {
		return nil, err
	}
	return New(runtimeDir), nil
}

type Coordinator struct {
	runtimeDir    string
	statePath     string
	ownerVerifier OwnerVerifier
}

func New(runtimeDir string) *Coordinator {
	return NewWithVerifier(runtimeDir, VerifyHTTPRouter)
}

func NewWithVerifier(runtimeDir string, ownerVerifier OwnerVerifier) *Coordinator {
	if ownerVerifier == nil {
		ownerVerifier = VerifyHTTPRouter
	}
	return &Coordinator{
		runtimeDir:    runtimeDir,
		statePath:     filepath.Join(runtimeDir, StateFileName),
		ownerVerifier: ownerVerifier,
	}
}

func (c *Coordinator) RuntimeDirPath() string {
	return c.runtimeDir
}

func (c *Coordinator) StateFilePath() string {
	return c.statePath
}

func (c *Coordinator) Verify() Verification {
	cache, err := c.read()
	if err != nil {
		if os.IsNotExist(err) {
			return Verification{Status: Missing}
		}
		return Verification{Status: Stale}
	}
	if c.ownerVerifier(cache) {
		return Verification{Status: Active, Cache: cache}
	}
	return Verification{Status: Stale, Cache: cache}
}

func (c *Coordinator) EnsureStartAllowed() (bool, error) {
	if err := os.MkdirAll(c.runtimeDir, 0o700); err != nil {
		return false, err
	}
	switch c.Verify().Status {
	case Active:
		return false, fmt.Errorf("gateway owner already running")
	case Stale:
		return true, nil
	default:
		return false, nil
	}
}

func (c *Coordinator) Claim(cache GatewayStateCache) error {
	if cache.HTTPRouterListen == "" || cache.Token == "" {
		return fmt.Errorf("gateway state cache requires router listen and token")
	}
	if err := writeExclusive(c.statePath, cache); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return err
	}
	if c.Verify().Status == Active {
		return fmt.Errorf("gateway owner already running")
	}
	return writeAtomicReplace(c.statePath, cache)
}

func (c *Coordinator) Remove() error {
	err := os.Remove(c.statePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *Coordinator) RemoveOwned(cache GatewayStateCache) error {
	if !c.Owns(cache) {
		return nil
	}
	return c.Remove()
}

func (c *Coordinator) Owns(cache GatewayStateCache) bool {
	current, err := c.read()
	if err != nil {
		return false
	}
	return current.HTTPRouterListen == cache.HTTPRouterListen && current.Token == cache.Token
}

func (c *Coordinator) Write(cache GatewayStateCache) error {
	return writeExclusive(c.statePath, cache)
}

func (c *Coordinator) read() (GatewayStateCache, error) {
	data, err := os.ReadFile(c.statePath)
	if err != nil {
		return GatewayStateCache{}, err
	}
	var cache GatewayStateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return GatewayStateCache{}, err
	}
	return cache, nil
}

func writeExclusive(path string, cache GatewayStateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func writeAtomicReplace(path string, cache GatewayStateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gateway-state-cache-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func WaitForStop(cache GatewayStateCache) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !VerifyHTTPRouter(cache) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func VerifyHTTPRouter(cache GatewayStateCache) bool {
	if cache.HTTPRouterListen == "" || cache.Token == "" {
		return false
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	client := http.Client{Timeout: 500 * time.Millisecond}
	for {
		req, err := http.NewRequest(http.MethodGet, "http://"+cache.HTTPRouterListen+"/health", nil)
		if err == nil {
			req.Header.Set("X-Seamless-CORS-Token", cache.Token)
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusNoContent {
					return true
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
