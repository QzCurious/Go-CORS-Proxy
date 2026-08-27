package corsproxy_test

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
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestHTTPProxyForwardsRequestsAndRepairsAllStatuses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "DELETE")
		w.Header().Set("Access-Control-Future", "upstream")
		w.Header().Set("Set-Cookie", "session=secret")
		w.Header().Set("Set-Cookie2", "legacy=secret")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("X-Trace", "abc")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("real upstream body"))
	}))
	defer upstream.Close()

	handler := corsproxy.New(testTransport(t, upstream.Client()), nil, corsproxy.NewHTTPSFacadeRoutes(nil))
	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/v1/items?q=dev", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":      "https://app.local",
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
	if !slices.IsSorted(exposed) {
		t.Fatalf("exposed headers are not sorted: %#v", exposed)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "real upstream body" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestHTTPProxyLeavesResponsesWithoutOriginUntouched(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "https://upstream.example")
		w.Header().Set("Access-Control-Allow-Credentials", "false")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	handler := corsproxy.New(testTransport(t, upstream.Client()), nil, corsproxy.NewHTTPSFacadeRoutes(nil))
	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://upstream.example" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "false" {
		t.Fatalf("allow credentials = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "" {
		t.Fatalf("expose headers = %q", got)
	}
}

func TestHTTPProxyPreservesWildcardVary(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Vary", "*")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	handler := corsproxy.New(testTransport(t, upstream.Client()), nil, corsproxy.NewHTTPSFacadeRoutes(nil))
	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	assertHeaderTokens(t, resp.Header.Values("Vary"), "*")
}

func TestProxyAnswersPreflightLocally(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer upstream.Close()

	handler := corsproxy.New(testTransport(t, upstream.Client()), nil, corsproxy.NewHTTPSFacadeRoutes(nil))
	req := httptest.NewRequest(http.MethodOptions, upstream.URL+"/v1/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
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
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestDirectCONNECTLeavesHTTPSOpaque(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte("direct"))
	}))
	defer upstream.Close()

	proxyServer := httptest.NewServer(corsproxy.New(
		testTransport(t, upstream.Client()), nil, corsproxy.NewHTTPSFacadeRoutes(nil),
	))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport := testTransport(t, upstream.Client())
	clientTransport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: clientTransport}
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
		t.Fatalf("allow origin = %q", got)
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

	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client(), nil)
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

func TestTrustedHTTPSInterceptionForwardsMatchingHTTPOriginWithoutTLS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.TLS != nil {
			t.Fatal("upstream request used TLS")
		}
		if req.URL.Path != "/secure" || req.URL.RawQuery != "mode=dev" {
			t.Fatalf("request target = %q?%q", req.URL.Path, req.URL.RawQuery)
		}
		if got := req.Header.Get("Origin"); got != "https://app.local" {
			t.Fatalf("Origin was rewritten: %q", got)
		}
		w.Header().Set("Location", "http://unchanged.example/next")
		_, _ = io.WriteString(w, "plain upstream")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	routes := corsproxy.NewHTTPSFacadeRoutes([]upstreamlist.OriginSelector{{
		Scheme: "http", Hostname: upstreamURL.Hostname(), Port: upstreamURL.Port(),
	}})
	proxyServer, proxyURL, roots := trustedProxyServer(t, upstream.Client(), routes)
	defer proxyServer.Close()
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: roots,
	}}
	req, err := http.NewRequest(
		http.MethodGet,
		"https://"+upstreamURL.Host+"/secure?mode=dev",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://app.local")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Location"); got != "http://unchanged.example/next" {
		t.Fatalf("Location = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "plain upstream" {
		t.Fatalf("body = %q", body)
	}
}

func TestHTTPSFacadeRoutesApplyLatestPublishedSnapshot(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "plain upstream")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	routes := corsproxy.NewHTTPSFacadeRoutes(nil)
	handler := corsproxy.New(testTransport(t, upstream.Client()), nil, routes)
	target := "https://" + upstreamURL.Host + "/live"

	before := httptest.NewRecorder()
	handler.ServeHTTP(before, httptest.NewRequest(http.MethodGet, target, nil))
	if before.Code < http.StatusInternalServerError {
		t.Fatalf("unmatched HTTPS status = %d, want proxy failure", before.Code)
	}

	routes.Set([]upstreamlist.OriginSelector{{
		Scheme: "http", Hostname: upstreamURL.Hostname(), Port: upstreamURL.Port(),
	}})
	after := httptest.NewRecorder()
	handler.ServeHTTP(after, httptest.NewRequest(http.MethodGet, target, nil))
	if after.Code != http.StatusOK || after.Body.String() != "plain upstream" {
		t.Fatalf("matched response = (%d, %q)", after.Code, after.Body.String())
	}
}

func TestHTTPSFacadePortlessSelectorUsesHTTPDefaultPort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Host != "example.test" {
			t.Fatalf("Host = %q", req.Host)
		}
		_, _ = io.WriteString(w, "plain upstream")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialed := make(chan string, 1)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- address
		return (&net.Dialer{}).DialContext(ctx, network, upstreamURL.Host)
	}
	handler := corsproxy.New(
		transport,
		nil,
		corsproxy.NewHTTPSFacadeRoutes([]upstreamlist.OriginSelector{{
			Scheme: "http", Hostname: "example.test",
		}}),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "https://example.test/default", nil),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "plain upstream" {
		t.Fatalf("response = (%d, %q)", recorder.Code, recorder.Body.String())
	}
	if address := <-dialed; address != "example.test:80" {
		t.Fatalf("dial address = %q, want HTTP default port", address)
	}
}

func TestProxyGeneratedFailureIsNotCORSRepaired(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errTestDial
	}
	handler := corsproxy.New(transport, nil, corsproxy.NewHTTPSFacadeRoutes(nil))
	req := httptest.NewRequest(http.MethodGet, "http://upstream.invalid/resource", nil)
	req.Header.Set("Origin", "https://app.local")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q", got)
	}
}

func trustedProxyServer(
	t *testing.T,
	upstreamClient *http.Client,
	routes *corsproxy.HTTPSFacadeRoutes,
) (*httptest.Server, *url.URL, *tls.Config) {
	t.Helper()
	certificate, certificatePEM := testHTTPSCertificate(t)
	if routes == nil {
		routes = corsproxy.NewHTTPSFacadeRoutes(nil)
	}
	handler := corsproxy.New(testTransport(t, upstreamClient), certificate, routes)
	proxyServer := httptest.NewServer(handler)
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

type testDialError struct{}

func (testDialError) Error() string { return "dial failed" }

var errTestDial testDialError
