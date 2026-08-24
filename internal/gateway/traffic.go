package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

type trafficRuntime struct {
	httpsMu                sync.Mutex
	mu                     sync.RWMutex
	upstreamLists          []runtimeUpstreamListSource
	currentUpstreamList    upstreamlist.Projection
	httpsPipeline          *HTTPSPipelineDetail
	httpsGeneration        uint64
	proxyHandler           *liveProxyHandler
	proxyTransport         *http.Transport
	proxy                  *http.Server
	pacContent             string
	pacHandler             *livePACHandler
	pac                    *http.Server
	listeners              []net.Listener
	publishMu              sync.Mutex
	pacProjectionPublisher conflatedstream.Publisher[string]
	pacProjectionStream    conflatedstream.Stream[string]
	runtimeChangePublisher conflatedstream.Publisher[RuntimeChangeKind]
	runtimeChangeStream    conflatedstream.Stream[RuntimeChangeKind]
	httpsPipelineRevision  uint64
}

type runtimeUpstreamListSource struct {
	kind            UpstreamListSourceKind
	path            string
	optional        bool
	observation     *fileobservation.Observation
	projection      upstreamlist.Projection
	fileSyncIssue   *FileSyncIssue
	projectionIssue *UpstreamListProjectionIssue
}

type runtimeUpstreamListInput struct {
	kind        UpstreamListSourceKind
	path        string
	optional    bool
	observation *fileobservation.Observation
	initial     fileobservation.Outcome
}

// RuntimeChangeKind identifies the current-state concern invalidated by a
// runtime mutation. Notifications are deliberately coalesced; consumers read
// snapshot after receiving a kind instead of treating the notification as an
// event history.
type RuntimeChangeKind uint8

const (
	RuntimeStatusChanged RuntimeChangeKind = iota
	HTTPSPipelineChanged
	HTTPSAssessmentRequested
	HTTPSDeadlineReached
)

type liveProxyHandler struct {
	mu      sync.RWMutex
	current http.Handler
}

func (h *liveProxyHandler) Set(next http.Handler) {
	h.mu.Lock()
	h.current = next
	h.mu.Unlock()
}

func (h *liveProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.mu.RLock()
	current := h.current
	h.mu.RUnlock()
	current.ServeHTTP(w, req)
}

type serverError struct {
	source string
	err    error
}

func newRuntime(upstreamListPath string, observation *fileobservation.Observation, initial fileobservation.Outcome) (*trafficRuntime, error) {
	return newRuntimeWithTransport(upstreamListPath, observation, initial, defaultProxyTransport())
}

func newRuntimeWithTransport(
	upstreamListPath string,
	observation *fileobservation.Observation,
	initial fileobservation.Outcome,
	proxyTransport *http.Transport,
) (*trafficRuntime, error) {
	return newRuntimeFromSources([]runtimeUpstreamListInput{{
		kind: UpstreamListSourceGlobal, path: upstreamListPath, observation: observation, initial: initial,
	}}, proxyTransport)
}

