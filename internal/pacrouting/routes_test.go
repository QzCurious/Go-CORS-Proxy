package pacrouting

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"seamless-cors/internal/liveconfig"
)

func TestDeriveRoutesUsesNormalizedDomainListEntries(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"*.qa.example.test",
		"https://localhost:9443",
		"http://[::1]:3000",
	)

	got := deriveRoutes(entries, true)
	want := []route{
		{Scheme: "http", Host: "api.example.test", Match: "exact"},
		{Scheme: "https", Host: "api.example.test", Match: "exact"},
		{Scheme: "http", Host: "qa.example.test", Match: "single"},
		{Scheme: "https", Host: "qa.example.test", Match: "single"},
		{Scheme: "https", Host: "localhost", Port: "9443", Match: "exact"},
		{Scheme: "http", Host: "::1", Port: "3000", Match: "exact"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
}

func TestDeriveRoutesExcludesUntrustedHTTPS(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"https://secure.example.test",
		"http://plain.example.test",
	)

	got := deriveRoutes(entries, false)
	want := []route{
		{Scheme: "http", Host: "api.example.test", Match: "exact"},
		{Scheme: "http", Host: "plain.example.test", Port: "80", Match: "exact"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
}

func mustParseEntries(t *testing.T, texts ...string) []liveconfig.DomainListEntry {
	t.Helper()
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(domainPath, []byte(strings.Join(texts, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("domain-list: "+domainPath+"\nca-trusted: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := liveconfig.LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.DomainListEntries()
}
