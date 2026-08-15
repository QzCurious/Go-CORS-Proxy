package userca

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProviderSourceProjectsBoundedSelectorCertificates(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(t.TempDir(), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, ok := result.Current().Source()
	if !ok {
		t.Fatal("install omitted usable provider source")
	}
	provider, err := source.Project(context.Background(), upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{
			{Hostname: "api.example.test"},
			{Hostname: "example.test", Wildcard: true},
		},
		OriginSelectors: []upstreamlist.OriginSelector{
			{Scheme: "https", Hostname: "secure.example.test", Port: "8443"},
			{Scheme: "http", Hostname: "plain.other.test"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := provider.ValidUntil(), result.Current().ExpiresAt(); !got.Equal(want) {
		t.Fatalf("provider deadline = %s, want assessment expiry %s", got, want)
	}

	exact, err := provider.CertificateFor("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	wildcard, err := provider.CertificateFor("qa.example.test")
	if err != nil {
		t.Fatal(err)
	}
	secure, err := provider.CertificateFor("secure.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if exact.PrivateKey != wildcard.PrivateKey || exact.PrivateKey != secure.PrivateKey {
		t.Fatal("Selector Certificates did not share the provider leaf key")
	}
	if !exact.Leaf.NotAfter.Equal(result.Current().ExpiresAt()) ||
		!wildcard.Leaf.NotAfter.Equal(result.Current().ExpiresAt()) {
		t.Fatal("Selector Certificate expiry did not match Active UserCA expiry")
	}
	if err := exact.Leaf.VerifyHostname("api.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := wildcard.Leaf.VerifyHostname("qa.example.test"); err != nil {
		t.Fatal(err)
	}
	for _, hostname := range []string{"deep.qa.example.test", "plain.other.test", "other.test"} {
		if _, err := provider.CertificateFor(hostname); err == nil || providerDisposition(err) != ProvisioningNotCovered {
			t.Fatalf("lookup %q error = %v", hostname, err)
		}
	}
}

func TestProviderPrefersExactCertificateOverCoveringWildcard(t *testing.T) {
	ca := openAt(t.TempDir(), &fakeTrustStore{}, time.Now)
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, _ := result.Current().Source()
	provider, err := source.Project(context.Background(), upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{
		{Hostname: "api.example.test"},
		{Hostname: "example.test", Wildcard: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := provider.CertificateFor("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(certificate.Leaf.DNSNames) != 1 || certificate.Leaf.DNSNames[0] != "api.example.test" {
		t.Fatalf("selected certificate DNS names = %#v", certificate.Leaf.DNSNames)
	}
}

func TestEmptyProviderIsValidAndCoversNothing(t *testing.T) {
	ca := openAt(t.TempDir(), &fakeTrustStore{}, time.Now)
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, _ := result.Current().Source()
	provider, err := source.Project(context.Background(), upstreamlist.Projection{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.CertificateFor("api.example.test"); err == nil || providerDisposition(err) != ProvisioningNotCovered {
		t.Fatalf("empty provider lookup error = %v", err)
	}
}

func TestProviderClassifiesExpiryInvalidRequestAndCancellation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca := openAt(t.TempDir(), &fakeTrustStore{}, func() time.Time { return now })
	result, err := ca.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, _ := result.Current().Source()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Project(ctx, upstreamlist.Projection{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled projection error = %v", err)
	}
	provider, err := source.Project(context.Background(), upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{{
		Hostname: "api.example.test",
	}}})
	if err != nil {
		t.Fatal(err)
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