func newRuntimeFromSources(inputs []runtimeUpstreamListInput, proxyTransport *http.Transport) (*trafficRuntime, error) {
	if proxyTransport == nil {
		return nil, fmt.Errorf("proxy transport is required")
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("at least one Upstream List source is required")
	}
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listener unavailable: %w", err)
	}
	pacListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}

	proxyListen := proxyListener.Addr().String()

	sources := make([]runtimeUpstreamListSource, 0, len(inputs))
	projections := make([]upstreamlist.Projection, 0, len(inputs))
	for _, input := range inputs {
		source, sourceErr := initialRuntimeUpstreamListSource(input)
		if sourceErr != nil {
			_ = proxyListener.Close()
			_ = pacListener.Close()
			return nil, sourceErr
		}
		sources = append(sources, source)
		projections = append(projections, source.projection)
	}
	initialList := upstreamlist.Merge(projections...)
	directProxy := corsproxy.New(proxyTransport, nil)
	pacContent := pacrouting.Project(initialList, false, proxyListen)
	pacHandler := newLivePACHandler(pacContent)
	proxyHandler := &liveProxyHandler{current: directProxy}
	var httpsPipeline *HTTPSPipelineDetail
	var httpsGeneration uint64
	if initialList.HTTPSIntent() {
		httpsPipeline = &HTTPSPipelineDetail{Phase: HTTPSPipelineAssessing}
		httpsGeneration = 1
	}
	pacProjectionPublisher, pacProjectionStream := conflatedstream.New[string]()
	runtimeChangePublisher, runtimeChangeStream := conflatedstream.New[RuntimeChangeKind]()
	return &trafficRuntime{
		upstreamLists:          sources,
		currentUpstreamList:    initialList,
		httpsPipeline:          httpsPipeline,
		httpsGeneration:        httpsGeneration,
		proxyHandler:           proxyHandler,
		proxyTransport:         proxyTransport,
		pacContent:             pacContent,
		pacHandler:             pacHandler,
		proxy:                  &http.Server{Handler: proxyHandler},
		pac:                    &http.Server{Handler: pacHandler},
		listeners:              []net.Listener{proxyListener, pacListener},
		pacProjectionPublisher: pacProjectionPublisher,
		pacProjectionStream:    pacProjectionStream,
		runtimeChangePublisher: runtimeChangePublisher,
		runtimeChangeStream:    runtimeChangeStream,
	}, nil
}

func initialRuntimeUpstreamListSource(input runtimeUpstreamListInput) (runtimeUpstreamListSource, error) {
	source := runtimeUpstreamListSource{
		kind: input.kind, path: input.path, optional: input.optional, observation: input.observation,
	}
	outcome := optionalMissingAsEmpty(input.optional, input.initial)
	switch outcome := outcome.(type) {
	case fileobservation.Contents:
		projection, err := upstreamlist.Project(outcome)
		if err != nil {
			source.projectionIssue = &UpstreamListProjectionIssue{Cause: err.Error()}
		} else {
			source.projection = projection
		}
	case fileobservation.ReadError:
		issue, err := classifyFileSyncIssue(outcome)
		if err != nil {
			return runtimeUpstreamListSource{}, err
		}
		source.fileSyncIssue = issue
	case fileobservation.ObservationStoppedError:
		issue, err := classifyFileSyncIssue(outcome)
		if err != nil {
			return runtimeUpstreamListSource{}, err
		}
		source.fileSyncIssue = issue
	default:
		return runtimeUpstreamListSource{}, fmt.Errorf("unsupported initial file observation outcome %T", outcome)
	}
	return source, nil
}

func optionalMissingAsEmpty(optional bool, outcome fileobservation.Outcome) fileobservation.Outcome {
	if !optional {
		return outcome
	}
	if readErr, ok := outcome.(fileobservation.ReadError); ok && errors.Is(readErr.Cause, os.ErrNotExist) {
		return fileobservation.Contents(nil)
	}
	return outcome
}

func defaultProxyTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

// SetInitialHTTPSAssessment settles the pipeline admitted by the initial
// Upstream List. Start skips UserCA inspection entirely when no pipeline exists.
func (r *trafficRuntime) SetInitialHTTPSAssessment(current userca.CurrentState, assessmentErr error) bool {
	r.mu.RLock()
	generation := r.httpsGeneration
	r.mu.RUnlock()
	applied, _ := r.settleHTTPSAssessment(generation, current, assessmentErr)
	return applied
}

// AdoptInstalledUserCA invalidates any in-flight pipeline assessment and
// settles the current pipeline from an explicit install result. Without HTTPS
// Intent it has no runtime consequence.
func (r *trafficRuntime) AdoptInstalledUserCA(current userca.CurrentState) *HTTPSPipelineDetail {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	r.mu.Lock()
	if r.httpsPipeline == nil {
		r.mu.Unlock()
		return nil
	}
	r.httpsGeneration++
	generation := r.httpsGeneration
	r.mu.Unlock()
	r.settleHTTPSAssessmentLocked(generation, current, nil)
	return r.snapshot().HTTPSPipeline
}

