package liveconfig

import (
	"strings"
	"testing"
)

func TestDomainListDecoderNormalizesEntries(t *testing.T) {
	decoded, err := decodeDomainList([]byte(`
API.EXAMPLE.TEST.
*.QA.EXAMPLE.TEST
https://Example.Test:443
http://[::1]:3000
`))
	if err != nil {
		t.Fatal(err)
	}
	entries := decoded.entries
	if len(entries) != 4 ||
		entries[0].Hostname != "api.example.test" ||
		entries[1].Hostname != "*.qa.example.test" ||
		entries[2].Hostname != "example.test" ||
		entries[2].Port != "443" ||
		entries[3].Hostname != "::1" ||
		entries[3].Port != "3000" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestDomainListDecoderRejectsNonUTF8(t *testing.T) {
	_, err := decodeDomainList([]byte{0xff})
	if err == nil || !strings.Contains(err.Error(), "content must be UTF-8") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestDomainListDecoderRejectsOriginUserInformation(t *testing.T) {
	decoded, err := decodeDomainList([]byte("https://user@example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.entries) != 0 ||
		len(decoded.warnings) != 1 ||
		!strings.Contains(decoded.warnings[0].Diagnostic, "must not include user information") {
		t.Fatalf("decode result = %#v", decoded)
	}
}

func TestDomainListDecoderCollectsEveryInvalidLine(t *testing.T) {
	decoded, err := decodeDomainList([]byte(`
valid.example.test
https://*.bad.example.test
api#bad.example.test
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.entries) != 1 || decoded.entries[0].Hostname != "valid.example.test" {
		t.Fatalf("entries = %#v", decoded.entries)
	}
	if len(decoded.warnings) != 2 ||
		decoded.warnings[0].Line != 3 ||
		decoded.warnings[1].Line != 4 {
		t.Fatalf("warnings = %#v", decoded.warnings)
	}
}
