// Package gatewaytest provides black-box fixtures for gateway command tests.
package gatewaytest

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const stateFileName = "gateway-state-cache.json"

func StatePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, stateFileName)
}

func RemoveState(runtimeDir string) error {
	return os.Remove(StatePath(runtimeDir))
}

func WriteStaleState(runtimeDir string) (string, error) {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]string{
		"httpRouterListen": "127.0.0.1:1",
		"token":            "stale-token",
	})
	if err != nil {
		return "", err
	}
	path := StatePath(runtimeDir)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
