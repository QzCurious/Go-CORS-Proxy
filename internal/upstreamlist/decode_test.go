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
			{Hostname: "api.example.test"},
			{Hostname: "qa.example.test", Wildcard: true},
		},
		OriginSelectors: []OriginSelector{
			{Scheme: "https", Hostname: "example.test", Port: 443},
			{Scheme: "http", Hostname: "::1", Port: 3000},
		},
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestHTTPSIntentIgnoresHostAndHTTPSelectors(t *testing.T) {
	list := Projection{
		HostSelectors:   []HostSelector{{Hostname: "api.example.test"}},
		OriginSelectors: []OriginSelector{{Scheme: "http", Hostname: "plain.example.test", Port: 80}},
	}
	if list.HTTPSIntent() {
		t.Fatal("HTTP-only entries should not express HTTPS intent")
	}
}

func TestDecodeNormalizesExplicitAndDefaultPorts(t *testing.T) {
	decoded, err := decode([]byte(`
HTTPS://user:password@EXAMPLE.TEST:0443/
https://example.test
http://example.test
http://example.test:00080
http://example.test:00001
https://example.test:65535
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []OriginSelector{
		{Scheme: "https", Hostname: "example.test", Port: 443},
		{Scheme: "https", Hostname: "example.test", Port: 443},
		{Scheme: "http", Hostname: "example.test", Port: 80},
		{Scheme: "http", Hostname: "example.test", Port: 80},
		{Scheme: "http", Hostname: "example.test", Port: 1},
		{Scheme: "https", Hostname: "example.test", Port: 65535},
	}
	if !reflect.DeepEqual(decoded.OriginSelectors, want) {
		t.Fatalf("Origin Selectors = %#v, want %#v", decoded.OriginSelectors, want)
	}
}

func TestDecodeRejectsExplicitPortsOutsideTCPRange(t *testing.T) {
	selectorTexts := []string{
		"http://example.test:0",
		"http://example.test:00000",
		"https://example.test:65536",
		"https://example.test:99999",
	}

	for _, selectorText := range selectorTexts {
		t.Run(selectorText, func(t *testing.T) {
			decoded, err := decode([]byte(selectorText))
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded.OriginSelectors) != 0 || len(decoded.Warnings) != 1 ||
				decoded.Warnings[0].Diagnostic != invalidSelectorDiagnostic {
				t.Fatalf("decode result = %#v", decoded)
			}
		})
	}
}

func TestDecodeSupportsExactAndSingleLevelHostSelectorMatches(t *testing.T) {
	decoded, err := decode([]byte(`
example.test
*.qa.example.test
[::1]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []HostSelector{
		{Hostname: "example.test"},
		{Hostname: "qa.example.test", Wildcard: true},
		{Hostname: "::1"},
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
		if warning.Line != wantLine || warning.Text == "" || warning.Diagnostic != invalidSelectorDiagnostic {
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
		if warning.Diagnostic != invalidSelectorDiagnostic {
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
		{Hostname: "xn--fsq.example"},
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
		decoded.Warnings[0].Diagnostic != invalidSelectorDiagnostic {
		t.Fatalf("warnings = %#v", decoded.Warnings)
	}
}

func TestOriginSelectorRejectsWildcardSyntax(t *testing.T) {
	decoded, err := decode([]byte("https://*.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.OriginSelectors) != 0 || len(decoded.HostSelectors) != 0 ||
		len(decoded.Warnings) != 1 || decoded.Warnings[0].Diagnostic != invalidSelectorDiagnostic {
		t.Fatalf("decode result = %#v", decoded)
	}
}

func TestSelectorsUseUniformHostnameValidation(t *testing.T) {
	selectorTexts := []string{
		"bad_name.example.test",
		"*.127.0.0.1",
		"trailing.example.test.",
		"https://bad_name.example.test",
		"http://bad_name.example.test",
	}

	for _, selectorText := range selectorTexts {
		t.Run(selectorText, func(t *testing.T) {
			decoded, err := decode([]byte(selectorText))
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded.HostSelectors)+len(decoded.OriginSelectors) != 0 ||
				len(decoded.Warnings) != 1 || decoded.Warnings[0].Text != selectorText {
				t.Fatalf("decode result = %#v", decoded)
			}
		})
	}
}
