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
	if entries[1].Hostname != "qa.example.test" || entries[1].HostMatch != liveconfig.HostSingleLevel {
		t.Fatalf("wildcard entry = %v", entries[1])
	}
	if entries[2].Scheme != "https" || entries[2].Hostname != "localhost" || entries[2].Port != "9443" {
		t.Fatalf("full origin entry = %v", entries[2])
	}
}

func TestLoadProjectsURLIntoDomainListEntry(t *testing.T) {
	snapshot := loadDomainList(t, "HTTPS://user:password@*.EXAMPLE.TEST.:0443/\n")

	entries := snapshot.DomainListEntries()
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	want := liveconfig.DomainListEntry{
		Scheme:    "https",
		Hostname:  "example.test",
		Port:      "0443",
		HostMatch: liveconfig.HostSingleLevel,
	}
	if entries[0] != want {
		t.Fatalf("entry = %#v, want %#v", entries[0], want)
	}
}

func TestLoadSupportsDomainSelectorForms(t *testing.T) {
	snapshot := loadDomainList(t, `
example.test
example.test:8080
//example.test:8081
**.internal
https://**.dev.example.test
[::1]:3000
`)

	want := []liveconfig.DomainListEntry{
		{Hostname: "example.test", HostMatch: liveconfig.HostExact},
		{Hostname: "example.test", Port: "8080", HostMatch: liveconfig.HostExact},
		{Hostname: "example.test", Port: "8081", HostMatch: liveconfig.HostExact},
		{Hostname: "internal", HostMatch: liveconfig.HostRecursive},
		{Scheme: "https", Hostname: "dev.example.test", HostMatch: liveconfig.HostRecursive},
		{Hostname: "::1", Port: "3000", HostMatch: liveconfig.HostExact},
	}
	entries := snapshot.DomainListEntries()
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v", entries)
	}
	for idx := range want {
		if entries[idx] != want[idx] {
			t.Fatalf("entry %d = %#v, want %#v", idx, entries[idx], want[idx])
		}
	}
}

func TestLoadDeduplicatesNormalizedURLProjection(t *testing.T) {
	snapshot := loadDomainList(t, `
HTTPS://user@EXAMPLE.TEST./
https://example.test?#
`)

	entries := snapshot.DomainListEntries()
	if len(entries) != 1 ||
		entries[0] != (liveconfig.DomainListEntry{
			Scheme:    "https",
			Hostname:  "example.test",
			HostMatch: liveconfig.HostExact,
		}) {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestLoadWarnsForEveryFilteredDomainSelector(t *testing.T) {
	snapshot := loadDomainList(t, `
ftp://example.test
https://example.test/path
https://example.test?query
https://example.test#fragment
*
**
https:///missing-host
`)

	warnings := snapshot.DomainListWarnings()
	if len(snapshot.DomainListEntries()) != 0 || len(warnings) != 7 {
		t.Fatalf("snapshot entries = %#v, warnings = %#v", snapshot.DomainListEntries(), warnings)
	}
	for idx, warning := range warnings {
		wantLine := idx + 2
		if warning.Line != wantLine || warning.Text == "" || warning.Diagnostic == "" {
			t.Fatalf("warning %d = %#v, want source line %d", idx, warning, wantLine)
		}
	}
}

func TestLoadRequiresBracketsForIPv6DomainListEntry(t *testing.T) {
	invalid := loadDomainList(t, "::1\n")
	if len(invalid.DomainListEntries()) != 0 ||
		len(invalid.DomainListWarnings()) != 1 {
		t.Fatalf("invalid IPv6 shorthand snapshot = %#v", invalid)
	}

	snapshot := loadDomainList(t, "[::1]:3000\n")
	entries := snapshot.DomainListEntries()
	if len(entries) != 1 ||
		entries[0].Scheme != "" ||
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
		!strings.Contains(warnings[0].Diagnostic, "fragment") {
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
