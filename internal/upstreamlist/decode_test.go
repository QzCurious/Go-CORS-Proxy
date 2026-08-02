package upstreamlist

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeProducesExplicitSelectorTypes(t *testing.T) {
	decoded, err := Decode([]byte(`
API.EXAMPLE.TEST
*.QA.EXAMPLE.TEST
https://Example.Test:0443
http://[::1]:3000
`))
	if err != nil {
		t.Fatal(err)
	}
	want := NewUpstreamList(NewEntries([]HostSelector{
		{Hostname: "api.example.test", HostnameMatch: HostnameExact},
		{Hostname: "qa.example.test", HostnameMatch: HostnameSingleLevel},
	}, []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: "443"},
		{Scheme: "http", Hostname: "::1", Port: "3000"},
	}), nil)
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestUpstreamListOwnsImmutableEntrySemantics(t *testing.T) {
	hosts := []HostSelector{{Hostname: "api.example.test", HostnameMatch: HostnameExact}}
	origins := []OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}}
	warnings := []Warning{{Line: 1, Text: "bad", Diagnostic: "invalid"}}
	list := NewUpstreamList(NewEntries(hosts, origins), warnings)

	hosts[0].Hostname = "mutated.example.test"
	origins[0].Hostname = "mutated.example.test"
	warnings[0].Diagnostic = "mutated"
	entries := list.Entries()
	if entries.Count() != 2 || !entries.HTTPSIntent() {
		t.Fatalf("entry semantics = count %d, https intent %t", entries.Count(), entries.HTTPSIntent())
	}
	if entries.HostSelectors()[0].Hostname != "api.example.test" ||
		entries.OriginSelectors()[0].Hostname != "secure.example.test" ||
		list.Warnings()[0].Diagnostic != "invalid" {
		t.Fatalf("list was mutated through constructor inputs: %#v", list)
	}

	gotHosts := entries.HostSelectors()
	gotHosts[0].Hostname = "another.example.test"
	if list.Entries().HostSelectors()[0].Hostname != "api.example.test" {
		t.Fatal("list was mutated through entry accessor")
	}
}

func TestHTTPSIntentIgnoresHostAndHTTPSelectors(t *testing.T) {
	list := NewUpstreamList(NewEntries(
		[]HostSelector{{Hostname: "api.example.test", HostnameMatch: HostnameExact}},
		[]OriginSelector{{Scheme: "http", Hostname: "plain.example.test"}},
	), nil)
	if list.HTTPSIntent() {
		t.Fatal("HTTP-only entries should not express HTTPS intent")
	}
}

func TestDecodeRetainsOmittedAndExplicitDefaultPortsAsDistinctSelectors(t *testing.T) {
	decoded, err := Decode([]byte(`
https://example.test
https://example.test:443
https://example.test:0443
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{
		{Scheme: "https", Hostname: "example.test"},
		{Scheme: "https", Hostname: "example.test", Port: "443"},
	}
	if !reflect.DeepEqual(decoded.Entries().OriginSelectors(), want) {
		t.Fatalf("Origin Selectors = %#v, want %#v", decoded.Entries().OriginSelectors(), want)
	}
}

func TestDecodeDeduplicatesNormalizedSelectors(t *testing.T) {
	decoded, err := Decode([]byte(`
EXAMPLE.TEST
example.test
https://EXAMPLE.TEST:0443
https://example.test:443
`))
	if err != nil {
		t.Fatal(err)
	}
	want := NewUpstreamList(NewEntries([]HostSelector{
		{Hostname: "example.test", HostnameMatch: HostnameExact},
	}, []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: "443"},
	}), nil)
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestDecodeNormalizesExplicitPortsAndPreservesOmission(t *testing.T) {
	decoded, err := Decode([]byte(`
HTTPS://user:password@EXAMPLE.TEST:0443/
https://example.test
http://example.test
http://example.test:00080
https://example.test:99999
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: "443"},
		{Scheme: "https", Hostname: "example.test"},
		{Scheme: "http", Hostname: "example.test"},
		{Scheme: "http", Hostname: "example.test", Port: "80"},
		{Scheme: "https", Hostname: "example.test", Port: "99999"},
	}
	if !reflect.DeepEqual(decoded.Entries().OriginSelectors(), want) {
		t.Fatalf("Origin Selectors = %#v, want %#v", decoded.Entries().OriginSelectors(), want)
	}
}

