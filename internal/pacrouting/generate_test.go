package pacrouting

import (
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectInjectsCanonicalRouteViewBag(t *testing.T) {
	upstreams := upstreamlist.Projection{
		HostSelectors:   []upstreamlist.HostSelector{{Hostname: "qa.example.test", Wildcard: true}},
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "api.example.test", Port: 443}},
	}
	projection := Project(upstreams, httpsfacade.Project(upstreams.OriginSelectors), true, "127.0.0.1:8080")

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
		OriginSelectors: []upstreamlist.OriginSelector{{Scheme: "https", Hostname: "secure.example.test", Port: 443}},
	}
	facades := httpsfacade.Project(upstreams.OriginSelectors)
	withoutTrust := Project(upstreams, facades, false, "127.0.0.1:8080")
	if strings.Contains(withoutTrust, `"scheme":"https"`) || strings.Contains(withoutTrust, "secure.example.test") {
		t.Fatalf("untrusted PAC includes HTTPS routes:\n%s", withoutTrust)
	}
	withTrust := Project(upstreams, facades, true, "127.0.0.1:8080")
	if !strings.Contains(withTrust, `"scheme":"https"`) || !strings.Contains(withTrust, "secure.example.test") {
		t.Fatalf("trusted PAC omitted HTTPS routes:\n%s", withTrust)
	}
}

func TestProjectionAddsHTTPSFacadeRoutesOnlyWhenTrusted(t *testing.T) {
	upstreams := upstreamlist.Projection{OriginSelectors: []upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "default.example.test", Port: 80},
		{Scheme: "http", Hostname: "local.example.test", Port: 3000},
	}}
	facades := httpsfacade.Project(upstreams.OriginSelectors)

	withoutTrust := Project(upstreams, facades, false, "127.0.0.1:8080")
	if strings.Contains(withoutTrust, `"scheme":"https"`) {
		t.Fatalf("untrusted PAC includes HTTPS Facade routes:\n%s", withoutTrust)
	}

	withTrust := Project(upstreams, facades, true, "127.0.0.1:8080")
	for _, want := range []string{
		`{"scheme":"https","hostname":"default.example.test","port":"443","wildcard":false}`,
		`{"scheme":"https","hostname":"local.example.test","port":"3000","wildcard":false}`,
	} {
		if !strings.Contains(withTrust, want) {
			t.Fatalf("trusted PAC omitted %s:\n%s", want, withTrust)
		}
	}
}
