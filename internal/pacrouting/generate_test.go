package pacrouting

import (
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestGenerateInjectsFlatPascalCaseViewBag(t *testing.T) {
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   true,
		UpstreamList: upstreamlist.UpstreamList{
			HostSelectors: []upstreamlist.HostSelector{
				{Hostname: "qa.example.test", HostnameMatch: upstreamlist.HostnameSingleLevel},
			},
			OriginSelectors: []upstreamlist.OriginSelector{
				{Scheme: "https", Hostname: "api.example.test"},
			},
		},
	})

	want := `var VIEW_BAG = {` +
		`"Proxy":"127.0.0.1:8080",` +
		`"HostRoutes":[` +
		`{"Scheme":"http","Hostname":"qa.example.test","HostnameMatch":"SingleLevel"},` +
		`{"Scheme":"https","Hostname":"qa.example.test","HostnameMatch":"SingleLevel"}],` +
		`"OriginRoutes":["https://api.example.test","https://api.example.test:443"]};`
	if !strings.Contains(js, want) {
		t.Fatalf("Generated PAC missing flat view bag, got:\n%s", js)
	}
	if !strings.Contains(js, `'PROXY ' + VIEW_BAG.Proxy`) {
		t.Fatalf("PAC program should construct the proxy directive, got:\n%s", js)
	}
}

func TestGeneratePACDoesNotParseOrInferPorts(t *testing.T) {
	js := Generate(Options{})
	for _, unwanted := range []string{"explicitPortForURL", "defaultPortForScheme", "parsedPort"} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Generated PAC contains obsolete port logic %q", unwanted)
		}
	}
	if !strings.Contains(js, `url == originRoute || url.indexOf(originRoute + '/') == 0`) {
		t.Fatalf("Generated PAC missing boundary-safe Origin Route match:\n%s", js)
	}
}

func TestGenerateUsesOriginRoutesBeforeHostRoutes(t *testing.T) {
	js := Generate(Options{})
	originIndex := strings.Index(js, "for (var i = 0; i < originRoutes.length; i++)")
	hostIndex := strings.Index(js, "for (var j = 0; j < hostRoutes.length; j++)")
	if originIndex == -1 || hostIndex == -1 || originIndex > hostIndex {
		t.Fatalf("Generated PAC should check Origin Routes first:\n%s", js)
	}
}
