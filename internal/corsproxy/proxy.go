package corsproxy

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/elazarl/goproxy"
)

const certificateCacheCapacity = 1024

// Handler is one immutable proxy generation. Gateway owns publication and
// lifecycle; Handler owns only request behavior.
type Handler struct {
	proxy *goproxy.ProxyHttpServer
}

type Options struct {
	Certificate *tls.Certificate
	Transport   *http.Transport
}

type localPreflight struct{}

func New(opts Options) *Handler {
	if opts.Transport == nil {
		panic("corsproxy: Transport is required")
	}
	return &Handler{proxy: newProxy(opts.Transport, opts.Certificate)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.proxy.ServeHTTP(w, req)
}

func newProxy(transport *http.Transport, certificate *tls.Certificate) *goproxy.ProxyHttpServer {
	proxy := goproxy.NewProxyHttpServer()
	configureProxyLogging(proxy)
	proxy.Tr = transport
	if certificate == nil {
		proxy.OnRequest().HandleConnectFunc(func(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return goproxy.OkConnect, host
		})
	} else {
		proxy.CertStore = newCertificateCache(certificateCacheCapacity)
		action := &goproxy.ConnectAction{
			Action:    goproxy.ConnectMitm,
			TLSConfig: goproxy.TLSConfigFromCA(certificate),
		}
		proxy.OnRequest().HandleConnectFunc(func(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return action, host
		})
	}

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if !isPreflight(req) {
			return req, nil
		}
		ctx.UserData = localPreflight{}
		return nil, preflightResponse(req)
	})
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil {
			return nil
		}
		if _, ok := ctx.UserData.(localPreflight); ok {
			return resp
		}
		if ctx.Req != nil {
			repairResponseHeaders(resp.Header, ctx.Req.Header.Get("Origin"))
		}
		return resp
	})
	return proxy
}

func configureProxyLogging(proxy *goproxy.ProxyHttpServer) {
	debug, err := strconv.ParseBool(os.Getenv("SEAMLESS_CORS_DEBUG_PROXY"))
	if err == nil && debug {
		proxy.Verbose = true
		return
	}
	proxy.Verbose = false
	proxy.Logger = log.New(io.Discard, "", 0)
}
