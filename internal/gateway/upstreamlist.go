package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/adrg/xdg"
)

const upstreamListFileName = "upstreams.txt"

// defaultGlobalUpstreamListPath is application policy. It has no filesystem
// side effects and intentionally does not resolve symlinks.
func defaultGlobalUpstreamListPath() string {
	return globalUpstreamListPath(xdg.ConfigHome)
}

func globalUpstreamListPath(configHome string) string {
	return filepath.Clean(filepath.Join(configHome, "seamless-cors", upstreamListFileName))
}

func directoryUpstreamListPath(workingDirectory string) (string, error) {
	if workingDirectory == "" {
		return "", fmt.Errorf("working directory is required")
	}
	if !filepath.IsAbs(workingDirectory) {
		return "", fmt.Errorf("working directory must be absolute: %q", workingDirectory)
	}
	return filepath.Join(filepath.Clean(workingDirectory), upstreamListFileName), nil
}

func assessUpstreamListCreation(path string) *UpstreamListCreationConsent {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil
	}
	consent := &UpstreamListCreationConsent{
		Path:            path,
		DefaultContents: upstreamlist.DefaultContents,
		Fingerprint:     upstreamListCreationFingerprint(path, upstreamlist.DefaultContents),
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(parent); err == nil {
			break
		}
		consent.MissingParentDirectories = append(consent.MissingParentDirectories, parent)
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
	}
	return consent
}

func upstreamListCreationFingerprint(path, contents string) UpstreamListCreationFingerprint {
	sum := sha256.Sum256([]byte(path + "\x00" + contents))
	return UpstreamListCreationFingerprint(hex.EncodeToString(sum[:]))
}

func createUpstreamList(path string) error {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("create upstream list %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create upstream list %q: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create upstream list %q: %w", path, err)
	}
	if _, err := file.WriteString(upstreamlist.DefaultContents); err != nil {
		_ = file.Close()
		return fmt.Errorf("create upstream list %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("create upstream list %q: %w", path, err)
	}
	return nil
}