// DeactivateHTTPS is the live-uninstall linearization companion. It removes
// HTTPS routes before publishing a direct generation. Without an admitted
// pipeline the UserCA operation has no runtime HTTPS consequence.
func (r *trafficRuntime) DeactivateHTTPS(current userca.CurrentState, assessmentErr error) {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	r.mu.Lock()
	if r.httpsPipeline == nil {
		r.mu.Unlock()
		return
	}
	r.httpsGeneration++
	next := settledHTTPSPipeline(current, assessmentErr)
	pipelineChanged := !sameHTTPSPipelineDetail(r.httpsPipeline, next)
	r.httpsPipeline = next
	if pipelineChanged {
		r.httpsPipelineRevision++
	}
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishPACProjection(projectionChanged)
	r.proxyHandler.Set(corsproxy.New(r.proxyTransport, nil))
	if pipelineChanged {
		r.publishRuntimeChange(HTTPSPipelineChanged)
	}
}

func (r *trafficRuntime) settleHTTPSAssessment(generation uint64, current userca.CurrentState, assessmentErr error) (bool, bool) {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	return r.settleHTTPSAssessmentLocked(generation, current, assessmentErr)
}

func (r *trafficRuntime) settleHTTPSAssessmentLocked(generation uint64, current userca.CurrentState, assessmentErr error) (bool, bool) {
	r.mu.Lock()
	if r.httpsPipeline == nil || r.httpsGeneration != generation {
		r.mu.Unlock()
		return false, false
	}
	ready := assessmentErr == nil && current.Usable && current.SigningMaterial() != nil
	next := settledHTTPSPipeline(current, assessmentErr)
	pipelineChanged := !sameHTTPSPipelineDetail(r.httpsPipeline, next)
	if ready {
		// Recovery publishes MITM behavior before exposing HTTPS PAC routes.
		r.proxyHandler.Set(corsproxy.New(r.proxyTransport, current.SigningMaterial()))
	}
	r.httpsPipeline = next
	if pipelineChanged {
		r.httpsPipelineRevision++
	}
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	if ready {
		r.publishPACProjection(projectionChanged)
	} else {
		// Degradation withdraws served and asynchronously published HTTPS
		// routes before new CONNECT requests switch to direct tunneling.
		r.publishPACProjection(projectionChanged)
		r.proxyHandler.Set(corsproxy.New(r.proxyTransport, nil))
	}
	if pipelineChanged {
		r.publishRuntimeChange(HTTPSPipelineChanged)
	}
	return true, ready
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	errs := make(chan serverError, len(r.upstreamLists)+2)
	for index := range r.upstreamLists {
		go r.watchUpstreamList(ctx, index, errs)
	}
	go func() {
		errs <- serverError{source: "proxy", err: r.proxy.Serve(r.listeners[0])}
	}()
	go func() {
		errs <- serverError{source: "pac", err: r.pac.Serve(r.listeners[1])}
	}()
	for _, address := range []string{r.listeners[0].Addr().String(), r.listeners[1].Addr().String()} {
		if err := proveHTTPServing(ctx, address); err != nil {
			_ = r.Close()
			return fmt.Errorf("runtime readiness failed for %s: %w", address, err)
		}
	}
	if ready != nil {
		close(ready)
	}

	select {
	case <-ctx.Done():
		return r.Close()
	case serverErr := <-errs:
		_ = r.Close()
		if serverErr.err == http.ErrServerClosed {
			return nil
		}
		return serverErr.err
	}
}

func proveHTTPServing(ctx context.Context, address string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancelClose()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", address); err != nil {
		return err
	}
	var first [1]byte
	_, err = conn.Read(first[:])
	return err
}

func (r *trafficRuntime) Close() error {
	return r.CloseTraffic()
}

