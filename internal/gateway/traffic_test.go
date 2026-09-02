package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

func TestRuntimeProjectsSourcesIndependentlyThenMerges(t *testing.T) {
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\nshared.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: fileobservation.Contents("shared.example.test\ndirectory.example.test\n")},
	}, defaultProxyTransport(), userCAState{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	state := runtime.snapshot()
	if state.UpstreamCount != 3 || len(state.UpstreamLists) != 2 || !state.HTTPDemand {
		t.Fatalf("merged runtime state = %#v", state)
	}
}

func TestRejectedSourceFailsClosedWithoutRemovingHealthySource(t *testing.T) {
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: fileobservation.Contents("directory.example.test\n")},
	}, defaultProxyTransport(), userCAState{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	drainRuntimeRequests(t, runtime)
	if err := runtime.applyUpstreamListSourceOutcome(1, fileobservation.Contents{0xff}); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamCount != 1 || state.UpstreamLists[1].ProjectionIssue == nil {
		t.Fatalf("source-local rejection = %#v", state)
	}
}

func drainRuntimeRequests(t *testing.T, runtime *trafficRuntime) {
	t.Helper()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-runtime.DeliveryRequests():
			case <-runtime.UserCAAssessmentRequests():
			case <-stop:
				return
			}
		}
	}()
}

func TestSourceReadFailureRetainsProjection(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("api.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	readErr := fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	if err := runtime.applyUpstreamListOutcome(readErr); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamCount != 1 || state.UpstreamLists[0].FileSyncIssue == nil {
		t.Fatalf("retained state = %#v", state)
	}
}

func TestTrafficDemandAndServedOutcomeDerivation(t *testing.T) {
	tests := []struct {
		name                    string
		contents                string
		ca                      userCAState
		httpDemand, httpsDemand bool
		httpServed, httpsServed bool
		facadeServed            bool
	}{
		{name: "HTTP origin without UserCA", contents: "http://plain.example.test\n", httpDemand: true, httpServed: true},
		{name: "HTTPS origin without UserCA", contents: "https://secure.example.test\n", httpsDemand: true},
		{name: "host without UserCA", contents: "api.example.test\n", httpDemand: true, httpServed: true},
		{name: "host with UserCA", contents: "api.example.test\n", ca: testUserCAState(t, time.Now().Add(time.Hour), false), httpDemand: true, httpsDemand: true, httpServed: true, httpsServed: true},
		{name: "HTTP facade with UserCA", contents: "http://plain.example.test\n", ca: testUserCAState(t, time.Now().Add(time.Hour), false), httpDemand: true, httpServed: true, facadeServed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(tt.contents))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTrafficTestRuntime(runtime)
			if tt.ca.Usable {
				done := make(chan struct{})
				go func() { runtime.AdoptUserCA(tt.ca, nil); close(done) }()
				<-runtime.DeliveryRequests()
				<-done
			}
			state := runtime.snapshot()
			if state.HTTPDemand != tt.httpDemand || state.HTTPSDemand != tt.httpsDemand ||
				state.ServedHTTPCORS != tt.httpServed || state.ServedHTTPSCORS != tt.httpsServed ||
				state.ServedHTTPSFacade != tt.facadeServed || !state.TrafficProjectionCurrent {
				t.Fatalf("traffic state = %#v", state)
			}
		})
	}
}

func TestTrafficStatusUsesServedProjectionAndRoutingNotProjectionCurrency(t *testing.T) {
	status := trafficStatus(runtimeState{
		HTTPDemand:               true,
		HTTPSDemand:              true,
		ServedHTTPCORS:           true,
		ServedHTTPSCORS:          true,
		ServedHTTPSFacade:        true,
		TrafficProjectionCurrent: false,
		UserCAUsable:             true,
		UserCAIdentityMatches:    true,
	}, true)
	if status.HTTPCORS != TrafficFeatureActive || status.HTTPSCORS != TrafficFeatureActive ||
		status.HTTPSFacade != TrafficFeatureActive || status.ProjectionCurrent {
		t.Fatalf("traffic status = %#v", status)
	}
}

func TestTrafficStatusDistinguishesBlockedFromInactive(t *testing.T) {
	status := trafficStatus(runtimeState{HTTPDemand: true, HTTPSDemand: true}, false)
	if status.HTTPCORS != TrafficFeatureBlocked || status.HTTPSCORS != TrafficFeatureBlocked ||
		status.HTTPSFacade != TrafficFeatureInactive {
		t.Fatalf("traffic status = %#v", status)
	}
}

