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
	if entries[0].Hostname != "api.example.test" || entries[0].Scheme != "" || entries[0].Port != "" {
		t.Fatalf("hostname shorthand entry = %v", entries[0])
	}
	if entries[1].Hostname != "*.qa.example.test" {
		t.Fatalf("wildcard entry = %v", entries[1])
	}
	if entries[2].Scheme != "https" || entries[2].Hostname != "localhost" || entries[2].Port != "9443" {
		t.Fatalf("full origin entry = %v", entries[2])
	}
}

func TestLoadRequiresFullOriginForIPv6DomainListEntry(t *testing.T) {
	invalid := loadDomainList(t, "::1\n")
	if len(invalid.DomainListEntries()) != 0 ||
		len(invalid.DomainListWarnings()) != 1 ||
		!strings.Contains(invalid.DomainListWarnings()[0].Diagnostic, "IPv6") {
		t.Fatalf("invalid IPv6 shorthand snapshot = %#v", invalid)
	}

	snapshot := loadDomainList(t, "http://[::1]:3000\n")
	entries := snapshot.DomainListEntries()
	if len(entries) != 1 ||
		entries[0].Scheme != "http" ||
		entries[0].Hostname != "::1" ||
		entries[0].Port != "3000" {
		t.Fatalf("full IPv6 origin entries = %v", entries)
	}
}

func TestLoadWarnsAboutInvalidLineAndUsesValidEntries(t *testing.T) {
	snapshot := loadDomainList(t, "api#bad.example.test\napi.example.test # staging\n")
	entries := snapshot.DomainListEntries()
	warnings := snapshot.DomainListWarnings()
	if len(entries) != 1 || entries[0].Hostname != "api.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if len(warnings) != 1 ||
		warnings[0].Line != 1 ||
		warnings[0].Text != "api#bad.example.test" ||
		!strings.Contains(warnings[0].Diagnostic, "invalid characters") {
		t.Fatalf("warnings = %#v", warnings)
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
