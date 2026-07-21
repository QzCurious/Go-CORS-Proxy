package pacrouting

import (
	"strings"
	"testing"

	"seamless-cors/internal/domainlist"
)

func TestGenerateUsesTrustAwareHTTPSRouting(t *testing.T) {
	httpsEntry, err := domainlist.ParseEntry("https://api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	httpEntry, err := domainlist.ParseEntry("http://api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   false,
		Entries:     []domainlist.Entry{httpsEntry, httpEntry},
	})
	if strings.Contains(js, "scheme == 'https' && host == 'api.example.test'") {
		t.Fatal("HTTPS route should be omitted when CA is not trusted")
	}
	if !strings.Contains(js, `var origins = [{"scheme":"http","host":"api.example.test","port":"80"}]`) {
		t.Fatalf("HTTP origin route should be present, got:\n%s", js)
	}
	if !strings.Contains(js, "DIRECT") {
		t.Fatal("PAC should return DIRECT for unmatched traffic")
	}
}

func TestPolicyMatchesDomainListEntries(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"API.EXAMPLE.TEST",
		"*.qa.example.test",
		"https://localhost:9443",
		"https://api.example.test",
		"http://[::1]:3000",
	)
	policy := NewPolicy(entries)

	if !policy.Matches("https", "api.example.test", "443") {
		t.Fatal("hostname shorthand should match any scheme and port")
	}
	if !policy.Matches("http", "API.EXAMPLE.TEST", "3333") {
		t.Fatal("matching should ignore host case")
	}
	if !policy.Matches("https", "one.qa.example.test", "443") {
		t.Fatal("wildcard should match one label")
	}
	if policy.Matches("https", "two.one.qa.example.test", "443") {
		t.Fatal("wildcard should not match two labels")
	}
	if !policy.Matches("https", "localhost", "9443") {
		t.Fatal("full origin should match exact scheme host port")
	}
	if policy.Matches("https", "localhost", "443") {
		t.Fatal("full origin matched the wrong port")
	}
	if !policy.Matches("http", "::1", "3000") {
		t.Fatal("full IPv6 origin should match")
	}

	originOnly := NewPolicy(mustParseEntries(t, "https://origin-only.example.test"))
	if !originOnly.Matches("https", "origin-only.example.test", "443") {
		t.Fatal("https origin should match the default HTTPS port")
	}
	if originOnly.Matches("http", "origin-only.example.test", "80") {
		t.Fatal("https origin matched a different scheme")
	}
}

func TestGenerateUsesExactPortsForFullOrigins(t *testing.T) {
	entry, err := domainlist.ParseEntry("http://api.example.test:8081")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   false,
		Entries:     []domainlist.Entry{entry},
	})
	if !strings.Contains(js, `"port":"8081"`) {
		t.Fatalf("PAC should preserve full-origin port, got:\n%s", js)
	}
}

func TestRouteSetFingerprintIgnoresOrderAndDuplicates(t *testing.T) {
	first := mustParseEntries(t,
		"api.example.test",
		"http://one.example.test:8080",
		"*.qa.example.test",
		"api.example.test",
	)
	second := mustParseEntries(t,
		"*.qa.example.test",
		"api.example.test",
		"http://one.example.test:8080",
	)

	if RouteSetFingerprint(first, true) != RouteSetFingerprint(second, true) {
		t.Fatal("route set fingerprint should ignore order and duplicates")
	}
	if RouteSetFingerprint(first, true) == RouteSetFingerprint(first, false) {
		t.Fatal("route set fingerprint should include trust-aware HTTPS routing")
	}
}

func mustParseEntries(t *testing.T, texts ...string) []domainlist.Entry {
	t.Helper()
	entries := make([]domainlist.Entry, 0, len(texts))
	for _, text := range texts {
		entry, err := domainlist.ParseEntry(text)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}
