package cors_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/cors"
)

func TestPreflightAnswersFromRequestPolicy(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "http://api.test/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")

	resp := cors.Preflight(req)
	if resp == nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight response = %#v", resp)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":          "null",
		"Access-Control-Allow-Credentials":     "true",
		"Access-Control-Allow-Methods":         "PATCH",
		"Access-Control-Allow-Headers":         "X-Dev",
		"Access-Control-Allow-Private-Network": "true",
		"Access-Control-Max-Age":               "0",
		"Content-Type":                         "",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	assertHeaderTokens(
		t,
		resp.Header.Values("Vary"),
		"Origin",
		"Access-Control-Request-Method",
		"Access-Control-Request-Headers",
		"Access-Control-Request-Private-Network",
	)
}

func TestRepairReplacesCORSHeadersAndPreservesResponse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.test/items", nil)
	req.Header.Set("Origin", "https://app.test")
	resp := &http.Response{Header: http.Header{
		"Access-Control-Allow-Origin":  {"*"},
		"Access-Control-Allow-Methods": {"DELETE"},
		"Access-Control-Future":        {"upstream"},
		"Set-Cookie":                   {"session=secret"},
		"Set-Cookie2":                  {"legacy=secret"},
		"Vary":                         {"Accept-Encoding"},
		"X-Trace":                      {"abc"},
	}}

	cors.Repair(resp, req)

	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":      "https://app.test",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "",
		"Access-Control-Future":            "",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	assertHeaderTokens(t, resp.Header.Values("Vary"), "Accept-Encoding", "Origin")
	exposed := headerTokens(resp.Header.Values("Access-Control-Expose-Headers"))
	if !slices.Contains(exposed, "Vary") || !slices.Contains(exposed, "X-Trace") {
		t.Fatalf("exposed headers = %#v", exposed)
	}
	for _, forbidden := range []string{"Set-Cookie", "Set-Cookie2", "Access-Control-Allow-Origin"} {
		if slices.Contains(exposed, forbidden) {
			t.Fatalf("exposed headers contain %q: %#v", forbidden, exposed)
		}
	}
}

func TestRepairLeavesNonCORSResponseUntouched(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.test/items", nil)
	resp := &http.Response{Header: http.Header{
		"Access-Control-Allow-Origin":      {"https://upstream.test"},
		"Access-Control-Allow-Credentials": {"false"},
	}}

	cors.Repair(resp, req)

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://upstream.test" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestRepairPreservesWildcardVary(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.test/items", nil)
	req.Header.Set("Origin", "https://app.test")
	resp := &http.Response{Header: http.Header{"Vary": {"*"}}}

	cors.Repair(resp, req)

	assertHeaderTokens(t, resp.Header.Values("Vary"), "*")
}

func assertHeaderTokens(t *testing.T, values []string, want ...string) {
	t.Helper()
	got := headerTokens(values)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("header tokens = %#v, want %#v", got, want)
	}
}

func headerTokens(values []string) []string {
	var tokens []string
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			tokens = append(tokens, strings.TrimSpace(token))
		}
	}
	slices.Sort(tokens)
	return tokens
}
