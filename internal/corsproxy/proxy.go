package corsproxy

import (
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/elazarl/goproxy"
)

// CertificateProvider is the minimal consumer-owned seam used by CORS Proxy.
// The provider owns bounded certificate lookup and expiry; CORS Proxy only
// requests a certificate for the CONNECT hostname.
type CertificateProvider interface {
	CertificateFor(string) (*tls.Certificate, error)
}

type HTTPSFailureDisposition string

const (
	HTTPSFailureExpired    HTTPSFailureDisposition = "expired"
	HTTPSFailureProvider   HTTPSFailureDisposition = "provider-failure"
	providerInvalidRequest                         = "invalid-request"
	providerNotCovered                             = "not-covered"
)

type HTTPSFailure struct {
	Disposition HTTPSFailureDisposition
	Err         error
}

type Core struct {
	proxy          *goproxy.ProxyHttpServer
	provider       atomic.Pointer[providerState]
	onHTTPSFailure func(HTTPSFailure)
}

type providerState struct {
	provider CertificateProvider
}

func New(opts Options) *Core {
	proxy := goproxy.NewProxyHttpServer()
	configureProxyLogging(proxy)
	proxy.Tr = opts.Transport
	if proxy.Tr == nil {
		proxy.Tr = defaultTransport()
	}
	core := &Core{proxy: proxy, onHTTPSFailure: opts.OnHTTPSFailure}
	proxy.OnRequest().HandleConnectFunc(core.handleConnect)
	if opts.Provider != nil {
		core.ReplaceProvider(opts.Provider)
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

	return core
}

type Options struct {
	Provider       CertificateProvider
	Transport      *http.Transport
	OnHTTPSFailure func(HTTPSFailure)
}

func (c *Core) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.proxy.ServeHTTP(w, req)
}

// ReplaceProvider atomically installs a prevalidated provider. UserCA makes
// provider construction and self-testing a precondition, so this operation
// cannot fail.
func (c *Core) ReplaceProvider(provider CertificateProvider) {
	if provider == nil {
		c.DeactivateHTTPS()
		return
	}
	c.provider.Store(&providerState{provider: provider})
}

func (c *Core) DeactivateHTTPS() {
	c.provider.Store(nil)
}

func (c *Core) handleConnect(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	state := c.provider.Load()
	if state == nil {
		return goproxy.OkConnect, host
	}
	hostname := stripConnectPort(host)
	certificate, err := state.provider.CertificateFor(hostname)
	if err == nil && certificate == nil {
		err = errors.New("certificate provider returned no certificate")
	}
	if err != nil {
		disposition := classifyProviderError(err)
		if disposition == providerInvalidRequest || disposition == providerNotCovered {
			return goproxy.OkConnect, host
		}
		failureDisposition := HTTPSFailureProvider
		if disposition == string(HTTPSFailureExpired) {
			failureDisposition = HTTPSFailureExpired
		}
		c.failProvider(state, HTTPSFailure{
			Disposition: failureDisposition,
			Err:         err,
		})
		return goproxy.OkConnect, host
	}
	config := &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{*certificate},
	}
	return &goproxy.ConnectAction{
		Action: goproxy.ConnectMitm,
		TLSConfig: func(string, *goproxy.ProxyCtx) (*tls.Config, error) {
			return config, nil
		},
	}, host
}

func classifyProviderError(err error) string {
	var classified interface{ Disposition() string }
	if !errors.As(err, &classified) {
		return string(HTTPSFailureProvider)
	}
	return classified.Disposition()
}

func (c *Core) failProvider(state *providerState, failure HTTPSFailure) {
	if !c.provider.CompareAndSwap(state, nil) {
		return
	}
	if c.onHTTPSFailure != nil {
		c.onHTTPSFailure(failure)
	}
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

type localPreflight struct{}

func stripConnectPort(host string) string {
	hostname, _, err := net.SplitHostPort(host)
	if err == nil {
		return hostname
	}
	return strings.Trim(host, "[]")
}
