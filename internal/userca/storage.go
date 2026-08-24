package userca

import (
	"context"
	"errors"
	"os"
)

func hasOwnedMaterial(dir string) (bool, error) {
	if _, err := os.Stat(dir); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}

func uninstallAll(ctx context.Context, dir string, store trustStore) error {
	records, trustErr := store.trustedCertificates(ctx)
	var removeErr error
	if trustErr == nil {
		if owned := fingerprints(records); len(owned) > 0 {
			removeErr = store.remove(ctx, owned)
		}
	}
	return errors.Join(trustErr, removeErr, os.RemoveAll(dir))
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

var chmod = os.Chmod

func authorityPermissionsNeedRepair(dir string, authority *authority) bool {
	expected := map[string]os.FileMode{
		dir:                0o700,
		authority.certPath: 0o600,
		authority.keyPath:  0o600,
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
		chmod(authority.certPath, 0o600),
		chmod(authority.keyPath, 0o600),
	)
}