func (r *trafficRuntime) CloseTraffic() error {
	for index := range r.upstreamLists {
		if r.upstreamLists[index].observation != nil {
			r.upstreamLists[index].observation.Close()
		}
	}
	closeErr := errors.Join(
		r.proxy.Close(),
		r.pac.Close(),
	)
	if r.proxyTransport != nil {
		r.proxyTransport.CloseIdleConnections()
	}
	return closeErr
}

func (r *trafficRuntime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *trafficRuntime) RuntimeChanges() <-chan RuntimeChangeKind {
	return r.runtimeChangeStream.Updates()
}

func (r *trafficRuntime) PACProjections() <-chan string {
	return r.pacProjectionStream.Updates()
}

func (r *trafficRuntime) snapshot() runtimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *trafficRuntime) interceptionActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.httpsReadyLocked()
}

func (r *trafficRuntime) pendingHTTPSAssessment() (uint64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.httpsGeneration, r.httpsPipeline != nil && r.httpsPipeline.Phase == HTTPSPipelineAssessing
}

func (r *trafficRuntime) invalidateHTTPSAssessments() {
	r.httpsMu.Lock()
	r.mu.Lock()
	r.httpsGeneration++
	r.mu.Unlock()
	r.httpsMu.Unlock()
}

// BeginHTTPSDeadlineAssessment withdraws managed HTTPS routing before
// switching new CONNECT requests to direct tunneling. The expected generation
// makes a callback from a replaced timer harmless.
func (r *trafficRuntime) BeginHTTPSDeadlineAssessment(expectedGeneration uint64) (uint64, bool) {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	r.mu.Lock()
	if r.httpsGeneration != expectedGeneration || !r.httpsReadyLocked() {
		r.mu.Unlock()
		return 0, false
	}
	r.httpsGeneration++
	generation := r.httpsGeneration
	r.httpsPipeline = &HTTPSPipelineDetail{Phase: HTTPSPipelineAssessing}
	r.httpsPipelineRevision++
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishPACProjection(projectionChanged)
	r.proxyHandler.Set(corsproxy.New(r.proxyTransport, nil))
	r.publishRuntimeChange(HTTPSPipelineChanged)
	return generation, true
}

func (r *trafficRuntime) watchUpstreamList(ctx context.Context, sourceIndex int, errs chan<- serverError) {
	source := &r.upstreamLists[sourceIndex]
	if source.observation == nil {
		return
	}
	outcomes := source.observation.Outcomes()
	for {
		select {
		case <-ctx.Done():
			return
		case outcome, ok := <-outcomes:
			if !ok {
				return
			}
			if err := r.applyUpstreamListSourceOutcomeContext(ctx, sourceIndex, outcome); err != nil {
				if ctx.Err() != nil {
					return
				}
				errs <- serverError{source: string(source.kind) + "-upstream-list", err: err}
				return
			}
		}
	}
}

func (r *trafficRuntime) applyUpstreamListOutcome(outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(context.Background(), 0, outcome)
}

func (r *trafficRuntime) applyUpstreamListOutcomeContext(_ context.Context, outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(context.Background(), 0, outcome)
}

func (r *trafficRuntime) applyUpstreamListSourceOutcome(sourceIndex int, outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(context.Background(), sourceIndex, outcome)
}