func TestDecodeSupportsHostSelectorHostnameMatches(t *testing.T) {
	decoded, err := Decode([]byte(`
example.test
*.qa.example.test
**.internal
[::1]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []HostSelector{
		{Hostname: "example.test", HostnameMatch: HostnameExact},
		{Hostname: "qa.example.test", HostnameMatch: HostnameSingleLevel},
		{Hostname: "internal", HostnameMatch: HostnameRecursive},
		{Hostname: "::1", HostnameMatch: HostnameExact},
	}
	if !reflect.DeepEqual(decoded.Entries().HostSelectors(), want) {
		t.Fatalf("Host Selectors = %#v, want %#v", decoded.Entries().HostSelectors(), want)
	}
}

func TestSameEntriesComparesNormalizedPortValues(t *testing.T) {
	left := NewUpstreamList(NewEntries(nil, []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: "443"},
	}), nil)
	right := NewUpstreamList(NewEntries(nil, []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: "443"},
	}), nil)
	if !SameEntries(left, right) {
		t.Fatal("equal explicit port values should identify the same entry")
	}

	right = NewUpstreamList(NewEntries(nil, []OriginSelector{
		{Scheme: "https", Hostname: "example.test"},
	}), nil)
	if SameEntries(left, right) {
		t.Fatal("omitted and explicit default ports should remain distinct entries")
	}
}

func TestDecodeRejectsNonUTF8(t *testing.T) {
	_, err := Decode([]byte{0xff})
	if err == nil || !strings.Contains(err.Error(), "content must be UTF-8") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestDecodeDiscardsOriginUserInformation(t *testing.T) {
	decoded, err := Decode([]byte("https://user:password@example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{{Scheme: "https", Hostname: "example.test"}}
	if !reflect.DeepEqual(decoded.Entries().OriginSelectors(), want) ||
		decoded.Entries().Count() != 1 ||
		len(decoded.Warnings()) != 0 {
		t.Fatalf("decode result = %#v", decoded)
	}
}

func TestDecodeCollectsEveryInvalidLine(t *testing.T) {
	decoded, err := Decode([]byte(`
valid.example.test
example.test:8080
https://bad.example.test/path
api#bad.example.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries().HostSelectors()) != 1 ||
		decoded.Entries().HostSelectors()[0].Hostname != "valid.example.test" {
		t.Fatalf("Host Selectors = %#v", decoded.Entries().HostSelectors())
	}
	if len(decoded.Warnings()) != 3 ||
		decoded.Warnings()[0].Line != 3 ||
		decoded.Warnings()[1].Line != 4 ||
		decoded.Warnings()[2].Line != 5 {
		t.Fatalf("warnings = %#v", decoded.Warnings())
	}
}

func TestDecodeWarnsForUnsupportedSelectorShapes(t *testing.T) {
	decoded, err := Decode([]byte(`
example.test:8080
example.test/
example.test?query
example.test?
//example.test
ftp://example.test
https://example.test/path
https://example.test?query
https://example.test?
https://example.test#fragment
https://example.test#
*
**
https:///missing-host
`))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Count() != 0 || len(decoded.Warnings()) != 14 {
		t.Fatalf("decoded = %#v", decoded)
	}
	for idx, warning := range decoded.Warnings() {
		wantLine := idx + 2
		if warning.Line != wantLine || warning.Text == "" || warning.Diagnostic == "" {
			t.Fatalf("warning %d = %#v, want source line %d", idx, warning, wantLine)
		}
	}
}

func TestDecodeWarnsForEmptyOriginPorts(t *testing.T) {
	decoded, err := Decode([]byte(`
https://example.test:
http://[::1]:
valid.example.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries().OriginSelectors()) != 0 ||
		len(decoded.Entries().HostSelectors()) != 1 ||
		len(decoded.Warnings()) != 2 {
		t.Fatalf("decoded = %#v", decoded)
	}
	for _, warning := range decoded.Warnings() {
		if !strings.Contains(warning.Diagnostic, "port must not be empty") {
			t.Fatalf("warning = %#v", warning)
		}
	}
}

func TestDecodeRequiresASCIIOrPunycodeHostname(t *testing.T) {
	decoded, err := Decode([]byte("例.example\nxn--fsq.example\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []HostSelector{
		{Hostname: "xn--fsq.example", HostnameMatch: HostnameExact},
	}
	if len(decoded.Warnings()) != 1 || !reflect.DeepEqual(decoded.Entries().HostSelectors(), want) {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeRemovesInlineCommentsAndRejectsNonHostnameText(t *testing.T) {
	decoded, err := Decode([]byte("api#bad.example.test\napi.example.test # staging\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries().HostSelectors()) != 1 ||
		decoded.Entries().HostSelectors()[0].Hostname != "api.example.test" {
		t.Fatalf("Host Selectors = %#v", decoded.Entries().HostSelectors())
	}
	if len(decoded.Warnings()) != 1 ||
		decoded.Warnings()[0].Line != 1 ||
		decoded.Warnings()[0].Text != "api#bad.example.test" ||
		!strings.Contains(decoded.Warnings()[0].Diagnostic, "only a hostname") {
		t.Fatalf("warnings = %#v", decoded.Warnings())
	}
}

func TestOriginSelectorDoesNotApplyHostWildcardSemantics(t *testing.T) {
	decoded, err := Decode([]byte("https://*.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{{Scheme: "https", Hostname: "*.example.test"}}
	if !reflect.DeepEqual(decoded.Entries().OriginSelectors(), want) ||
		len(decoded.Entries().HostSelectors()) != 0 ||
		len(decoded.Warnings()) != 0 {
		t.Fatalf("decode result = %#v", decoded)
	}
}
