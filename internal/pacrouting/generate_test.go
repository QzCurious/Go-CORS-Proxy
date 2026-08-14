package pacrouting

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectInjectsFlatPascalCaseViewBag(t *testing.T) {
	projection := Project(upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "qa.example.test", HostnameMatch: upstreamlist.HostnameSingleLevel}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "api.example.test"}},
	}, true, "127.0.0.1:8080", "127.0.0.1:8081")

	want := `var VIEW_BAG = {` +
		`"Proxy":"127.0.0.1:8080",` +
		`"HostRoutes":[` +
		`{"Scheme":"http","Hostname":"qa.example.test","HostnameMatch":"SingleLevel"},` +
		`{"Scheme":"https","Hostname":"qa.example.test","HostnameMatch":"SingleLevel"}],` +
		`"OriginRoutes":["https://api.example.test","https://api.example.test:443"]};`
	if !strings.Contains(projection.Body(), want) {
		t.Fatalf("Generated PAC missing flat view bag, got:\n%s", projection.Body())
	}
	if !strings.Contains(projection.Body(), `'PROXY ' + VIEW_BAG.Proxy`) {
		t.Fatalf("Generated PAC missing proxy directive:\n%s", projection.Body())
	}
	if projection.PACListen() != "127.0.0.1:8081" {
		t.Fatalf("PAC listen = %q", projection.PACListen())
	}
}

func TestProjectionIdentityUsesRoutesAndRuntimeEndpoints(t *testing.T) {
	left := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{
			{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact},
			{Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact},
		},
	}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	right := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{
			{Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact},
			{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact},
		},
		Warnings: []upstreamlist.Warning{{Line: 3, Text: "bad", Diagnostic: "ignored"}},
	}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	if !Equal(left, right) {
		t.Fatal("route order or Upstream List warnings changed PAC identity")
	}
	if Equal(left, Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081")) {
		t.Fatal("route removal preserved PAC identity")
	}
	if Equal(left, Project(upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{
		{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact},
		{Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact},
	}}, false, "127.0.0.1:9090", "127.0.0.1:8081")) {
		t.Fatal("proxy endpoint change preserved PAC identity")
	}
	if Equal(left, Project(upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{
		{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact},
		{Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact},
	}}, false, "127.0.0.1:8080", "127.0.0.1:9091")) {
		t.Fatal("PAC endpoint change preserved PAC identity")
	}
}

func TestProjectionAddsHTTPSRoutesOnlyWhenTrusted(t *testing.T) {
	upstreams := upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}},
	}
	withoutTrust := Project(upstreams, false, "127.0.0.1:8080", "127.0.0.1:8081")
	if strings.Contains(withoutTrust.Body(), `"Scheme":"https"`) || strings.Contains(withoutTrust.Body(), "secure.example.test") {
		t.Fatalf("untrusted PAC includes HTTPS routes:\n%s", withoutTrust.Body())
	}
	withTrust := Project(upstreams, true, "127.0.0.1:8080", "127.0.0.1:8081")
	if !strings.Contains(withTrust.Body(), `"Scheme":"https"`) || !strings.Contains(withTrust.Body(), "secure.example.test") {
		t.Fatalf("trusted PAC omitted HTTPS routes:\n%s", withTrust.Body())
	}
}

func TestLiveHandlerServesAdoptedProjection(t *testing.T) {
	initial := Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	next := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}},
	}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	handler := NewLiveHandler(initial)
	handler.Set(next)

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8081/seamless-cors.pac", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != next.Body() {
		t.Fatalf("handler response = %d %q", response.Code, response.Body.String())
	}
}

func TestGeneratedPACDoesNotParseOrInferPorts(t *testing.T) {
	js := Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081").Body()
	for _, unwanted := range []string{"explicitPortForURL", "defaultPortForScheme", "parsedPort"} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Generated PAC contains obsolete port logic %q", unwanted)
		}
	}
	if !strings.Contains(js, `url == originRoute || url.indexOf(originRoute + '/') == 0`) {
		t.Fatalf("Generated PAC missing boundary-safe Origin Route match:\n%s", js)
	}
}
