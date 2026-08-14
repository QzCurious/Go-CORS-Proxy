package upstreamlist

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeProducesExplicitSelectorTypes(t *testing.T) {
	decoded, err := decode([]byte(`
API.EXAMPLE.TEST
*.QA.EXAMPLE.TEST
https://Example.Test:0443
http://[::1]:3000
`))
	if err != nil {
		t.Fatal(err)
	}
	want := parsedUpstreamList{
		HostSelectors: []HostSelector{
			{Hostname: "api.example.test", HostnameMatch: HostnameExact},
			{Hostname: "qa.example.test", HostnameMatch: HostnameSingleLevel},
		},
		OriginSelectors: []OriginSelector{
			{Scheme: "https", Hostname: "example.test", Port: "443"},
			{Scheme: "http", Hostname: "::1", Port: "3000"},
		},
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestProjectionExposesFields(t *testing.T) {
	hosts := []HostSelector{{Hostname: "api.example.test", HostnameMatch: HostnameExact}}
	origins := []OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}}
	warnings := []Warning{{Line: 1, Text: "bad", Diagnostic: "invalid"}}
	list := Projection{HostSelectors: hosts, OriginSelectors: origins, Warnings: warnings}

	hosts[0].Hostname = "mutated.example.test"
	origins[0].Hostname = "mutated.example.test"
	warnings[0].Diagnostic = "mutated"
	if len(list.HostSelectors)+len(list.OriginSelectors) != 2 || !list.HTTPSIntent() {
		t.Fatalf("entry semantics = count %d, https intent %t", len(list.HostSelectors)+len(list.OriginSelectors), list.HTTPSIntent())
	}
	if list.HostSelectors[0].Hostname != "mutated.example.test" ||
		list.OriginSelectors[0].Hostname != "mutated.example.test" ||
		list.Warnings[0].Diagnostic != "mutated" {
		t.Fatalf("exported fields did not expose the supplied values: %#v", list)
	}

	gotHosts := list.HostSelectors
	gotHosts[0].Hostname = "another.example.test"
	if list.HostSelectors[0].Hostname != "another.example.test" {
		t.Fatal("exported entry fields did not expose the stored entries")
	}
}

func TestHTTPSIntentIgnoresHostAndHTTPSelectors(t *testing.T) {
	list := Projection{
		HostSelectors:   []HostSelector{{Hostname: "api.example.test", HostnameMatch: HostnameExact}},
		OriginSelectors: []OriginSelector{{Scheme: "http", Hostname: "plain.example.test"}},
	}
	if list.HTTPSIntent() {
		t.Fatal("HTTP-only entries should not express HTTPS intent")
	}
}

