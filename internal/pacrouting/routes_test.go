package pacrouting

import (
	"reflect"
	"testing"

	"seamless-cors/internal/domainlist"
)

func TestDeriveRouteBucketsUsesNormalizedDomainListEntries(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"*.qa.example.test",
		"https://localhost:9443",
		"http://[::1]:3000",
	)

	got := deriveRouteBuckets(entries, true)
	want := routeBuckets{
		exactHosts: []hostRoute{{
			Host:       "api.example.test",
			AllowHTTP:  true,
			AllowHTTPS: true,
		}},
		wildcardParents: []hostRoute{{
			Host:       "qa.example.test",
			AllowHTTP:  true,
			AllowHTTPS: true,
		}},
		origins: []originRoute{
			{Scheme: "https", Host: "localhost", Port: "9443"},
			{Scheme: "http", Host: "::1", Port: "3000"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route buckets = %#v, want %#v", got, want)
	}
}

func TestDeriveRouteBucketsExcludesUntrustedHTTPS(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"https://secure.example.test",
		"http://plain.example.test",
	)

	got := deriveRouteBuckets(entries, false)
	want := routeBuckets{
		exactHosts: []hostRoute{{
			Host:      "api.example.test",
			AllowHTTP: true,
		}},
		wildcardParents: []hostRoute{},
		origins: []originRoute{{
			Scheme: "http",
			Host:   "plain.example.test",
			Port:   "80",
		}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route buckets = %#v, want %#v", got, want)
	}
}

func mustParseEntries(t *testing.T, texts ...string) []domainlist.Entry {
	t.Helper()
	entries := make([]domainlist.Entry, 0, len(texts))
	for _, text := range texts {
		entry, err := domainlist.ParseEntry(text)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}
