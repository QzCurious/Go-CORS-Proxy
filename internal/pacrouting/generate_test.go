package pacrouting

import (
	"strings"
	"testing"
)

func TestGenerateUsesTrustAwareHTTPSRouting(t *testing.T) {
	js := Generate(Options{
		ProxyListen:       "127.0.0.1:8080",
		CATrusted:         false,
		DomainListEntries: mustParseEntries(t, "https://api.example.test", "http://api.example.test"),
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

func TestGenerateUsesExactPortsForFullOrigins(t *testing.T) {
	js := Generate(Options{
		ProxyListen:       "127.0.0.1:8080",
		CATrusted:         false,
		DomainListEntries: mustParseEntries(t, "http://api.example.test:8081"),
	})
	if !strings.Contains(js, `"port":"8081"`) {
		t.Fatalf("PAC should preserve full-origin port, got:\n%s", js)
	}
}
