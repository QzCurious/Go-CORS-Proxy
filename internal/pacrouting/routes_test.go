package pacrouting

import (
	"reflect"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestDeriveHostRoutesExpandsSchemesWithoutPorts(t *testing.T) {
	selectors := []upstreamlist.HostSelector{
		{Hostname: "api.example.test"},
		{Hostname: "qa.example.test", Wildcard: true},
	}

	got := deriveHostRoutes(selectors, true)
	want := []hostRoute{
		{Scheme: "http", Hostname: "api.example.test"},
		{Scheme: "https", Hostname: "api.example.test"},
		{Scheme: "http", Hostname: "qa.example.test", Wildcard: true},
		{Scheme: "https", Hostname: "qa.example.test", Wildcard: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Host Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveHostRoutesExcludesUntrustedHTTPS(t *testing.T) {
	selectors := []upstreamlist.HostSelector{
		{Hostname: "api.example.test"},
	}
	got := deriveHostRoutes(selectors, false)
	want := []hostRoute{
		{Scheme: "http", Hostname: "api.example.test"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Host Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveOriginRoutesExpandsDefaultPorts(t *testing.T) {
	selectors := []upstreamlist.OriginSelector{
		{Scheme: "https", Hostname: "api.example.test"},
		{Scheme: "https", Hostname: "api.example.test", Port: "443"},
		{Scheme: "http", Hostname: "api.example.test", Port: "8080"},
		{Scheme: "http", Hostname: "::1", Port: "80"},
	}
	got := deriveOriginRoutes(selectors, true)
	want := []string{
		"https://api.example.test",
		"https://api.example.test:443",
		"http://api.example.test:8080",
		"http://[::1]",
		"http://[::1]:80",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Origin Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveOriginRoutesExcludesUntrustedHTTPS(t *testing.T) {
	selectors := []upstreamlist.OriginSelector{
		{Scheme: "https", Hostname: "secure.example.test"},
		{Scheme: "http", Hostname: "plain.example.test"},
	}
	got := deriveOriginRoutes(selectors, false)
	want := []string{
		"http://plain.example.test",
		"http://plain.example.test:80",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Origin Routes = %#v, want %#v", got, want)
	}
}
