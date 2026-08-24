package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
)

const stateFileName = "gateway-state-cache.json"
const ownerLockFileName = "gateway-owner.lock"
const gatewayRuntimeDirectoryName = "seamless-cors"

type stateStatus string

const (
	stateMissing stateStatus = "missing"
	stateActive  stateStatus = "active"
	stateStale   stateStatus = "stale"
)

type stateCache struct {
	HTTPRouterListen string `json:"httpRouterListen"`
	Token            string `json:"token"`
}

type verification struct {
	Status stateStatus
	Cache  stateCache
}

type ownerVerifier func(stateCache) bool

func defaultCoordinator() (*coordinator, error) {
	runtimeDir, err := defaultRuntimeDir()
	if err != nil {
		return nil, err
	}
	return newCoordinator(runtimeDir), nil
}

func defaultRuntimeDir() (string, error) {
	return resolveRuntimeDir(xdg.RuntimeFile)
}

type runtimeFileResolver func(string) (string, error)

func resolveRuntimeDir(resolve runtimeFileResolver) (string, error) {
	statePath, err := resolve(filepath.Join(gatewayRuntimeDirectoryName, stateFileName))
	if err != nil {
		return "", fmt.Errorf("resolve Gateway Runtime Directory: %w", err)
	}
	statePath = filepath.Clean(statePath)
	if !filepath.IsAbs(statePath) {
		return "", fmt.Errorf(
			"Gateway Runtime Directory resolution returned a non-absolute path: %q",
			statePath,
		)
	}
	return filepath.Dir(statePath), nil
}

type coordinator struct {
	runtimeDir    string
	statePath     string
	lockPath      string
	ownerVerifier ownerVerifier
}

func newCoordinator(runtimeDir string) *coordinator {
	return newCoordinatorWithVerifier(runtimeDir, verifyHTTPRouter)
}

func newCoordinatorWithVerifier(runtimeDir string, ownerVerifier ownerVerifier) *coordinator {
	if ownerVerifier == nil {
		ownerVerifier = verifyHTTPRouter
	}
	return &coordinator{
		runtimeDir:    runtimeDir,
		statePath:     filepath.Join(runtimeDir, stateFileName),
		lockPath:      filepath.Join(runtimeDir, ownerLockFileName),
		ownerVerifier: ownerVerifier,
	}
}

func (c *coordinator) RuntimeDirPath() string {
	return c.runtimeDir
}

func (c *coordinator) StateFilePath() string {
	return c.statePath
}

func (c *coordinator) Exists() bool {
	_, err := os.Stat(c.statePath)
	return err == nil
}

func (c *coordinator) Verify() verification {
	cache, err := c.read()
	if err != nil {
		if os.IsNotExist(err) {
			return verification{Status: stateMissing}
		}
		return verification{Status: stateStale}
	}
	if c.ownerVerifier(cache) {
		return verification{Status: stateActive, Cache: cache}
	}
	return verification{Status: stateStale, Cache: cache}
}

func (c *coordinator) Claim(cache stateCache) error {
	if cache.HTTPRouterListen == "" || cache.Token == "" {
		return fmt.Errorf("gateway state cache requires router listen and token")
	}
	return writeAtomicReplace(c.statePath, cache)
}

func (c *coordinator) Remove() error {
	err := os.Remove(c.statePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *coordinator) RemoveOwned(cache stateCache) error {
	if !c.Owns(cache) {
		return nil
	}
	return c.Remove()
}

func (c *coordinator) Owns(cache stateCache) bool {
	current, err := c.read()
	if err != nil {
		return false
	}
	return current.HTTPRouterListen == cache.HTTPRouterListen && current.Token == cache.Token
}

func (c *coordinator) Write(cache stateCache) error {
	return writeExclusive(c.statePath, cache)
}

func (c *coordinator) read() (stateCache, error) {
	data, err := os.ReadFile(c.statePath)
	if err != nil {
		return stateCache{}, err
	}
	var cache stateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return stateCache{}, err
	}
	return cache, nil
}

func writeExclusive(path string, cache stateCache) error {
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

func writeAtomicReplace(path string, cache stateCache) error {
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

func waitForStop(cache stateCache) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !verifyHTTPRouter(cache) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func verifyHTTPRouter(cache stateCache) bool {
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
