// Package cors owns seamless-cors' fixed DEV/QA preflight and response policy.
package cors

import (
	"net/http"
	"slices"
	"strings"
)

// Preflight answers a browser CORS preflight locally. It returns nil when the
// request is not a preflight.
func Preflight(req *http.Request) *http.Response {
	if !isPreflight(req) {
		return nil
	}

	header := make(http.Header)
	header.Set("Access-Control-Allow-Origin", req.Header.Get("Origin"))
	header.Set("Access-Control-Allow-Credentials", "true")
	header.Set("Access-Control-Allow-Methods", req.Header.Get("Access-Control-Request-Method"))
	if headers := req.Header.Get("Access-Control-Request-Headers"); headers != "" {
		header.Set("Access-Control-Allow-Headers", headers)
	}
	if req.Header.Get("Access-Control-Request-Private-Network") == "true" {
		header.Set("Access-Control-Allow-Private-Network", "true")
	}
	header.Set("Access-Control-Max-Age", "0")
	addVary(
		header,
		"Origin",
		"Access-Control-Request-Method",
		"Access-Control-Request-Headers",
		"Access-Control-Request-Private-Network",
	)

	return &http.Response{
		StatusCode:    http.StatusNoContent,
		Status:        http.StatusText(http.StatusNoContent),
		Header:        header,
		Body:          http.NoBody,
		ContentLength: 0,
		Request:       req,
	}
}

func isPreflight(req *http.Request) bool {
	return req.Method == http.MethodOptions &&
		req.Header.Get("Origin") != "" &&
		req.Header.Get("Access-Control-Request-Method") != ""
}

// Repair replaces an origin-bearing upstream response's CORS policy with the
// fixed seamless-cors DEV/QA policy. Proxy-generated failures should not cross
// this interface.
func Repair(resp *http.Response, req *http.Request) *http.Response {
	if resp == nil || req == nil || isPreflight(req) {
		return resp
	}

	origin := req.Header.Get("Origin")
	if origin == "" {
		return resp
	}

	for name := range resp.Header {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			resp.Header.Del(name)
		}
	}
	resp.Header.Set("Access-Control-Allow-Origin", origin)
	resp.Header.Set("Access-Control-Allow-Credentials", "true")
	addVary(resp.Header, "Origin")

	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		if strings.EqualFold(name, "Set-Cookie") ||
			strings.EqualFold(name, "Set-Cookie2") ||
			strings.HasPrefix(strings.ToLower(name), "access-control-") {
			continue
		}
		names = append(names, http.CanonicalHeaderKey(name))
	}
	slices.Sort(names)
	if len(names) != 0 {
		resp.Header.Set("Access-Control-Expose-Headers", strings.Join(names, ", "))
	}

	return resp
}

func addVary(header http.Header, values ...string) {
	existing := make(map[string]bool, len(values))
	for _, line := range header.Values("Vary") {
		for value := range strings.SplitSeq(line, ",") {
			existing[strings.ToLower(strings.TrimSpace(value))] = true
		}
	}
	if existing["*"] {
		return
	}

	for _, value := range values {
		key := strings.ToLower(value)
		if existing[key] {
			continue
		}
		header.Add("Vary", value)
		existing[key] = true
	}
}
