package corsproxy

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
)

func TestHTTPProxyForwardsRequestsAndRepairsAllStatuses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("real upstream body"))
	}))
	defer upstream.Close()

	core := newTestCore(t, Options{Transport: testTransport(t, upstream.Client())})
	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/v1/items?q=dev", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "real upstream body" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestProxyAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	core := newTestCore(t, Options{Transport: testTransport(t, upstream.Client())})
	req := httptest.NewRequest(http.MethodOptions, upstream.URL+"/v1/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()

	core.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":          "null",
		"Access-Control-Allow-Methods":         "PATCH",
		"Access-Control-Allow-Headers":         "X-Dev",
		"Access-Control-Allow-Private-Network": "true",
		"Access-Control-Max-Age":               "600",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestTrustedHTTPSInterceptionRepairsResponseAndCompletes(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Upstream", "ok")
		_, _ = w.Write([]byte("secure upstream body"))
	}))
	defer upstream.Close()

	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client())
	defer proxyServer.Close()

	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secure upstream body" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestTrustedHTTPSInterceptionAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client())
	defer proxyServer.Close()

	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodOptions, upstream.URL+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")
	req.Header.Set("Access-Control-Request-Method", "PATCH")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.local" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestHTTPSPreparationFailureDirectTunnelsDetectingRequest(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte("direct"))
	}))
	defer upstream.Close()
	provider, _ := testHTTPSProvider(t)
	provider.(*testProvider).err = &testProviderError{
		disposition: string(HTTPSFailureProvider),
		err:         errors.New("leaf generation failed"),
	}
	failures := make(chan HTTPSFailure, 1)
	core := newTestCore(t, Options{
		Provider:       provider,
		Transport:      testTransport(t, upstream.Client()),
		OnHTTPSFailure: func(failure HTTPSFailure) { failures <- failure },
	})
	proxyServer := httptest.NewServer(core)
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := testTransport(t, upstream.Client())
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: transport}
	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("direct-tunneled response was CORS-repaired: %q", got)
	}
	failure := <-failures
	if failure.Disposition != HTTPSFailureProvider || !strings.Contains(failure.Err.Error(), "leaf generation failed") {
		t.Fatalf("failure = %#v", failure)
	}
	if core.provider.Load() != nil {
		t.Fatal("failed interception remained active")
	}
}

func TestExpiredUserCADetectedAtRequestBoundaryDirectTunnels(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}))
	defer upstream.Close()
	provider, _ := testHTTPSProvider(t)
	provider.(*testProvider).err = &testProviderError{
		disposition: string(HTTPSFailureExpired),
		err:         errors.New("provider expired"),
	}
	failures := make(chan HTTPSFailure, 1)
	core := newTestCore(t, Options{
		Provider:       provider,
		OnHTTPSFailure: func(failure HTTPSFailure) { failures <- failure },
	})
	proxyServer := httptest.NewServer(core)
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := testTransport(t, upstream.Client())
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expired detecting request was intercepted: %q", got)
	}
	if failure := <-failures; failure.Disposition != HTTPSFailureExpired {
		t.Fatalf("expiry failure = %#v", failure)
	}
}

func TestInvalidHostnameStaysRequestLocal(t *testing.T) {
	provider, _ := testHTTPSProvider(t)
	provider.(*testProvider).err = &testProviderError{
		disposition: "invalid-request",
		err:         errors.New("unsupported hostname"),
	}
	var failures atomic.Int32
	core := newTestCore(t, Options{
		Provider: provider,
		OnHTTPSFailure: func(HTTPSFailure) {
			failures.Add(1)
		},
	})

	action, host := core.handleConnect("bad host:443", nil)
	if action != goproxy.OkConnect || host != "bad host:443" {
		t.Fatalf("invalid hostname action = %#v host = %q", action, host)
	}
	if failures.Load() != 0 {
		t.Fatal("invalid hostname reported a Gateway failure")
	}
	if core.provider.Load() == nil {
		t.Fatal("invalid hostname disabled the provider")
	}
}

