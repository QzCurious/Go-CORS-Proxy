package userca

import (
	"context"
	"errors"
	"os"

	"github.com/QzCurious/seamless-cors/internal/lib/truststore"
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
	records, trustErr := store.List(ctx)
	var removeErr error
	if trustErr == nil {
		if owned := fingerprints(ownedCertificates(records)); len(owned) > 0 {
			removeErr = store.Remove(ctx, owned)
		}
	}
	return errors.Join(trustErr, removeErr, os.RemoveAll(dir))
}

func ownedCertificates(records []truststore.Certificate) []truststore.Certificate {
	owned := make([]truststore.Certificate, 0, len(records))
	for _, record := range records {
		if record.X509 != nil && isOwnedAuthorityCertificate(record.X509) {
			owned = append(owned, record)
		}
	}
	return owned
}

func fingerprints(records []truststore.Certificate) []string {
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
