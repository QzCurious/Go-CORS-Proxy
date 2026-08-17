package pacrouting

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectInjectsCanonicalRouteViewBag(t *testing.T) {
	projection := Project(upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "qa.example.test", Wildcard: true}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "api.example.test"}},
	}, true, "127.0.0.1:8080", "127.0.0.1:8081")

	want := `var VIEW_BAG = {` +
		`"proxy":"127.0.0.1:8080",` +
		`"routes":[` +
		`{"scheme":"http","hostname":"qa.example.test","port":null,"wildcard":true},` +
		`{"scheme":"https","hostname":"api.example.test","port":"443","wildcard":false},` +
		`{"scheme":"https","hostname":"qa.example.test","port":null,"wildcard":true}]};`
	if !strings.Contains(projection.Body(), want) {
		t.Fatalf("Generated PAC missing canonical route view bag, got:\n%s", projection.Body())
	}
	if !strings.Contains(projection.Body(), `'PROXY ' + VIEW_BAG.proxy`) {
		t.Fatalf("Generated PAC missing proxy directive:\n%s", projection.Body())
	}
	if projection.PACListen() != "127.0.0.1:8081" {
		t.Fatalf("PAC listen = %q", projection.PACListen())
	}
}

func TestProjectionIdentityUsesRoutesAndRuntimeEndpoints(t *testing.T) {
	left := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{
			{Hostname: "api.example.test"},
			{Hostname: "other.example.test"},
		},
	}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	right := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{
			{Hostname: "other.example.test"},
			{Hostname: "api.example.test"},
		},
		Warnings: []upstreamlist.Warning{{Line: 3, Text: "bad", Diagnostic: "ignored"}},
	}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	if !Equal(left, right) {
		t.Fatal("route order or Upstream List warnings changed PAC identity")
	}
	if left.Body() != right.Body() {
		t.Fatal("equivalent PAC Projections produced different canonical bodies")
	}
	if Equal(left, Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081")) {
		t.Fatal("route removal preserved PAC identity")
	}
	if Equal(left, Project(upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{
		{Hostname: "api.example.test"},
		{Hostname: "other.example.test"},
	}}, false, "127.0.0.1:9090", "127.0.0.1:8081")) {
		t.Fatal("proxy endpoint change preserved PAC identity")
	}
	if Equal(left, Project(upstreamlist.Projection{HostSelectors: []upstreamlist.HostSelector{
		{Hostname: "api.example.test"},
		{Hostname: "other.example.test"},
	}}, false, "127.0.0.1:8080", "127.0.0.1:9091")) {
		t.Fatal("PAC endpoint change preserved PAC identity")
	}
}

func TestProjectionAddsHTTPSRoutesOnlyWhenTrusted(t *testing.T) {
	upstreams := upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "api.example.test"}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}},
	}
	withoutTrust := Project(upstreams, false, "127.0.0.1:8080", "127.0.0.1:8081")
	if strings.Contains(withoutTrust.Body(), `"scheme":"https"`) || strings.Contains(withoutTrust.Body(), "secure.example.test") {
		t.Fatalf("untrusted PAC includes HTTPS routes:\n%s", withoutTrust.Body())
	}
	withTrust := Project(upstreams, true, "127.0.0.1:8080", "127.0.0.1:8081")
	if !strings.Contains(withTrust.Body(), `"scheme":"https"`) || !strings.Contains(withTrust.Body(), "secure.example.test") {
		t.Fatalf("trusted PAC omitted HTTPS routes:\n%s", withTrust.Body())
	}
}

func TestLiveHandlerServesAdoptedProjection(t *testing.T) {
	initial := Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081")
	next := Project(upstreamlist.Projection{
		HostSelectors: []upstreamlist.HostSelector{{Hostname: "api.example.test"}},
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

func TestGeneratedPACUsesCanonicalRouteMatcher(t *testing.T) {
	js := Project(upstreamlist.Projection{}, false, "127.0.0.1:8080", "127.0.0.1:8081").Body()
	for _, unwanted := range []string{"HostRoutes", "OriginRoutes", "HTTPSRoutingEnabled", "dnsDomainIs"} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Generated PAC contains obsolete routing logic %q", unwanted)
		}
	}
	for _, wanted := range []string{"VIEW_BAG.routes", "normalizeRequest", "normalizeExplicitPort", "matchesRoute", "parseInt(portText, 10)"} {
		if !strings.Contains(js, wanted) {
			t.Fatalf("Generated PAC missing canonical route logic %q:\n%s", wanted, js)
		}
	}
}
