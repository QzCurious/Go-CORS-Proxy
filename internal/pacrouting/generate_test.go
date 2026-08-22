package pacrouting

import (
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectInjectsCanonicalRouteViewBag(t *testing.T) {
	projection := Project(upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "qa.example.test", Wildcard: true}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "api.example.test"}},
	}, true, "127.0.0.1:8080")

	want := `var VIEW_BAG = {` +
		`"proxy":"127.0.0.1:8080",` +
		`"routes":[` +
		`{"scheme":"http","hostname":"qa.example.test","port":null,"wildcard":true},` +
		`{"scheme":"https","hostname":"qa.example.test","port":null,"wildcard":true},` +
		`{"scheme":"https","hostname":"api.example.test","port":"443","wildcard":false}]};`
	if !strings.Contains(projection, want) {
		t.Fatalf("Generated PAC missing canonical route view bag, got:\n%s", projection)
	}
	if !strings.Contains(projection, `'PROXY ' + VIEW_BAG.proxy`) {
		t.Fatalf("Generated PAC missing proxy directive:\n%s", projection)
	}
}

func TestProjectionAddsHTTPSRoutesOnlyWhenTrusted(t *testing.T) {
	upstreams := upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "api.example.test"}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}},
	}
	withoutTrust := Project(upstreams, false, "127.0.0.1:8080")
	if strings.Contains(withoutTrust, `"scheme":"https"`) || strings.Contains(withoutTrust, "secure.example.test") {
		t.Fatalf("untrusted PAC includes HTTPS routes:\n%s", withoutTrust)
	}
	withTrust := Project(upstreams, true, "127.0.0.1:8080")
	if !strings.Contains(withTrust, `"scheme":"https"`) || !strings.Contains(withTrust, "secure.example.test") {
		t.Fatalf("trusted PAC omitted HTTPS routes:\n%s", withTrust)
	}
}

func TestGeneratedPACUsesCanonicalRouteMatcher(t *testing.T) {
	js := Project(upstreamlist.Projection{}, false, "127.0.0.1:8080")
	for _, unwanted := range []string{"HostRoutes", "OriginRoutes", "HTTPSRoutingEnabled", "dnsDomainIs"} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Generated PAC contains obsolete routing logic %q", unwanted)
		}
	}
	for _, wanted := range []string{"VIEW_BAG.routes", "normalizeRequest", "matchesRoute"} {
		if !strings.Contains(js, wanted) {
			t.Fatalf("Generated PAC missing canonical route logic %q:\n%s", wanted, js)
		}
	}
}