func TestUncoveredHostnameIsDirectTunneledWithoutDisablingProvider(t *testing.T) {
	provider, _ := testHTTPSProvider(t)
	provider.(*testProvider).err = &testProviderError{
		disposition: "not-covered",
		err:         errors.New("outside certificate scope"),
	}
	var failures atomic.Int32
	core := newTestCore(t, Options{
		Provider: provider,
		OnHTTPSFailure: func(HTTPSFailure) {
			failures.Add(1)
		},
	})

	action, host := core.handleConnect("outside.example.test:443", nil)
	if action != goproxy.OkConnect || host != "outside.example.test:443" {
		t.Fatalf("uncovered hostname action = %#v host = %q", action, host)
	}
	if failures.Load() != 0 {
		t.Fatal("uncovered hostname reported a Gateway failure")
	}
	if core.provider.Load() == nil {
		t.Fatal("uncovered hostname disabled the provider")
	}
}

func TestPerHostLeafExpiryIsCappedByUserCA(t *testing.T) {
	provider, _ := testHTTPSProvider(t)
	userCAExpiry := time.Now().Add(time.Hour).Truncate(time.Second)
	provider.(*testProvider).certificate.Leaf.NotAfter = userCAExpiry

	leaf, err := provider.CertificateFor("api.example.test")

	if err != nil {
		t.Fatal(err)
	}
	if leaf.Leaf.NotAfter.After(userCAExpiry) {
		t.Fatalf("leaf expiry %s exceeds UserCA expiry %s", leaf.Leaf.NotAfter, userCAExpiry)
	}
}

func TestProxyLoggingIsQuietByDefault(t *testing.T) {
	t.Setenv("SEAMLESS_CORS_DEBUG_PROXY", "")
	core := newTestCore(t, Options{})

	if core.proxy.Verbose {
		t.Fatal("proxy should not be verbose by default")
	}
}

func TestProxyLoggingDebugEnvEnablesVerboseLogs(t *testing.T) {
	t.Setenv("SEAMLESS_CORS_DEBUG_PROXY", "1")
	core := newTestCore(t, Options{})

	if !core.proxy.Verbose {
		t.Fatal("proxy should be verbose with debug env")
	}
}

func trustedProxyServer(t *testing.T, upstreamClient *http.Client) (*httptest.Server, *url.URL, *tls.Config) {
	t.Helper()
	provider, certificatePEM := testHTTPSProvider(t)
	core := newTestCore(t, Options{
		Provider:  provider,
		Transport: testTransport(t, upstreamClient),
	})
	proxyServer := httptest.NewServer(core)
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		proxyServer.Close()
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		proxyServer.Close()
		t.Fatal("failed to trust gateway CA")
	}
	return proxyServer, proxyURL, &tls.Config{RootCAs: roots}
}

type testProvider struct {
	certificate tls.Certificate
	err         error
}

func (p *testProvider) CertificateFor(hostname string) (*tls.Certificate, error) {
	if p.err != nil {
		return nil, p.err
	}
	return signTestHostCertificate(p.certificate, hostname)
}

type testProviderError struct {
	disposition string
	err         error
}

func (e *testProviderError) Error() string       { return e.err.Error() }
func (e *testProviderError) Unwrap() error       { return e.err }
func (e *testProviderError) Disposition() string { return e.disposition }

func testHTTPSProvider(t *testing.T) (CertificateProvider, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "proxy test root"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	certificate.Leaf, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testProvider{certificate: certificate}, certificatePEM
}

func signTestHostCertificate(caCert tls.Certificate, hostname string) (*tls.Certificate, error) {
	caLeaf := caCert.Leaf
	signer, ok := caCert.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("CA private key cannot sign certificates")
	}
	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Issuer:                caLeaf.Subject,
		Subject:               pkix.Name{CommonName: hostname},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              minTestTime(now.Add(30*24*time.Hour), caLeaf.NotAfter),
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
	return &tls.Certificate{Certificate: [][]byte{der, caCert.Certificate[0]}, PrivateKey: leafKey, Leaf: leaf}, nil
}

func minTestTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func newTestCore(t *testing.T, opts Options) *Core {
	t.Helper()
	core, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func testTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want *http.Transport", client.Transport)
	}
	return transport.Clone()
}
