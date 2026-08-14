package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

// defaultUpstreamListPath is application policy. The resulting path is
// absolute and cleaned, but intentionally not resolved through symlinks; the
// file observation module enforces ordinary-file safety when it reads the path.
func defaultUpstreamListPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(home, ".seamless-cors", "upstreams.txt"))
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
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
