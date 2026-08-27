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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestGatewayFiltersHTTPOriginSelectorsForCORSProxy(t *testing.T) {
	projection := upstreamlist.Projection{OriginSelectors: []upstreamlist.OriginSelector{
		{Scheme: "https", Hostname: "secure.example.test"},
		{Scheme: "http", Hostname: "plain.example.test"},
		{Scheme: "http", Hostname: "local.example.test", Port: "3000"},
	}}
	want := []upstreamlist.OriginSelector{
		{Scheme: "http", Hostname: "plain.example.test"},
		{Scheme: "http", Hostname: "local.example.test", Port: "3000"},
	}
	if got := httpOriginSelectors(projection); !reflect.DeepEqual(got, want) {
		t.Fatalf("HTTP Origin Selectors = %#v, want %#v", got, want)
	}
}

func TestRuntimeClassifiesInitialFileSyncIssue(t *testing.T) {
	observed := fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("source unavailable")}
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, observed)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	issue := runtime.snapshot().UpstreamLists[0].FileSyncIssue
	if issue == nil || issue.Kind != FileSyncIssueFileUnreadable || !strings.Contains(issue.Cause, "source unavailable") {
		t.Fatalf("file sync issue = %#v", issue)
	}
}

func TestRuntimeProjectsSourcesIndependentlyThenMerges(t *testing.T) {
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\nshared.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: fileobservation.Contents("shared.example.test\ndirectory.example.test\n")},
	}, defaultProxyTransport())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	state := runtime.snapshot()
	if state.UpstreamCount != 3 || len(state.UpstreamLists) != 2 {
		t.Fatalf("merged runtime state = %#v", state)
	}
	if state.UpstreamLists[0].Kind != UpstreamListSourceGlobal || state.UpstreamLists[1].Kind != UpstreamListSourceDirectory {
		t.Fatalf("source order = %#v", state.UpstreamLists)
	}
}

func TestRejectedSourceFailsClosedWithoutRemovingHealthySource(t *testing.T) {
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: fileobservation.Contents("directory.example.test\n")},
	}, defaultProxyTransport())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	if err := runtime.applyUpstreamListSourceOutcome(1, fileobservation.Contents{0xff}); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamCount != 1 || state.UpstreamLists[1].ProjectionIssue == nil || state.UpstreamLists[0].ProjectionIssue != nil {
		t.Fatalf("source-local rejection state = %#v", state)
	}
}

func TestSourceReadFailureRetainsItsProjectionWhileOtherSourceChanges(t *testing.T) {
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: fileobservation.Contents("directory.example.test\n")},
	}, defaultProxyTransport())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	readErr := fileobservation.ReadError{Path: "/project/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	if err := runtime.applyUpstreamListSourceOutcome(1, readErr); err != nil {
		t.Fatal(err)
	}
	if err := runtime.applyUpstreamListSourceOutcome(0, fileobservation.Contents("changed.example.test\n")); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamCount != 2 || state.UpstreamLists[1].FileSyncIssue == nil {
		t.Fatalf("independent source state = %#v", state)
	}
}

func TestOptionalDirectorySourceTreatsMissingAsEmptyAndKeepsObserving(t *testing.T) {
	missing := fileobservation.ReadError{Path: "/project/upstreams.txt", Cause: os.ErrNotExist}
	runtime, err := newRuntimeFromSources([]runtimeUpstreamListInput{
		{kind: UpstreamListSourceGlobal, path: "/config/upstreams.txt", initial: fileobservation.Contents("global.example.test\n")},
		{kind: UpstreamListSourceDirectory, path: "/project/upstreams.txt", optional: true, initial: missing},
	}, defaultProxyTransport())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	state := runtime.snapshot()
	if state.UpstreamCount != 1 || state.UpstreamLists[1].FileSyncIssue != nil {
		t.Fatalf("initial optional source state = %#v", state)
	}
	if err := runtime.applyUpstreamListSourceOutcome(1, fileobservation.Contents("directory.example.test\n")); err != nil {
		t.Fatal(err)
	}
	if state = runtime.snapshot(); state.UpstreamCount != 2 {
		t.Fatalf("created optional source state = %#v", state)
	}
	if err := runtime.applyUpstreamListSourceOutcome(1, missing); err != nil {
		t.Fatal(err)
	}
	if state = runtime.snapshot(); state.UpstreamCount != 1 || state.UpstreamLists[1].FileSyncIssue != nil {
		t.Fatalf("removed optional source state = %#v", state)
	}
}

