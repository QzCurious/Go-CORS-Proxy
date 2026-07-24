package liveconfig_test

import (
	"path/filepath"
	"strings"
	"testing"

	"seamless-cors/internal/liveconfig"
)

func TestLoadSupportsDomainListCommentsAndNormalization(t *testing.T) {
	snapshot := loadDomainList(t, `
# staging
api.example.test # exact
API.EXAMPLE.TEST
*.qa.example.test
https://localhost:9443
`)
	entries := snapshot.DomainListEntries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Hostname() != "api.example.test" || entries[0].Scheme() != "" || entries[0].Port() != "" {
		t.Fatalf("hostname shorthand entry = %v", entries[0])
	}
	if !entries[1].IsWildcard() || entries[1].Hostname() != "*.qa.example.test" {
		t.Fatalf("wildcard entry = %v", entries[1])
	}
	if entries[2].Scheme() != "https" || entries[2].Hostname() != "localhost" || entries[2].Port() != "9443" {
		t.Fatalf("full origin entry = %v", entries[2])
	}
}

func TestLoadRequiresFullOriginForIPv6DomainListEntry(t *testing.T) {
	_, err := tryLoadDomainList(t, "::1\n")
	if err == nil {
		t.Fatal("expected IPv6 shorthand to fail")
	}

	snapshot := loadDomainList(t, "http://[::1]:3000\n")
	entries := snapshot.DomainListEntries()
	if len(entries) != 1 ||
		entries[0].Scheme() != "http" ||
		entries[0].Hostname() != "::1" ||
		entries[0].Port() != "3000" {
		t.Fatalf("full IPv6 origin entries = %v", entries)
	}
}

func TestLoadReportsInvalidInlineDomainListComment(t *testing.T) {
	_, err := tryLoadDomainList(t, "api#bad.example.test\napi.example.test # staging\n")
	if err == nil || !strings.Contains(err.Error(), "line 1: api#bad.example.test") {
		t.Fatalf("load error = %v", err)
	}
}

func loadDomainList(t *testing.T, contents string) liveconfig.Snapshot {
	t.Helper()
	snapshot, err := tryLoadDomainList(t, contents)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func tryLoadDomainList(t *testing.T, contents string) (liveconfig.Snapshot, error) {
	t.Helper()
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, contents)
	writeConfig(t, configPath, domainPath, false)
	return liveconfig.LoadExisting(configPath)
}
