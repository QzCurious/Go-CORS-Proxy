package userca

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAssessmentProviderIssuesAndCachesLeavesByGeneration(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(t.TempDir(), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := result.Current().Provider()
	if !ok {
		t.Fatal("install omitted usable provider")
	}
	if got, want := provider.ValidUntil(), result.Current().ExpiresAt(); !got.Equal(want) {
		t.Fatalf("provider deadline = %s, want assessment expiry %s", got, want)
	}

	first, err := provider.CertificateFor("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.CertificateFor("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Fatal("provider did not reuse the generation-scoped leaf")
	}

	now = now.Add(25 * time.Hour)
	third, err := provider.CertificateFor("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Certificate[0]) == string(third.Certificate[0]) {
		t.Fatal("provider reused a leaf beyond its cache age")
	}
}

func TestProviderClassifiesExpiryAndInvalidRequest(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(t.TempDir(), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := result.Current().Provider()
	if !ok {
		t.Fatal("install omitted usable provider")
	}

	if _, err := provider.CertificateFor("bad host"); err == nil || providerDisposition(err) != ProvisioningInvalidRequest {
		t.Fatalf("invalid request error = %v", err)
	}
	now = provider.ValidUntil()
	if _, err := provider.CertificateFor("api.example.test"); err == nil || providerDisposition(err) != ProvisioningExpired || !errors.Is(err, ErrProviderExpired) {
		t.Fatalf("expired provider error = %v", err)
	}
}

func providerDisposition(err error) ProvisioningDisposition {
	var classified interface{ Disposition() string }
	if !errors.As(err, &classified) {
		return ""
	}
	return ProvisioningDisposition(classified.Disposition())
}