func TestDecodeRetainsOmittedAndExplicitDefaultPortsAsDistinctSelectors(t *testing.T) {
	decoded, err := decode([]byte(`
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
		{Scheme: "https", Hostname: "example.test", Port: "443"},
	}
	if !reflect.DeepEqual(decoded.OriginSelectors, want) {
		t.Fatalf("Origin Selectors = %#v, want %#v", decoded.OriginSelectors, want)
	}
}

func TestDecodeNormalizesExplicitPortsAndPreservesOmission(t *testing.T) {
	decoded, err := decode([]byte(`
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
	if !reflect.DeepEqual(decoded.OriginSelectors, want) {
		t.Fatalf("Origin Selectors = %#v, want %#v", decoded.OriginSelectors, want)
	}
}

func TestDecodeSupportsHostSelectorHostnameMatches(t *testing.T) {
	decoded, err := decode([]byte(`
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
	if !reflect.DeepEqual(decoded.HostSelectors, want) {
		t.Fatalf("Host Selectors = %#v, want %#v", decoded.HostSelectors, want)
	}
}

func TestDecodeRejectsNonUTF8(t *testing.T) {
	_, err := decode([]byte{0xff})
	if err == nil || !strings.Contains(err.Error(), "content must be UTF-8") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestDecodeDiscardsOriginUserInformation(t *testing.T) {
	decoded, err := decode([]byte("https://user:password@example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{{Scheme: "https", Hostname: "example.test"}}
	if !reflect.DeepEqual(decoded.OriginSelectors, want) ||
		len(decoded.HostSelectors)+len(decoded.OriginSelectors) != 1 ||
		len(decoded.Warnings) != 0 {
		t.Fatalf("decode result = %#v", decoded)
	}
}

func TestDecodeCollectsEveryInvalidLine(t *testing.T) {
	decoded, err := decode([]byte(`
valid.example.test
example.test:8080
https://bad.example.test/path
api#bad.example.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.HostSelectors) != 1 ||
		decoded.HostSelectors[0].Hostname != "valid.example.test" {
		t.Fatalf("Host Selectors = %#v", decoded.HostSelectors)
	}
	if len(decoded.Warnings) != 3 ||
		decoded.Warnings[0].Line != 3 ||
		decoded.Warnings[1].Line != 4 ||
		decoded.Warnings[2].Line != 5 {
		t.Fatalf("warnings = %#v", decoded.Warnings)
	}
}

func TestDecodeWarnsForUnsupportedSelectorShapes(t *testing.T) {
	decoded, err := decode([]byte(`
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
	if len(decoded.HostSelectors)+len(decoded.OriginSelectors) != 0 || len(decoded.Warnings) != 14 {
		t.Fatalf("decoded = %#v", decoded)
	}
	for idx, warning := range decoded.Warnings {
		wantLine := idx + 2
		if warning.Line != wantLine || warning.Text == "" || warning.Diagnostic == "" {
			t.Fatalf("warning %d = %#v, want source line %d", idx, warning, wantLine)
		}
	}
}

func TestDecodeWarnsForEmptyOriginPorts(t *testing.T) {
	decoded, err := decode([]byte(`
https://example.test:
http://[::1]:
valid.example.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.OriginSelectors) != 0 ||
		len(decoded.HostSelectors) != 1 ||
		len(decoded.Warnings) != 2 {
		t.Fatalf("decoded = %#v", decoded)
	}
	for _, warning := range decoded.Warnings {
		if !strings.Contains(warning.Diagnostic, "port must not be empty") {
			t.Fatalf("warning = %#v", warning)
		}
	}
}

func TestDecodeRequiresASCIIOrPunycodeHostname(t *testing.T) {
	decoded, err := decode([]byte("例.example\nxn--fsq.example\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []HostSelector{
		{Hostname: "xn--fsq.example", HostnameMatch: HostnameExact},
	}
	if len(decoded.Warnings) != 1 || !reflect.DeepEqual(decoded.HostSelectors, want) {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeRemovesInlineCommentsAndRejectsNonHostnameText(t *testing.T) {
	decoded, err := decode([]byte("api#bad.example.test\napi.example.test # staging\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.HostSelectors) != 1 ||
		decoded.HostSelectors[0].Hostname != "api.example.test" {
		t.Fatalf("Host Selectors = %#v", decoded.HostSelectors)
	}
	if len(decoded.Warnings) != 1 ||
		decoded.Warnings[0].Line != 1 ||
		decoded.Warnings[0].Text != "api#bad.example.test" ||
		!strings.Contains(decoded.Warnings[0].Diagnostic, "only a hostname") {
		t.Fatalf("warnings = %#v", decoded.Warnings)
	}
}

func TestOriginSelectorDoesNotApplyHostWildcardSemantics(t *testing.T) {
	decoded, err := decode([]byte("https://*.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{{Scheme: "https", Hostname: "*.example.test"}}
	if !reflect.DeepEqual(decoded.OriginSelectors, want) ||
		len(decoded.HostSelectors) != 0 ||
		len(decoded.Warnings) != 0 {
		t.Fatalf("decode result = %#v", decoded)
	}
}
