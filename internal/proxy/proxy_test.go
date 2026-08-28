package proxy_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/proxy"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestHTTPProxyComposesCORSRepair(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.test" {
			t.Fatalf("Origin = %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "upstream body")
	}))
	defer upstream.Close()

	handler := proxy.New(testTransport(t, upstream.Client()), nil, emptyFacade())
	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/items", nil)
	req.Header.Set("Origin", "https://app.test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.test" {
		t.Fatalf("allow origin = %q", got)
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != "upstream body" {
		t.Fatalf("body = %q", body)
	}
}

func TestProxyComposesLocalPreflight(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	handler := proxy.New(testTransport(t, upstream.Client()), nil, emptyFacade())
	req := httptest.NewRequest(http.MethodOptions, upstream.URL+"/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestDirectCONNECTLeavesHTTPSOpaque(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = io.WriteString(w, "direct")
	}))
	defer upstream.Close()

	proxyServer := httptest.NewServer(proxy.New(
		testTransport(t, upstream.Client()), nil, emptyFacade(),
	))
	defer proxyServer.Close()
	proxyURL := mustParseURL(t, proxyServer.URL)
	clientTransport := testTransport(t, upstream.Client())
	clientTransport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: clientTransport}
	req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.test")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestTrustedHTTPSInterceptionComposesCORSRepair(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = io.WriteString(w, "secure upstream")
	}))
	defer upstream.Close()

	proxyServer, proxyURL, roots := trustedProxyServer(
		t, testTransport(t, upstream.Client()), emptyFacade(),
	)
	defer proxyServer.Close()
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/secure", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.test")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.test" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestHTTPSFacadeProvidesDefaultPortReverseProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.TLS != nil {
			t.Fatal("HTTP upstream received TLS")
		}
		if req.Host != "example.test" {
			t.Fatalf("Host = %q", req.Host)
		}
		if got := req.Header.Get("Origin"); got != "https://app.test" {
			t.Fatalf("Origin = %q", got)
		}
		if got := req.Header.Get("Forwarded"); got != "" {
			t.Fatalf("Forwarded = %q", got)
		}
		if got := req.Header.Get("X-Forwarded-Proto"); got != "" {
			t.Fatalf("X-Forwarded-Proto = %q", got)
		}
		w.Header().Set("Location", "http://example.test/next?q=1#done")
		w.Header().Set("Set-Cookie", "session=plain")
		_, _ = io.WriteString(w, "plain upstream")
	}))
	defer upstream.Close()

	upstreamURL := mustParseURL(t, upstream.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "example.test:80" {
			t.Fatalf("dial address = %q", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstreamURL.Host)
	}
	liveFacade := httpsfacade.NewLive(httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "example.test", Port: 80},
	}))
	proxyServer, proxyURL, roots := trustedProxyServer(t, transport, liveFacade)
	defer proxyServer.Close()
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/request", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.test")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Location"); got != "https://example.test/next?q=1#done" {
		t.Fatalf("Location = %q", got)
	}
	if got := resp.Header.Get("Set-Cookie"); got != "session=plain" {
		t.Fatalf("Set-Cookie = %q", got)
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("Strict-Transport-Security = %q", got)
	}
	if body, _ := io.ReadAll(resp.Body); string(body) != "plain upstream" {
		t.Fatalf("body = %q", body)
	}
}

func TestProxyUsesLatestHTTPSFacadeProjection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "plain upstream")
	}))
	defer upstream.Close()
	upstreamURL := mustParseURL(t, upstream.URL)
	port := mustParsePort(t, upstreamURL.Port())
	liveFacade := httpsfacade.NewLive(httpsfacade.Projection{})
	handler := proxy.New(testTransport(t, upstream.Client()), nil, liveFacade)
	target := "https://" + upstreamURL.Host + "/live"

	before := httptest.NewRecorder()
	handler.ServeHTTP(before, httptest.NewRequest(http.MethodGet, target, nil))
	if before.Code < http.StatusInternalServerError {
		t.Fatalf("unmatched HTTPS status = %d, want proxy failure", before.Code)
	}

	liveFacade.Set(httpsfacade.Project([]upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: upstreamURL.Hostname(), Port: port},
	}))
	after := httptest.NewRecorder()
	handler.ServeHTTP(after, httptest.NewRequest(http.MethodGet, target, nil))
	if after.Code != http.StatusOK || after.Body.String() != "plain upstream" {
		t.Fatalf("matched response = (%d, %q)", after.Code, after.Body.String())
	}
}

func TestProxyGeneratedFailureIsNotCORSRepaired(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errTestDial
	}
	handler := proxy.New(transport, nil, emptyFacade())
	req := httptest.NewRequest(http.MethodGet, "http://upstream.invalid/resource", nil)
	req.Header.Set("Origin", "https://app.test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q", got)
	}
}

func emptyFacade() *httpsfacade.Live {
	return httpsfacade.NewLive(httpsfacade.Projection{})
}

func trustedProxyServer(
	t *testing.T,
	transport *http.Transport,
	liveFacade *httpsfacade.Live,
) (*httptest.Server, *url.URL, *tls.Config) {
	t.Helper()
	certificate, certificatePEM := testHTTPSCertificate(t)
	proxyServer := httptest.NewServer(proxy.New(transport, certificate, liveFacade))
	proxyURL := mustParseURL(t, proxyServer.URL)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		proxyServer.Close()
		t.Fatal("failed to trust gateway CA")
	}
	return proxyServer, proxyURL, &tls.Config{RootCAs: roots}
}

func testHTTPSCertificate(t *testing.T) (*tls.Certificate, []byte) {
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
	return &certificate, certificatePEM
}

func testTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want *http.Transport", client.Transport)
	}
	return transport.Clone()
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustParsePort(t *testing.T, raw string) uint16 {
	t.Helper()
	parsed, err := net.LookupPort("tcp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(parsed)
}

type testDialError struct{}

func (testDialError) Error() string { return "dial failed" }

var errTestDial testDialError
