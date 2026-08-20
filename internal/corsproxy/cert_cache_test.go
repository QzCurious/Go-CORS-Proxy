package corsproxy

import (
	"crypto/tls"
	"testing"
)

func TestCertificateCacheReusesAndEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newCertificateCache(2)
	generated := map[string]int{}
	fetch := func(hostname string) *tls.Certificate {
		t.Helper()
		certificate, err := cache.Fetch(hostname, func() (*tls.Certificate, error) {
			generated[hostname]++
			return &tls.Certificate{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return certificate
	}

	firstA := fetch("a.example")
	fetch("b.example")
	if secondA := fetch("a.example"); secondA != firstA {
		t.Fatal("cache did not reuse the certificate")
	}
	fetch("c.example")
	fetch("b.example")

	if generated["a.example"] != 1 || generated["b.example"] != 2 || generated["c.example"] != 1 {
		t.Fatalf("generation counts = %#v", generated)
	}
}

func TestCertificateCacheDoesNotStoreGenerationFailure(t *testing.T) {
	cache := newCertificateCache(1)
	calls := 0
	generate := func() (*tls.Certificate, error) {
		calls++
		if calls == 1 {
			return nil, errTestCertificateGeneration
		}
		return &tls.Certificate{}, nil
	}
	if _, err := cache.Fetch("a.example", generate); err == nil {
		t.Fatal("generation failure was hidden")
	}
	if _, err := cache.Fetch("a.example", generate); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("generation calls = %d", calls)
	}
}

type testCertificateGenerationError struct{}

func (testCertificateGenerationError) Error() string { return "certificate generation failed" }

var errTestCertificateGeneration testCertificateGenerationError
