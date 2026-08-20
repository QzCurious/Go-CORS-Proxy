package corsproxy

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/elazarl/goproxy"
)

const certificateCacheCapacity = 1024

// Core serves one stable proxy endpoint while atomically replacing immutable
// goproxy handler generations as UserCA signing material changes.
type Core struct {
	current   atomic.Pointer[goproxy.ProxyHttpServer]
	transport *http.Transport
}

type Options struct {
	Certificate *tls.Certificate
	Transport   *http.Transport
}

type localPreflight struct{}

func New(opts Options) *Core {
	transport := opts.Transport
	if transport == nil {
		transport = defaultTransport()
	}
	core := &Core{transport: transport}
	core.current.Store(core.newGeneration(opts.Certificate))
	return core
}

func (c *Core) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.current.Load().ServeHTTP(w, req)
}

// ReplaceCertificate publishes a fresh MITM generation. The certificate and
// its backing slices and signer are immutable after publication by contract.
func (c *Core) ReplaceCertificate(certificate *tls.Certificate) {
	if certificate == nil {
		c.DeactivateHTTPS()
		return
	}
	c.current.Store(c.newGeneration(certificate))
}

// DeactivateHTTPS publishes a direct-tunnel generation. Requests already
// admitted by the previous generation are not drained.
func (c *Core) DeactivateHTTPS() {
	c.current.Store(c.newGeneration(nil))
}

func (c *Core) newGeneration(certificate *tls.Certificate) *goproxy.ProxyHttpServer {
	proxy := goproxy.NewProxyHttpServer()
	configureProxyLogging(proxy)
	proxy.Tr = c.transport
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

func defaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}
