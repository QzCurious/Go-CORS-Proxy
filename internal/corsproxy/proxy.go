package corsproxy

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/elazarl/goproxy"
)

const certificateCacheCapacity = 128

// New forms one immutable proxy generation. Gateway owns publication and
// lifecycle; the returned handler owns only request behavior.
func New(
	transport *http.Transport,
	certificate *tls.Certificate,
) http.Handler {
	if transport == nil {
		panic("corsproxy: Transport is required")
	}

	proxy := goproxy.NewProxyHttpServer()
	proxy.Tr = transport

	// Logging.
	debug, err := strconv.ParseBool(os.Getenv("SEAMLESS_CORS_DEBUG_PROXY"))
	if err == nil && debug {
		proxy.Verbose = true
	} else {
		proxy.Verbose = false
		proxy.Logger = log.New(io.Discard, "", 0)
	}

	// HTTPS.
	action := goproxy.OkConnect
	if certificate != nil {
		proxy.CertStore = newCertificateCache(certificateCacheCapacity)
		action = &goproxy.ConnectAction{
			Action:    goproxy.ConnectMitm,
			TLSConfig: goproxy.TLSConfigFromCA(certificate),
		}
	}
	proxy.OnRequest().HandleConnectFunc(func(
		host string,
		_ *goproxy.ProxyCtx,
	) (*goproxy.ConnectAction, string) {
		return action, host
	})

	// Preflight.
	proxy.OnRequest().DoFunc(func(
		req *http.Request,
		_ *goproxy.ProxyCtx,
	) (*http.Request, *http.Response) {
		if !isPreflight(req) {
			return req, nil
		}

		resp := goproxy.NewResponse(
			req,
			goproxy.ContentTypeText,
			http.StatusNoContent,
			"",
		)
		resp.Header.Del("Content-Type")
		resp.Header.Set("Access-Control-Allow-Origin", req.Header.Get("Origin"))
		resp.Header.Set("Access-Control-Allow-Credentials", "true")
		resp.Header.Set(
			"Access-Control-Allow-Methods",
			req.Header.Get("Access-Control-Request-Method"),
		)
		if headers := req.Header.Get("Access-Control-Request-Headers"); headers != "" {
			resp.Header.Set("Access-Control-Allow-Headers", headers)
		}
		if req.Header.Get("Access-Control-Request-Private-Network") == "true" {
			resp.Header.Set("Access-Control-Allow-Private-Network", "true")
		}
		resp.Header.Set("Access-Control-Max-Age", "0")
		addVary(
			resp.Header,
			"Origin",
			"Access-Control-Request-Method",
			"Access-Control-Request-Headers",
			"Access-Control-Request-Private-Network",
		)

		return nil, resp
	})

	// CORS response repair.
	proxy.OnResponse().DoFunc(func(
		resp *http.Response,
		ctx *goproxy.ProxyCtx,
	) *http.Response {
		if resp == nil || ctx.Req == nil || isPreflight(ctx.Req) {
			return resp
		}

		origin := ctx.Req.Header.Get("Origin")
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
	})

	return proxy
}

func isPreflight(req *http.Request) bool {
	return req.Method == http.MethodOptions &&
		req.Header.Get("Origin") != "" &&
		req.Header.Get("Access-Control-Request-Method") != ""
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
