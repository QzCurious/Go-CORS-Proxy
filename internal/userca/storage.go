package userca

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func readActiveFingerprint(dir string) (string, error) {
	data, err := readFile(filepath.Join(dir, activeFingerprintFileName))
	if err != nil {
		return "", err
	}
	fingerprint := strings.TrimSpace(string(data))
	if !validFingerprint(fingerprint) {
		return "", errInvalidActiveFingerprint
	}
	return fingerprint, nil
}

// writeActiveFingerprint atomically commits a generation. The bool reports
// whether rename completed even when the following durability sync fails.
func writeActiveFingerprint(dir, fingerprint string) (bool, error) {
	if !validFingerprint(fingerprint) {
		return false, fmt.Errorf("active UserCA fingerprint is invalid")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(dir, ".active-fingerprint-*")
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return false, err
	}
	if _, err := temp.WriteString(fingerprint + "\n"); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, filepath.Join(dir, activeFingerprintFileName)); err != nil {
		return false, err
	}
	if err := syncDirectory(dir); err != nil {
		return true, err
	}
	return true, nil
}

func writeDurableFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

var syncDirectory = syncDirectoryPlatform

func syncDirectoryPlatform(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func hasOwnedGenerations(dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, activeFingerprintFileName)); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, authoritiesDirName))
	if err == nil {
		return len(entries) > 0, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func uninstallAll(ctx context.Context, dir string, store trustStore) error {
	records, trustErr := store.TrustedCertificates(ctx)
	var removeErr error
	if trustErr == nil {
		if owned := fingerprints(records); len(owned) > 0 {
			removeErr = store.Remove(ctx, owned)
		}
	}
	return errors.Join(trustErr, removeErr, os.RemoveAll(dir))
}

func cleanupNonActive(ctx context.Context, dir string, store trustStore, active string) error {
	// Phase 1: remove non-Active strict-footprint trust.
	records, err := store.TrustedCertificates(ctx)
	if err != nil {
		return err
	}
	remove := make([]string, 0, len(records))
	for _, record := range records {
		if record.Fingerprint != active {
			remove = append(remove, record.Fingerprint)
		}
	}
	if len(remove) > 0 {
		if err := store.Remove(ctx, remove); err != nil {
			return err
		}
	}

	// Phase 2: remove non-Active immutable local generations.
	authoritiesDir := filepath.Join(dir, authoritiesDirName)
	entries, err := os.ReadDir(authoritiesDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if err := os.RemoveAll(filepath.Join(authoritiesDir, entry.Name())); err != nil {
			return err
		}
	}

	// Phase 3: verify cleanup before another Candidate may be added.
	return verifyNonActiveCleared(ctx, dir, store, active)
}

func verifyNonActiveCleared(ctx context.Context, dir string, store trustStore, active string) error {
	records, err := store.TrustedCertificates(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Fingerprint != active {
			return fmt.Errorf("non-active UserCA trust remains after cleanup")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, authoritiesDirName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.Name() != active {
			return fmt.Errorf("non-active UserCA material remains after cleanup")
		}
	}
	return nil
}

func cleanupCandidate(ctx context.Context, dir string, store trustStore, fingerprint string) error {
	return errors.Join(
		store.Remove(ctx, []string{fingerprint}),
		os.RemoveAll(filepath.Join(dir, authoritiesDirName, fingerprint)),
	)
}

func containsFingerprint(records []trustedCertificate, fingerprint string) bool {
	for _, record := range records {
		if record.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

func fingerprints(records []trustedCertificate) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Fingerprint)
	}
	return out
}

func validFingerprint(value string) bool {
	if len(value) != sha1.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToUpper(value)
}

var chmod = os.Chmod

func authorityPermissionsNeedRepair(dir string, authority *authority) bool {
	expected := map[string]os.FileMode{
		dir:                                    0o700,
		filepath.Join(dir, authoritiesDirName): 0o700,
		filepath.Dir(authority.certPath):       0o700,
		filepath.Join(dir, activeFingerprintFileName): 0o600,
		authority.certPath:                            0o600,
		authority.keyPath:                             0o600,
	}
	for path, mode := range expected {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			return true
		}
	}
	return false
}

func repairAuthorityPermissions(dir string, authority *authority) error {
	return errors.Join(
		chmod(dir, 0o700),
		chmod(filepath.Join(dir, authoritiesDirName), 0o700),
		chmod(filepath.Dir(authority.certPath), 0o700),
		chmod(filepath.Join(dir, activeFingerprintFileName), 0o600),
		chmod(authority.certPath, 0o600),
		chmod(authority.keyPath, 0o600),
	)
}
