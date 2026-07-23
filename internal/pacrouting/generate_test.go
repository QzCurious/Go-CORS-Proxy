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