func (r *trafficRuntime) applyUpstreamListSourceOutcomeContext(_ context.Context, sourceIndex int, outcome fileobservation.Outcome) error {
	if sourceIndex < 0 || sourceIndex >= len(r.upstreamLists) {
		return fmt.Errorf("unsupported Upstream List source index %d", sourceIndex)
	}
	source := &r.upstreamLists[sourceIndex]
	outcome = optionalMissingAsEmpty(source.optional, outcome)
	var contents fileobservation.Contents
	var observationError error
	switch outcome := outcome.(type) {
	case fileobservation.Contents:
		contents = outcome
	case fileobservation.ReadError:
		observationError = outcome
	case fileobservation.ObservationStoppedError:
		observationError = outcome
	default:
		return fmt.Errorf("unsupported file observation outcome %T", outcome)
	}

	var nextFileIssue *FileSyncIssue
	var err error
	if observationError != nil {
		nextFileIssue, err = classifyFileSyncIssue(observationError)
		if err != nil {
			return err
		}
	}

	if observationError != nil {
		r.mu.Lock()
		statusChanged := false
		if !sameFileSyncIssue(source.fileSyncIssue, nextFileIssue) {
			source.fileSyncIssue = nextFileIssue
			statusChanged = true
		}
		r.mu.Unlock()
		if statusChanged {
			r.publishRuntimeChange(RuntimeStatusChanged)
		}
		return nil
	}

	candidate, projectErr := upstreamlist.Project(contents)
	var nextProjectionIssue *UpstreamListProjectionIssue
	if projectErr != nil {
		nextProjectionIssue = &UpstreamListProjectionIssue{Cause: projectErr.Error()}
		candidate = upstreamlist.Projection{}
	}

	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	r.mu.Lock()
	projectionChanged := false
	statusChanged := false
	pipelineChanged := false
	assessmentRequested := false
	deactivateProxy := false
	hadHTTPSIntent := r.httpsPipeline != nil
	if source.fileSyncIssue != nil {
		source.fileSyncIssue = nil
		statusChanged = true
	}
	if !sameProjectionIssue(source.projectionIssue, nextProjectionIssue) {
		source.projectionIssue = nextProjectionIssue
		statusChanged = true
	}
	source.projection = candidate
	projections := make([]upstreamlist.Projection, 0, len(r.upstreamLists))
	for index := range r.upstreamLists {
		projections = append(projections, r.upstreamLists[index].projection)
	}
	r.currentUpstreamList = upstreamlist.Merge(projections...)
	statusChanged = true
	hasHTTPSIntent := r.currentUpstreamList.HTTPSIntent()
	switch {
	case !hadHTTPSIntent && hasHTTPSIntent:
		r.httpsGeneration++
		r.httpsPipeline = &HTTPSPipelineDetail{Phase: HTTPSPipelineAssessing}
		r.httpsPipelineRevision++
		pipelineChanged = true
		assessmentRequested = true
	case hadHTTPSIntent && !hasHTTPSIntent:
		r.httpsGeneration++
		r.httpsPipeline = nil
		r.httpsPipelineRevision++
		pipelineChanged = true
		deactivateProxy = true
	}
	projectionChanged = r.updatePACProjectionLocked()
	r.mu.Unlock()
	// On deactivation the served PAC and its async publication lose HTTPS
	// routes before new CONNECT requests switch to direct tunneling.
	r.publishPACProjection(projectionChanged)
	if deactivateProxy {
		r.proxyHandler.Set(corsproxy.New(r.proxyTransport, nil))
	}
	if pipelineChanged {
		r.publishRuntimeChange(HTTPSPipelineChanged)
	}
	if assessmentRequested {
		r.publishRuntimeChange(HTTPSAssessmentRequested)
	}
	if statusChanged {
		r.publishRuntimeChange(RuntimeStatusChanged)
	}
	return nil
}

func classifyFileSyncIssue(err error) (*FileSyncIssue, error) {
	switch err := err.(type) {
	case fileobservation.ReadError:
		return &FileSyncIssue{Kind: FileSyncIssueFileUnreadable, Cause: err.Error()}, nil
	case fileobservation.ObservationStoppedError:
		return &FileSyncIssue{Kind: FileSyncIssueObservationStopped, Cause: err.Error()}, nil
	default:
		return nil, fmt.Errorf("unsupported file observation error %T: %w", err, err)
	}
}

func sameFileSyncIssue(left, right *FileSyncIssue) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameProjectionIssue(left, right *UpstreamListProjectionIssue) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (r *trafficRuntime) publishPACProjection(changed bool) {
	if !changed {
		return
	}
	projection := r.currentPACProjection()
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	r.pacProjectionPublisher.Publish(projection)
}

func (r *trafficRuntime) currentPACProjection() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pacContent
}

