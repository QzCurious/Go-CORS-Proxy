package pacrouting

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestGenerateInjectsFlatPascalCaseViewBag(t *testing.T) {
	routing := NewRouting("127.0.0.1:8080")
	routing.Apply(upstreamlist.NewEntries(
		[]upstreamlist.HostSelector{
			{Hostname: "qa.example.test", HostnameMatch: upstreamlist.HostnameSingleLevel},
		},
		[]upstreamlist.OriginSelector{
			{Scheme: "https", Hostname: "api.example.test"},
		},
	), true)
	js := routing.Body()

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
	js := NewRouting("127.0.0.1:8080").Body()
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
	js := NewRouting("127.0.0.1:8080").Body()
	originIndex := strings.Index(js, "for (var i = 0; i < originRoutes.length; i++)")
	hostIndex := strings.Index(js, "for (var j = 0; j < hostRoutes.length; j++)")
	if originIndex == -1 || hostIndex == -1 || originIndex > hostIndex {
		t.Fatalf("Generated PAC should check Origin Routes first:\n%s", js)
	}
}

func TestRoutingOwnsHandlerAndPublishesOnlyRouteSetChanges(t *testing.T) {
	routing := NewRouting("127.0.0.1:8080")
	empty := upstreamlist.NewEntries(nil, nil)
	if routing.Apply(empty, true) {
		t.Fatal("trust-only change with no entries should not change the route set")
	}
	entries := upstreamlist.NewEntries(
		[]upstreamlist.HostSelector{{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}},
		nil,
	)
	if !routing.Apply(entries, false) {
		t.Fatal("adding an HTTP host route should change the route set")
	}
	httpBody := routing.Body()
	if !strings.Contains(httpBody, `"Hostname":"api.example.test"`) ||
		strings.Contains(httpBody, `"Scheme":"https"`) {
		t.Fatalf("unexpected PAC body after HTTP apply:\n%s", httpBody)
	}
	if routing.Apply(entries, false) {
		t.Fatal("reapplying equivalent route input should be coalesced")
	}
	reordered := upstreamlist.NewEntries(
		[]upstreamlist.HostSelector{{Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact}, {Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}},
		nil,
	)
	if !routing.Apply(reordered, false) {
		t.Fatal("adding a distinct host route should change the route set")
	}
	if routing.Apply(upstreamlist.NewEntries(
		[]upstreamlist.HostSelector{{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}, {Hostname: "other.example.test", HostnameMatch: upstreamlist.HostnameExact}},
		nil,
	), false) {
		t.Fatal("reordering equivalent host routes should be coalesced")
	}
	body := routing.Body()

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/seamless-cors.pac", nil)
	response := httptest.NewRecorder()
	routing.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != body {
		t.Fatalf("routing handler response = %d %q, want body %q", response.Code, response.Body.String(), body)
	}
}

func TestRoutingAddsHTTPSRoutesOnlyWhenTrusted(t *testing.T) {
	entries := upstreamlist.NewEntries(
		[]upstreamlist.HostSelector{{Hostname: "api.example.test", HostnameMatch: upstreamlist.HostnameExact}},
		[]upstreamlist.OriginSelector{{Scheme: "https", Hostname: "secure.example.test"}},
	)
	routing := NewRouting("127.0.0.1:8080")
	if !routing.Apply(entries, false) {
		t.Fatal("HTTP route set should change on first semantic apply")
	}
	withoutTrust := routing.Body()
	if strings.Contains(withoutTrust, `"Scheme":"https"`) || strings.Contains(withoutTrust, "secure.example.test") {
		t.Fatalf("untrusted PAC should omit HTTPS routes:\n%s", withoutTrust)
	}
	if !routing.Apply(entries, true) {
		t.Fatal("trusted HTTPS should change the route set")
	}
	withTrust := routing.Body()
	if !strings.Contains(withTrust, `"Scheme":"https"`) || !strings.Contains(withTrust, "secure.example.test") {
		t.Fatalf("trusted PAC should include HTTPS routes:\n%s", withTrust)
	}
}