func TestTrafficProjectionSwitchPublishesPACAndProxyTogether(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("http://plain.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	before := runtime.live.current.Load()
	if strings.Contains(before.pacContent, `"scheme":"https"`) {
		t.Fatalf("initial PAC unexpectedly contains HTTPS: %s", before.pacContent)
	}
	done := make(chan struct{})
	go func() { runtime.AdoptUserCA(testUserCAState(t, time.Now().Add(time.Hour), false), nil); close(done) }()
	<-runtime.DeliveryRequests()
	<-done
	after := runtime.live.current.Load()
	if before == after || !strings.Contains(after.pacContent, `"scheme":"https"`) || after.proxy == nil {
		t.Fatalf("served switch did not publish coherent projection: before=%p after=%p", before, after)
	}
	if !strings.Contains(before.pacContent, `plain.example.test`) {
		t.Fatal("previous immutable projection was mutated")
	}
}

func TestSemanticallyEquivalentSourceUpdateDoesNotRequestDelivery(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("b.example.test\na.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	before := runtime.live.current.Load()
	done := make(chan error, 1)
	go func() {
		done <- runtime.applyUpstreamListOutcome(fileobservation.Contents("A.EXAMPLE.TEST\nb.example.test\nhttps://bad.example.test/path\n"))
	}()
	<-runtime.UserCAAssessmentRequests()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runtime.live.current.Load() != before {
		t.Fatal("selector order or warning replaced the served traffic projection")
	}
	select {
	case <-runtime.DeliveryRequests():
		t.Fatal("warning-only equivalent projection requested PAC delivery")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAdoptedUpdateRequestsUserCAReassessmentOnlyWhenNotUsable(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	deliveries := runtime.DeliveryRequests()
	assessments := runtime.UserCAAssessmentRequests()
	firstDone := make(chan error, 1)
	go func() { firstDone <- runtime.applyUpstreamListOutcome(fileobservation.Contents("api.example.test\n")) }()
	<-deliveries
	select {
	case <-assessments:
	case <-time.After(time.Second):
		t.Fatal("not-usable UserCA did not request reassessment")
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}

	adopted := make(chan struct{})
	go func() { runtime.AdoptUserCA(testUserCAState(t, time.Now().Add(time.Hour), false), nil); close(adopted) }()
	<-deliveries
	<-adopted
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- runtime.applyUpstreamListOutcome(fileobservation.Contents("other.example.test\n"))
	}()
	<-deliveries
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-assessments:
		t.Fatal("usable UserCA was reassessed after source update")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServedTrafficSwitchDoesNotDrainAdmittedRequest(t *testing.T) {
	admitted := make(chan struct{})
	release := make(chan struct{})
	old := &servedTrafficProjection{proxy: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(admitted)
		<-release
		_, _ = io.WriteString(w, "old")
	})}
	live := newLiveTrafficProjection()
	live.Store(old)
	oldResult := httptest.NewRecorder()
	oldDone := make(chan struct{})
	go func() {
		live.serveProxy(oldResult, httptest.NewRequest(http.MethodGet, "http://old.example.test", nil))
		close(oldDone)
	}()
	<-admitted
	live.Store(&servedTrafficProjection{proxy: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "new")
	})})
	newResult := httptest.NewRecorder()
	live.serveProxy(newResult, httptest.NewRequest(http.MethodGet, "http://new.example.test", nil))
	close(release)
	<-oldDone
	if oldResult.Body.String() != "old" || newResult.Body.String() != "new" {
		t.Fatalf("old = %q, new = %q", oldResult.Body.String(), newResult.Body.String())
	}
}

func TestRuntimeCloseClosesGatewayOwnedProxyIdleConnections(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer upstream.Close()
	closed := make(chan struct{})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &closeTrackingConn{Conn: conn, closed: closed}, nil
	}
	runtime, err := newRuntimeWithTransport("/tmp/upstreams.txt", nil, fileobservation.Contents(nil), transport)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	runtime.live.serveProxy(recorder, httptest.NewRequest(http.MethodGet, upstream.URL, nil))
	response := recorder.Result()
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	_ = runtime.CloseTraffic()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("runtime close left outbound connection idle")
	}
}

type closeTrackingConn struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func (c *closeTrackingConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func testUserCAState(t *testing.T, expiresAt time.Time, renewalDue bool) userCAState {
	t.Helper()
	if expiresAt.IsZero() {
		t.Fatal("test UserCA state requires expiry")
	}
	return userCAState{
		Usable:          true,
		ExpiresAt:       expiresAt,
		RenewalDue:      renewalDue,
		Identity:        "test-userca",
		signingMaterial: &tls.Certificate{},
	}
}

func closeTrafficTestRuntime(runtime *trafficRuntime) {
	for _, listener := range runtime.listeners {
		_ = listener.Close()
	}
}

func writeTrafficTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createTrafficConfig(t *testing.T, upstreams string) (*fileobservation.Observation, fileobservation.Outcome, string) {
	t.Helper()
	return createTrafficConfigAtCurrentHome(t, upstreams)
}

func createTrafficConfigAtCurrentHome(t *testing.T, upstreams string) (*fileobservation.Observation, fileobservation.Outcome, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	writeTrafficTestFile(t, path, upstreams)
	observation := fileobservation.Open(path)
	return observation, <-observation.Outcomes(), path
}
