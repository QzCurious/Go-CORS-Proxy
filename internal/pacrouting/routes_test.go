package pacrouting

import (
	"reflect"
	"testing"

	"seamless-cors/internal/domainlist"
)

func TestDeriveDomainRoutesExpandsSchemesWithoutPorts(t *testing.T) {
	selectors := []domainlist.DomainSelector{
		{Hostname: "api.example.test", HostnameMatch: domainlist.HostnameExact},
		{Hostname: "qa.example.test", HostnameMatch: domainlist.HostnameSingleLevel},
		{Hostname: "dev.example.test", HostnameMatch: domainlist.HostnameRecursive},
	}

	got := deriveDomainRoutes(selectors, true)
	want := []domainRoute{
		{Scheme: "http", Hostname: "api.example.test", HostnameMatch: "Exact"},
		{Scheme: "https", Hostname: "api.example.test", HostnameMatch: "Exact"},
		{Scheme: "http", Hostname: "qa.example.test", HostnameMatch: "SingleLevel"},
		{Scheme: "https", Hostname: "qa.example.test", HostnameMatch: "SingleLevel"},
		{Scheme: "http", Hostname: "dev.example.test", HostnameMatch: "Recursive"},
		{Scheme: "https", Hostname: "dev.example.test", HostnameMatch: "Recursive"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Domain Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveDomainRoutesExcludesUntrustedHTTPS(t *testing.T) {
	selectors := []domainlist.DomainSelector{
		{Hostname: "api.example.test", HostnameMatch: domainlist.HostnameExact},
	}
	got := deriveDomainRoutes(selectors, false)
	want := []domainRoute{
		{Scheme: "http", Hostname: "api.example.test", HostnameMatch: "Exact"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Domain Routes = %#v, want %#v", got, want)
	}
}

func TestDeriveOriginRoutesExpandsDefaultPorts(t *testing.T) {
	selectors := []domainlist.OriginSelector{
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
	selectors := []domainlist.OriginSelector{
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