func TestProjectionFailureIsIndependentAndFailClosed(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("api.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents{0xff}); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamLists[0].ProjectionIssue == nil || state.UpstreamLists[0].FileSyncIssue != nil || state.UpstreamCount != 0 {
		t.Fatalf("rejected contents state = %#v", state)
	}

	readErr := fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	if err := runtime.applyUpstreamListOutcome(readErr); err != nil {
		t.Fatal(err)
	}
	state = runtime.snapshot()
	if state.UpstreamLists[0].FileSyncIssue == nil || state.UpstreamLists[0].ProjectionIssue == nil || state.UpstreamCount != 0 {
		t.Fatalf("observation failure did not preserve projection state = %#v", state)
	}
}

func TestSuccessfulProjectionClearsIssuesAndPublishesPAC(t *testing.T) {
	readErr := fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("temporarily unavailable")}
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, readErr)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents{0xff}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents(nil)); err != nil {
		t.Fatal(err)
	}
	state := runtime.snapshot()
	if state.UpstreamLists[0].FileSyncIssue != nil || state.UpstreamLists[0].ProjectionIssue != nil || state.UpstreamCount != 0 {
		t.Fatalf("cleared equal projection state = %#v", state)
	}
	select {
	case projection := <-runtime.PACProjections():
		if strings.Contains(projection, `"hostname"`) {
			t.Fatalf("empty projection contains a route: %s", projection)
		}
	case <-time.After(time.Second):
		t.Fatal("successful projection did not publish PAC projection")
	}
}

