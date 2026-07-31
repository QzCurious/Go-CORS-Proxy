package corsproxy

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elazarl/goproxy"
)

const (
	leafValidity    = 30 * 24 * time.Hour
	leafCacheMaxAge = 24 * time.Hour
)

type HTTPSGeneration struct {
	Certificate tls.Certificate
	ExpiresAt   time.Time
}

type Options struct {
	InterceptHTTPS  bool
	HTTPSGeneration *HTTPSGeneration
	Transport       *http.Transport
	OnHTTPSFailure  func(HTTPSFailure)
	GenerateLeaf    func(tls.Certificate, string) (*tls.Certificate, error)
}

type HTTPSFailureKind string

const (
	HTTPSFailureReadiness    HTTPSFailureKind = "readiness"
	HTTPSFailureInterception HTTPSFailureKind = "interception"
)

type HTTPSFailure struct {
	Kind HTTPSFailureKind
	Err  error
}

type Core struct {
	proxy           *goproxy.ProxyHttpServer
	httpsGeneration atomic.Pointer[httpsGeneration]
	onHTTPSFailure  func(HTTPSFailure)
	generateLeaf    func(tls.Certificate, string) (*tls.Certificate, error)
}

type httpsGeneration struct {
	certificate tls.Certificate
	expiresAt   time.Time
	leafCache   *memoryCertStore
}

func New(opts Options) (*Core, error) {
	proxy := goproxy.NewProxyHttpServer()
	configureProxyLogging(proxy)
	proxy.Tr = opts.Transport
	if proxy.Tr == nil {
		proxy.Tr = defaultTransport()
	}
	proxy.CertStore = newMemoryCertStore()

	generateLeaf := opts.GenerateLeaf
	if generateLeaf == nil {
		generateLeaf = signHostCertificate
	}
	core := &Core{proxy: proxy, onHTTPSFailure: opts.OnHTTPSFailure, generateLeaf: generateLeaf}
	proxy.OnRequest().HandleConnectFunc(core.handleConnect)
	if opts.InterceptHTTPS {
		if opts.HTTPSGeneration == nil {
			return nil, fmt.Errorf("trusted HTTPS interception requires a generation")
		}
		if err := core.ActivateHTTPS(*opts.HTTPSGeneration); err != nil {
			return nil, err
		}
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

	return core, nil
}

func (c *Core) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.proxy.ServeHTTP(w, req)
}

// ActivateHTTPS atomically swaps signer, certificate, and generation-owned
// leaf cache. A handshake that already loaded the previous generation keeps it.
func (c *Core) ActivateHTTPS(input HTTPSGeneration) error {
	if len(input.Certificate.Certificate) == 0 || input.Certificate.PrivateKey == nil || input.ExpiresAt.IsZero() {
		return fmt.Errorf("trusted HTTPS interception requires complete TLS signing material")
	}
	generation := &httpsGeneration{
		certificate: input.Certificate,
		expiresAt:   input.ExpiresAt,
		leafCache:   newMemoryCertStore(),
	}
	c.httpsGeneration.Store(generation)
	return nil
}

func (c *Core) DeactivateHTTPS() {
	c.httpsGeneration.Store(nil)
}

func (c *Core) handleConnect(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	generation := c.httpsGeneration.Load()
	if generation == nil {
		return goproxy.OkConnect, host
	}
	if !time.Now().Before(generation.expiresAt) {
		c.failHTTPS(generation, HTTPSFailure{
			Kind: HTTPSFailureReadiness,
			Err:  fmt.Errorf("Installed User CA expired at %s", generation.expiresAt.Format(time.RFC3339)),
		})
		return goproxy.OkConnect, host
	}
	hostname := stripConnectPort(host)
	cert, err := generation.leafCache.Fetch(hostname, func() (*tls.Certificate, error) {
		return c.generateLeaf(generation.certificate, hostname)
	})
	if err != nil {
		c.failHTTPS(generation, HTTPSFailure{Kind: HTTPSFailureInterception, Err: err})
		return goproxy.OkConnect, host
	}
	config := &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{*cert},
	}
	return &goproxy.ConnectAction{
		Action: goproxy.ConnectMitm,
		TLSConfig: func(string, *goproxy.ProxyCtx) (*tls.Config, error) {
			return config, nil
		},
	}, host
}

func (c *Core) failHTTPS(generation *httpsGeneration, failure HTTPSFailure) {
	if !c.httpsGeneration.CompareAndSwap(generation, nil) {
		return
	}
	if c.onHTTPSFailure != nil {
		c.onHTTPSFailure(failure)
	}
}

func configureProxyLogging(proxy *goproxy.ProxyHttpServer) {
	if proxyDebugEnabled() {
		proxy.Verbose = true
		return
	}
	proxy.Verbose = false
	proxy.Logger = log.New(io.Discard, "", 0)
}

func proxyDebugEnabled() bool {
	switch strings.ToLower(os.Getenv("SEAMLESS_CORS_DEBUG_PROXY")) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func defaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

type localPreflight struct{}

func signHostCertificate(caCert tls.Certificate, hostname string) (*tls.Certificate, error) {
	caLeaf := caCert.Leaf
	if caLeaf == nil {
		var err error
		caLeaf, err = x509.ParseCertificate(caCert.Certificate[0])
		if err != nil {
			return nil, err
		}
	}
	signer, ok := caCert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("CA private key cannot sign certificates")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	notBefore := time.Now().Add(-time.Minute)
	template := x509.Certificate{
		SerialNumber: serial,
		Issuer:       caLeaf.Subject,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"seamless-cors local MITM proxy"},
		},
		NotBefore:             notBefore,
		NotAfter:              minTime(notBefore.Add(leafValidity), caLeaf.NotAfter),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{hostname}
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, caLeaf, &leafKey.PublicKey, signer)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	chain := make([][]byte, 1+len(caCert.Certificate))
	chain[0] = der
	copy(chain[1:], caCert.Certificate)
	return &tls.Certificate{
		Certificate: chain,
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}, nil
}

func stripConnectPort(host string) string {
	hostname, _, err := net.SplitHostPort(host)
	if err == nil {
		return hostname
	}
	return strings.Trim(host, "[]")
}

type memoryCertStore struct {
	mu    sync.Mutex
	certs map[string]cachedCert
	now   func() time.Time
}

type cachedCert struct {
	cert        *tls.Certificate
	generatedAt time.Time
}

func newMemoryCertStore() *memoryCertStore {
	return &memoryCertStore{certs: map[string]cachedCert{}, now: time.Now}
}

func (s *memoryCertStore) Fetch(hostname string, gen func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if cached, ok := s.certs[hostname]; ok && now.Sub(cached.generatedAt) <= leafCacheMaxAge {
		return cached.cert, nil
	}
	cert, err := gen()
	if err != nil {
		return nil, err
	}
	s.certs[hostname] = cachedCert{cert: cert, generatedAt: now}
	return cert, nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