func (r *trafficRuntime) updatePACProjectionLocked() bool {
	next := pacrouting.Project(r.currentUpstreamList, r.httpsReadyLocked(), r.listeners[0].Addr().String())
	r.pacContent = next
	r.pacHandler.Set(next)
	return true
}

func (r *trafficRuntime) publishRuntimeChange(kind RuntimeChangeKind) {
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	if kind != HTTPSDeadlineReached && kind != HTTPSAssessmentRequested {
		// Assessment and deadline requests are lifecycle signals, not ordinary
		// status invalidations. Preserve them when another invalidation races;
		// conflation must not erase Gateway's request to reassess UserCA.
		select {
		case pending := <-r.runtimeChangeStream.Updates():
			if pending == HTTPSDeadlineReached || pending == HTTPSAssessmentRequested {
				r.runtimeChangePublisher.Publish(pending)
				return
			}
		default:
		}
	}
	r.runtimeChangePublisher.Publish(kind)
}

type runtimeState struct {
	HTTPSPipelineRevision uint64
	HTTPSGeneration       uint64
	ProxyListen           string
	PACListen             string
	UpstreamLists         []UpstreamListSourceDetail
	HTTPSPipeline         *HTTPSPipelineDetail
	UpstreamCount         int
}

func (r *trafficRuntime) stateLocked() runtimeState {
	upstreamList := r.currentUpstreamList
	sources := make([]UpstreamListSourceDetail, 0, len(r.upstreamLists))
	for _, source := range r.upstreamLists {
		sources = append(sources, UpstreamListSourceDetail{
			Kind:            source.kind,
			Path:            source.path,
			Warnings:        upstreamListWarningDetails(source.kind, source.path, source.projection.Warnings),
			FileSyncIssue:   source.fileSyncIssue,
			ProjectionIssue: source.projectionIssue,
		})
	}
	return runtimeState{
		HTTPSPipelineRevision: r.httpsPipelineRevision,
		HTTPSGeneration:       r.httpsGeneration,
		ProxyListen:           r.listeners[0].Addr().String(),
		PACListen:             r.listeners[1].Addr().String(),
		UpstreamLists:         sources,
		HTTPSPipeline:         r.httpsPipeline,
		UpstreamCount:         len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
	}
}

func (r *trafficRuntime) httpsReadyLocked() bool {
	return r.httpsPipeline != nil &&
		r.httpsPipeline.Phase == HTTPSPipelineSettled &&
		r.httpsPipeline.Readiness == HTTPSReadinessReady
}

func settledHTTPSPipeline(current userca.CurrentState, assessmentErr error) *HTTPSPipelineDetail {
	detail := &HTTPSPipelineDetail{
		Phase:     HTTPSPipelineSettled,
		Readiness: HTTPSReadinessNotReady,
	}
	if assessmentErr != nil {
		detail.UserCAAssessmentIssue = &UserCAAssessmentIssue{
			Cause:  assessmentErr.Error(),
			Action: "Run `seamless-cors install`; if assessment still fails, report the issue.",
		}
		return detail
	}
	if current.Usable && current.SigningMaterial() != nil {
		detail.Readiness = HTTPSReadinessReady
		return detail
	}
	detail.UnmetIntent = &UnmetHTTPSIntentDetail{
		Diagnostic: "HTTPS was requested but the Installed User CA is not usable.",
		Action:     "Run `seamless-cors install`.",
	}
	return detail
}

func sameHTTPSPipelineDetail(left, right *HTTPSPipelineDetail) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Phase == right.Phase &&
		left.Readiness == right.Readiness &&
		sameUnmetHTTPSIntent(left.UnmetIntent, right.UnmetIntent) &&
		sameUserCAAssessmentIssue(left.UserCAAssessmentIssue, right.UserCAAssessmentIssue)
}

func sameUnmetHTTPSIntent(left, right *UnmetHTTPSIntentDetail) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameUserCAAssessmentIssue(left, right *UserCAAssessmentIssue) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