func TestEquivalentFileSyncIssueDoesNotInvalidateStatusAgain(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	outcome := fileobservation.ReadError{Path: "/tmp/upstreams.txt", Cause: errors.New("missing")}

	if err := runtime.applyUpstreamListOutcome(outcome); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.RuntimeChanges():
	case <-time.After(time.Second):
		t.Fatal("first issue did not invalidate status")
	}
	if err := runtime.applyUpstreamListOutcome(outcome); err != nil {
		t.Fatal(err)
	}
	select {
	case kind := <-runtime.RuntimeChanges():
		t.Fatalf("equivalent issue invalidated status: %v", kind)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPACPublicationFollowsEverySuccessfulProjection(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, 0, errs)
	desired := runtime.PACProjections()

	writeTrafficTestFile(t, upstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.UpstreamLists[0].Warnings) == 1
	})
	select {
	case state := <-desired:
		if !strings.Contains(state, "api.example.test") || strings.Contains(state, "bad.example.test") {
			t.Fatalf("warning-bearing projection produced unexpected PAC: %s", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for warning-bearing PAC projection")
	}

	writeTrafficTestFile(t, upstreamPath, "changed.example.test\n")
	select {
	case state := <-desired:
		if !strings.Contains(state, "changed.example.test") {
			t.Fatalf("PAC projection does not contain changed host: %s", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for desired Upstream List input")
	}
}

func TestPACPublicationExcludesInactiveHTTPSRoutes(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	select {
	case <-runtime.PACProjections():
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, 0, errs)
	desired := runtime.PACProjections()

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool { return state.HTTPSPipeline != nil })
	select {
	case state := <-desired:
		if strings.Contains(state, "secure.example.test") {
			t.Fatalf("inactive HTTPS selector entered PAC input: %s", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PAC projection")
	}
}

func TestHTTPSIntentAdmitsAssessingPipelineAndRequestsAssessment(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchUpstreamList(ctx, 0, errs)

	writeTrafficTestFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.HTTPSPipeline != nil && state.HTTPSPipeline.Phase == HTTPSPipelineAssessing
	})

	state := runtime.snapshot()
	if state.HTTPSPipeline.Readiness != "" {
		t.Fatalf("assessing pipeline has readiness = %q", state.HTTPSPipeline.Readiness)
	}
	select {
	case kind := <-runtime.RuntimeChanges():
		if kind != HTTPSAssessmentRequested {
			t.Fatalf("runtime change = %v, want assessment request", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTPS Intent did not request assessment")
	}
}

func TestInstalledUserCASettlesActivePipelineAndPublishesHTTPSPACInput(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	runtime.SetInitialHTTPSAssessment(userCAState{}, nil)
	pipeline := runtime.AdoptInstalledUserCA(testUserCAState(t, time.Now().Add(24*time.Hour), false))
	state := runtime.snapshot()
	if pipeline == nil || pipeline.Readiness != HTTPSReadinessReady || !runtime.interceptionActive() {
		t.Fatalf("recovered state = %#v", state)
	}
	select {
	case desired := <-runtime.PACProjections():
		if !strings.Contains(desired, "secure.example.test") {
			t.Fatalf("PAC projection did not enable HTTPS route: %s", desired)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for desired PAC state")
	}
}

func TestHTTPSDeadlineSignalSurvivesStatusInvalidation(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "api.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)

	runtime.publishRuntimeChange(HTTPSDeadlineReached)
	runtime.publishRuntimeChange(RuntimeStatusChanged)

	select {
	case got := <-runtime.RuntimeChanges():
		if got != HTTPSDeadlineReached {
			t.Fatalf("runtime change = %v, want deadline signal", got)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline signal was lost")
	}
}

func TestDeadlineMovesCurrentReadyPipelineBackToAssessing(t *testing.T) {
	source, initial, upstreamPath := createTrafficConfig(t, "https://secure.example.test\n")
	runtime, err := newRuntime(upstreamPath, source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	expiresAt := time.Now().Add(time.Hour)
	runtime.SetInitialHTTPSAssessment(testUserCAState(t, expiresAt, false), nil)
	before := runtime.snapshot()
	if _, ok := runtime.BeginHTTPSDeadlineAssessment(before.HTTPSGeneration); !ok {
		t.Fatal("deadline did not admit a fresh assessment")
	}
	state := runtime.snapshot()
	if state.HTTPSPipeline == nil || state.HTTPSPipeline.Phase != HTTPSPipelineAssessing || state.HTTPSPipeline.Readiness != "" {
		t.Fatalf("deadline state = %#v", state)
	}
}

func TestHTTPSPipelineDetailsPreserveTheirSource(t *testing.T) {
	expiry := time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)
	notUsable := settledHTTPSPipeline(userCAState{}, nil)
	if notUsable.UnmetIntent == nil || notUsable.UserCAAssessmentIssue != nil {
		t.Fatalf("not-usable detail = %#v", notUsable)
	}
	assessmentIssue := settledHTTPSPipeline(userCAState{}, context.DeadlineExceeded)
	if assessmentIssue.UserCAAssessmentIssue == nil || assessmentIssue.UnmetIntent != nil {
		t.Fatalf("assessment issue = %#v", assessmentIssue)
	}
	invalidUsable := settledHTTPSPipeline(userCAState{Usable: true, ExpiresAt: expiry}, nil)
	if invalidUsable.Readiness == HTTPSReadinessReady || invalidUsable.UnmetIntent == nil {
		t.Fatalf("usable state without signing material = %#v", invalidUsable)
	}
	usable := testUserCAState(t, expiry, true)
	ready := settledHTTPSPipeline(usable, nil)
	if ready.Readiness != HTTPSReadinessReady || ready.UnmetIntent != nil || ready.UserCAAssessmentIssue != nil {
		t.Fatalf("ready detail = %#v", ready)
	}
}

func TestStaleHTTPSAssessmentCannotSettleReplacementPipeline(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("https://first.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	staleGeneration := runtime.snapshot().HTTPSGeneration

	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents("api.example.test\n")); err != nil {
		t.Fatal(err)
	}
	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents("https://second.example.test\n")); err != nil {
		t.Fatal(err)
	}
	applied, _ := runtime.settleHTTPSAssessment(
		staleGeneration,
		testUserCAState(t, time.Now().Add(24*time.Hour), false),
		nil,
	)
	if applied {
		t.Fatal("stale assessment settled the replacement pipeline")
	}
	state := runtime.snapshot()
	if state.HTTPSPipeline == nil || state.HTTPSPipeline.Phase != HTTPSPipelineAssessing || runtime.interceptionActive() {
		t.Fatalf("replacement pipeline = %#v", state.HTTPSPipeline)
	}
}

func TestLiveHTTPSIntentAssessmentSettlesCurrentPipeline(t *testing.T) {
	runtime, err := newRuntime("/tmp/upstreams.txt", nil, fileobservation.Contents("api.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTrafficTestRuntime(runtime)
	assessment := testUserCAState(t, time.Now().Add(24*time.Hour), false)
	ca := &fakeUserCA{state: assessment}
	lifecycle, err := newLifecycle(&lifecycleTestSystemSettings{}, ca, newCoordinator(t.TempDir()), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	active := &activeRuntime{engine: runtime, ctx: ctx, phase: runtimePhaseRunning}
	lifecycle.runtime = active
	go lifecycle.watchRuntimeChanges(ctx, active, runtime.snapshot())

	if err := runtime.applyUpstreamListOutcome(fileobservation.Contents("https://secure.example.test\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for pipelineReadiness(runtime.snapshot().HTTPSPipeline) != HTTPSReadinessReady {
		select {
		case <-deadline.C:
			t.Fatalf("pipeline did not settle ready: %#v", runtime.snapshot().HTTPSPipeline)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestLiveProxyHandlerSwapDoesNotDrainAdmittedRequest(t *testing.T) {
	admitted := make(chan struct{})
	release := make(chan struct{})
	old := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(admitted)
		<-release
		_, _ = io.WriteString(w, "old")
	})
	live := &liveProxyHandler{current: old}
	oldResult := httptest.NewRecorder()
	oldDone := make(chan struct{})
	go func() {
		live.ServeHTTP(oldResult, httptest.NewRequest(http.MethodGet, "http://old.example.test", nil))
		close(oldDone)
	}()
	<-admitted

	live.Set(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "new")
	}))
	newResult := httptest.NewRecorder()
	live.ServeHTTP(newResult, httptest.NewRequest(http.MethodGet, "http://new.example.test", nil))
	close(release)
	<-oldDone

	if oldResult.Body.String() != "old" || newResult.Body.String() != "new" {
		t.Fatalf("old = %q, new = %q", oldResult.Body.String(), newResult.Body.String())
	}
}

func TestRuntimeCloseClosesGatewayOwnedProxyIdleConnections(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
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
	defer closeTrafficTestRuntime(runtime)
	recorder := httptest.NewRecorder()
	runtime.proxyHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, upstream.URL, nil))
	response := recorder.Result()
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	_ = runtime.CloseTraffic()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("runtime close left the outbound proxy connection idle")
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
	t.Setenv("HOME", t.TempDir())
	return createTrafficConfigAtCurrentHome(t, upstreams)
}

func createTrafficConfigAtCurrentHome(t *testing.T, upstreams string) (*fileobservation.Observation, fileobservation.Outcome, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".seamless-cors")
	upstreamPath := filepath.Join(dir, "upstreams.txt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTrafficTestFile(t, upstreamPath, upstreams)
	source := fileobservation.Open(upstreamPath)
	select {
	case initial := <-source.Outcomes():
		return source, initial, upstreamPath
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial Upstream List state")
		return nil, nil, ""
	}
}

func waitForTrafficConfig(t *testing.T, runtime *trafficRuntime, errs <-chan serverError, ready func(runtimeState) bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready(runtime.snapshot()) {
			return
		}
		select {
		case err := <-errs:
			t.Fatal(err.err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for Upstream List update")
		}
	}
}
