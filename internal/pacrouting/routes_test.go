package pacrouting

import (
	"reflect"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestDeriveRouteSetNormalizesDeduplicatesAndSorts(t *testing.T) {
	got := deriveRouteSet(
		[]upstreamlist.HostSelector{
			{Hostname: "qa.example.test", Wildcard: true},
			{Hostname: "api.example.test"},
			{Hostname: "api.example.test"},
		},
		[]upstreamlist.OriginSelector{
			{Scheme: "https", Hostname: "api.example.test"},
			{Scheme: "https", Hostname: "api.example.test", Port: "443"},
			{Scheme: "http", Hostname: "api.example.test", Port: "8080"},
			{Scheme: "http", Hostname: "::1", Port: "80"},
		},
		true,
	)
	want := routeSet{
		{Scheme: "http", Hostname: "::1", Port: "80"},
		{Scheme: "http", Hostname: "api.example.test"},
		{Scheme: "http", Hostname: "api.example.test", Port: "8080"},
		{Scheme: "http", Hostname: "qa.example.test", Wildcard: true},
		{Scheme: "https", Hostname: "api.example.test"},
		{Scheme: "https", Hostname: "api.example.test", Port: "443"},
		{Scheme: "https", Hostname: "qa.example.test", Wildcard: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PAC Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveRouteSetExcludesUntrustedHTTPS(t *testing.T) {
	got := deriveRouteSet(
		[]upstreamlist.HostSelector{{Hostname: "api.example.test"}},
		[]upstreamlist.OriginSelector{
			{Scheme: "https", Hostname: "secure.example.test"},
			{Scheme: "http", Hostname: "plain.example.test"},
		},
		false,
	)
	want := routeSet{
		{Scheme: "http", Hostname: "api.example.test"},
		{Scheme: "http", Hostname: "plain.example.test", Port: "80"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PAC Routes = %#v, want %#v", got, want)
	}
}
