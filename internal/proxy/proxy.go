// Package proxy adapts seamless-cors traffic policies to goproxy mechanics.
package proxy

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/QzCurious/seamless-cors/internal/cors"
	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/elazarl/goproxy"
)

const certificateCacheCapacity = 128

type facadeContext struct {
	route httpsfacade.Route
}

// New constructs one CA-bound proxy handler. Gateway owns handler generation
// publication and lifecycle; liveFacade independently publishes current
// forwarding to every generation that shares it.
func New(
	transport *http.Transport,
	certificate *tls.Certificate,
	liveFacade *httpsfacade.Live,
) http.Handler {
	if transport == nil {
		panic("proxy: Transport is required")
	}
	if liveFacade == nil {
		panic("proxy: live HTTPS Facade is required")
	}

	handler := goproxy.NewProxyHttpServer()
	handler.Tr = transport

	debug, err := strconv.ParseBool(os.Getenv("SEAMLESS_CORS_DEBUG_PROXY"))
	if err == nil && debug {
		handler.Verbose = true
	} else {
		handler.Verbose = false
		handler.Logger = log.New(io.Discard, "", 0)
	}

	action := goproxy.OkConnect
	if certificate != nil {
		handler.CertStore = newCertificateCache(certificateCacheCapacity)
		action = &goproxy.ConnectAction{
			Action:    goproxy.ConnectMitm,
			TLSConfig: goproxy.TLSConfigFromCA(certificate),
		}
	}
	handler.OnRequest().HandleConnectFunc(func(
		host string,
		_ *goproxy.ProxyCtx,
	) (*goproxy.ConnectAction, string) {
		return action, host
	})

	// Resolve HTTPS Facade before CORS so local preflight answers retain the
	// same request identity as real forwarded requests.
	handler.OnRequest().DoFunc(func(
		req *http.Request,
		ctx *goproxy.ProxyCtx,
	) (*http.Request, *http.Response) {
		if route, ok := liveFacade.Forward(req); ok {
			ctx.UserData = facadeContext{route: route}
		}
		return req, nil
	})

	handler.OnRequest().DoFunc(func(
		req *http.Request,
		_ *goproxy.ProxyCtx,
	) (*http.Request, *http.Response) {
		if resp := cors.Preflight(req); resp != nil {
			return nil, resp
		}
		return req, nil
	})

	handler.OnResponse().DoFunc(func(
		resp *http.Response,
		ctx *goproxy.ProxyCtx,
	) *http.Response {
		if facade, ok := ctx.UserData.(facadeContext); ok {
			facade.route.RewriteResponse(resp)
		}
		if ctx.Req == nil {
			return resp
		}
		return cors.Repair(resp, ctx.Req)
	})

	return handler
}
