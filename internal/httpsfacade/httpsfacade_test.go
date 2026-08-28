package httpsfacade_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectDerivesDefaultAndCustomPortRoutes(t *testing.T) {
	projection := httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "api.test", Port: 80},
		{Scheme: "http", Hostname: "local.test", Port: 3000},
	})
	want := []httpsfacade.Route{
		{Hostname: "api.test", HTTPSPort: 443, HTTPPort: 80},
		{Hostname: "local.test", HTTPSPort: 3000, HTTPPort: 3000},
	}
	if got := projection.Routes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
}

func TestProjectAppliesHTTPSRoutingSpecificity(t *testing.T) {
	tests := []struct {
		name      string
		selectors []upstreamlist.OriginSelector
		want      []httpsfacade.Route
	}{
		{
			name: "native HTTPS shadows every HTTP facade",
			selectors: []upstreamlist.OriginSelector{
				{Scheme: "http", Hostname: "api.test", Port: 80},
				{Scheme: "http", Hostname: "api.test", Port: 443},
				{Scheme: "https", Hostname: "api.test", Port: 443},
			},
		},
		{
			name: "unchanged HTTP port shadows translated default",
			selectors: []upstreamlist.OriginSelector{
				{Scheme: "http", Hostname: "api.test", Port: 80},
				{Scheme: "http", Hostname: "api.test", Port: 443},
			},
			want: []httpsfacade.Route{{Hostname: "api.test", HTTPSPort: 443, HTTPPort: 443}},
		},
		{
			name: "unchanged HTTP port wins independently of line order",
			selectors: []upstreamlist.OriginSelector{
				{Scheme: "http", Hostname: "api.test", Port: 443},
				{Scheme: "http", Hostname: "api.test", Port: 80},
			},
			want: []httpsfacade.Route{{Hostname: "api.test", HTTPSPort: 443, HTTPPort: 443}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpsfacade.Project(tt.selectors).Routes(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("routes = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLiveForwardUsesSelectedHTTPAuthority(t *testing.T) {
	live := httpsfacade.NewLive(httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "api.test", Port: 80},
		{Scheme: "http", Hostname: "local.test", Port: 3000},
		{Scheme: "http", Hostname: "::1", Port: 3000},
	}))
	tests := []struct {
		target   string
		wantURL  string
		wantHost string
	}{
		{target: "https://api.test:443/items?q=1", wantURL: "http://api.test/items?q=1", wantHost: "api.test"},
		{target: "https://local.test:3000/items?q=1", wantURL: "http://local.test:3000/items?q=1", wantHost: "local.test:3000"},
		{target: "https://[::1]:3000/items?q=1", wantURL: "http://[::1]:3000/items?q=1", wantHost: "[::1]:3000"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			req.Header.Set("Origin", "https://app.test")
			if _, ok := live.Forward(req); !ok {
				t.Fatal("request did not match HTTPS Facade")
			}
			if got := req.URL.String(); got != tt.wantURL {
				t.Fatalf("forward URL = %q, want %q", got, tt.wantURL)
			}
			if req.Host != tt.wantHost {
				t.Fatalf("Host = %q, want %q", req.Host, tt.wantHost)
			}
			if got := req.Header.Get("Origin"); got != "https://app.test" {
				t.Fatalf("Origin = %q", got)
			}
		})
	}
}

func TestLivePublishesProjectionAtRequestBoundary(t *testing.T) {
	live := httpsfacade.NewLive(httpsfacade.Projection{})
	before := httptest.NewRequest(http.MethodGet, "https://api.test/items", nil)
	if _, ok := live.Forward(before); ok {
		t.Fatal("empty projection matched request")
	}

	live.Set(httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "api.test", Port: 80},
	}))
	after := httptest.NewRequest(http.MethodGet, "https://api.test/items", nil)
	if _, ok := live.Forward(after); !ok {
		t.Fatal("published projection did not match request")
	}
}

func TestRouteRewritesOnlySelectedOriginAbsoluteLocation(t *testing.T) {
	live := httpsfacade.NewLive(httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "api.test", Port: 80},
	}))
	req := httptest.NewRequest(http.MethodGet, "https://api.test/login", nil)
	route, ok := live.Forward(req)
	if !ok {
		t.Fatal("request did not match HTTPS Facade")
	}

	tests := []struct {
		location string
		want     string
	}{
		{location: "http://api.test/dashboard?q=1#done", want: "https://api.test/dashboard?q=1#done"},
		{location: "http://api.test:80/explicit", want: "https://api.test/explicit"},
		{location: "/dashboard", want: "/dashboard"},
		{location: "http://accounts.test/login", want: "http://accounts.test/login"},
		{location: "https://api.test/already-secure", want: "https://api.test/already-secure"},
	}
	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{"Location": {tt.location}}}
			route.RewriteResponse(resp)
			if got := resp.Header.Get("Location"); got != tt.want {
				t.Fatalf("Location = %q, want %q", got, tt.want)
			}
		})
	}
}
