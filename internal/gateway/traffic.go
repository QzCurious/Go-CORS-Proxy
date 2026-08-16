package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

type trafficRuntime struct {
	providerMu              sync.Mutex
	mu                      sync.RWMutex
	upstreamListPath        string
	upstreamListObservation *fileobservation.Observation
	currentUpstreamList     upstreamlist.Projection
	fileSyncIssue           *FileSyncIssue
	projectionIssue         *UpstreamListProjectionIssue
	userCA                  userca.Snapshot
	providerSource          userca.ProviderSource
	readinessError          error
	interceptionState       HTTPSInterceptionState
	interceptionError       error
	userCAOperationWarning  *HTTPSWarningDetail
	proxyCore               *corsproxy.Core
	proxyHandler            *dynamicHTTPHandler
	proxyConfigured         bool
	httpsWarnings           []HTTPSWarningDetail
	proxy                   *http.Server
	pacProjection           pacrouting.Projection
	pacHandler              *pacrouting.LiveHandler
	pac                     *http.Server
	listeners               []net.Listener
	publishMu               sync.Mutex
	pacProjectionPublisher  conflatedstream.Publisher[pacrouting.Projection]
	pacProjectionStream     conflatedstream.Stream[pacrouting.Projection]
	runtimeChangePublisher  conflatedstream.Publisher[RuntimeChangeKind]
	runtimeChangeStream     conflatedstream.Stream[RuntimeChangeKind]
	httpsWarningsRevision   uint64
	now                     func() time.Time
}

// RuntimeChangeKind identifies the current-state concern invalidated by a
// runtime mutation. Notifications are deliberately coalesced; consumers read
// snapshot after receiving a kind instead of treating the notification as an
// event history.
type RuntimeChangeKind uint8

const (
	RuntimeStatusChanged RuntimeChangeKind = iota
	HTTPSWarningsChanged
	HTTPSDeadlineReached
)

type dynamicHTTPHandler struct {
	mu      sync.RWMutex
	current http.Handler
}

func (h *dynamicHTTPHandler) Set(next http.Handler) {
	h.mu.Lock()
	h.current = next
	h.mu.Unlock()
}

func (h *dynamicHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

	initialList := upstreamlist.Projection{}
	var fileIssue *FileSyncIssue
	var projectionIssue *UpstreamListProjectionIssue
	var initialContents fileobservation.Contents
	var initialObservationError error
	switch initial := initial.(type) {
	case fileobservation.Contents:
		initialContents = initial
	case fileobservation.ReadError:
		initialObservationError = initial
	case fileobservation.ObservationStoppedError:
		initialObservationError = initial
	default:
		_ = proxyListener.Close()
		_ = pacListener.Close()
		return nil, fmt.Errorf("unsupported initial file observation outcome %T", initial)
	}
	if initialObservationError != nil {
		fileIssue, err = classifyFileSyncIssue(initialObservationError)
		if err != nil {
			_ = proxyListener.Close()
			_ = pacListener.Close()
			return nil, err
		}
	} else {
		initialList, err = upstreamlist.Project(initialContents)
		if err != nil {
			projectionIssue = &UpstreamListProjectionIssue{Cause: err.Error()}
		}
	}
	pacProjection := pacrouting.Project(initialList, false, proxyListen, pacListener.Addr().String())
	pacHandler := pacrouting.NewLiveHandler(pacProjection)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	pacProjectionPublisher, pacProjectionStream := conflatedstream.New[pacrouting.Projection]()
	runtimeChangePublisher, runtimeChangeStream := conflatedstream.New[RuntimeChangeKind]()
	return &trafficRuntime{
		upstreamListPath:        upstreamListPath,
		upstreamListObservation: observation,
		currentUpstreamList:     initialList,
		fileSyncIssue:           fileIssue,
		projectionIssue:         projectionIssue,
		proxyHandler:            proxyHandler,
		pacProjection:           pacProjection,
		pacHandler:              pacHandler,
		proxy:                   &http.Server{Handler: proxyHandler},
		pac:                     &http.Server{Handler: pacHandler},
		listeners:               []net.Listener{proxyListener, pacListener},
		pacProjectionPublisher:  pacProjectionPublisher,
		pacProjectionStream:     pacProjectionStream,
		runtimeChangePublisher:  runtimeChangePublisher,
		runtimeChangeStream:     runtimeChangeStream,
		now:                     time.Now,
	}, nil
}

func (r *trafficRuntime) SetInitialHTTPSReadiness(ctx context.Context, assessment userca.Assessment, assessmentErr error) error {
	r.providerMu.Lock()
	defer r.providerMu.Unlock()
	snapshot := assessment.Snapshot()
	source, sourceOK := assessment.Source()
	if assessmentErr != nil {
		source = nil
		sourceOK = false
	}
	var provider userca.CertificateProvider
	var projectionErr error
	if sourceOK {
		provider, projectionErr = source.Project(ctx, r.currentUpstreamList)
		if projectionErr != nil {
			projectionErr = fmt.Errorf("certificate projection failed: %w", projectionErr)
		}
	}
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		Provider:       provider,
		OnHTTPSFailure: r.handleHTTPSFailure,
	})
	if err != nil {
		return err
	}
	r.proxyHandler.Set(proxyHandler)
	r.mu.Lock()
	r.userCA = snapshot
	r.providerSource = source
	r.readinessError = assessmentErr
	r.proxyCore = proxyHandler
	r.interceptionState = HTTPSInterceptionInactive
	if provider != nil && projectionErr == nil && assessmentErr == nil {
		r.interceptionState = HTTPSInterceptionActive
	}
	r.interceptionError = projectionErr
	if projectionErr != nil {
		r.interceptionState = HTTPSInterceptionFailed
	}
	r.proxyConfigured = true
	nextWarnings := r.currentHTTPSWarningsLocked()
	if !slices.Equal(nextWarnings, r.httpsWarnings) {
		r.httpsWarnings = nextWarnings
		r.httpsWarningsRevision++
	}
	r.updatePACProjectionLocked()
	r.mu.Unlock()
	return nil
}

// RecoverHTTPS atomically adopts a fresh UserCA provider into a live runtime
// and publishes the complete Managed PAC desired state when interception is
// active.
func (r *trafficRuntime) RecoverHTTPS(ctx context.Context, assessment userca.Assessment) error {
	r.providerMu.Lock()
	defer r.providerMu.Unlock()
	snapshot := assessment.Snapshot()
	source, ok := assessment.Source()
	if !ok {
		return fmt.Errorf("HTTPS Readiness Recovery requires a usable UserCA assessment")
	}
	r.mu.Lock()
	core := r.proxyCore
	upstreams := r.currentUpstreamList
	r.mu.Unlock()
	if core == nil {
		return fmt.Errorf("HTTPS proxy is not configured")
	}
	provider, projectionErr := source.Project(ctx, upstreams)
	if projectionErr != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if projectionErr != nil {
		projectionErr = fmt.Errorf("certificate projection failed: %w", projectionErr)
	}
	if projectionErr != nil {
		core.DeactivateHTTPS()
	} else {
		core.ReplaceProvider(provider)
	}
	r.mu.Lock()
	r.userCA = snapshot
	r.providerSource = source
	r.readinessError = nil
	r.interceptionState = HTTPSInterceptionActive
	r.interceptionError = projectionErr
	if projectionErr != nil {
		r.interceptionState = HTTPSInterceptionFailed
	}
	r.proxyConfigured = true
	warningsChanged := r.updateHTTPSWarningsLocked()
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishPACProjection(projectionChanged)
	return projectionErr
}

// DeactivateHTTPS is the live-uninstall linearization companion: new CONNECT
// requests tunnel directly and HTTPS PAC routes are withdrawn immediately.
func (r *trafficRuntime) DeactivateHTTPS(snapshot userca.Snapshot, assessmentErr error) {
	r.providerMu.Lock()
	defer r.providerMu.Unlock()
	r.mu.Lock()
	desiredChanged := r.interceptionState == HTTPSInterceptionActive
	if r.proxyCore != nil {
		r.proxyCore.DeactivateHTTPS()
	}
	r.userCA = snapshot
	r.providerSource = nil
	r.readinessError = assessmentErr
	r.interceptionState = HTTPSInterceptionInactive
	r.interceptionError = nil
	warningsChanged := r.updateHTTPSWarningsLocked()
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	if desiredChanged {
		r.publishPACProjection(projectionChanged)
	}
}

func (r *trafficRuntime) handleHTTPSFailure(failure corsproxy.HTTPSFailure) {
	if failure.Disposition == corsproxy.HTTPSFailureExpired {
		// Expiry is a signal only. Gateway lifecycle re-assesses UserCA and
		// decides whether the current capability is truly unusable.
		r.publishRuntimeChange(HTTPSDeadlineReached)
		return
	}
	r.mu.Lock()
	if r.interceptionState != HTTPSInterceptionActive {
		r.mu.Unlock()
		return
	}
	r.interceptionState = HTTPSInterceptionFailed
	r.interceptionError = failure.Err
	warningsChanged := r.updateHTTPSWarningsLocked()
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishPACProjection(projectionChanged)
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	if !r.proxyConfigured {
		if err := r.SetInitialHTTPSReadiness(context.Background(), userca.Assessment{}, nil); err != nil {
			return err
		}
	}
	errs := make(chan serverError, 3)
	go r.watchUpstreamList(ctx, errs)
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
	return errors.Join(
		closeObservation(r.upstreamListObservation),
		r.proxy.Close(),
		r.pac.Close(),
	)
}

func (r *trafficRuntime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *trafficRuntime) RuntimeChanges() <-chan RuntimeChangeKind {
	return r.runtimeChangeStream.Updates()
}

func (r *trafficRuntime) PACProjections() <-chan pacrouting.Projection {
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
	return r.interceptionState == HTTPSInterceptionActive
}

func (r *trafficRuntime) watchUpstreamList(ctx context.Context, errs chan<- serverError) {
	if r.upstreamListObservation == nil {
		return
	}
	outcomes := r.upstreamListObservation.Outcomes()
	for {
		select {
		case <-ctx.Done():
			return
		case outcome, ok := <-outcomes:
			if !ok {
				return
			}
			if err := r.applyUpstreamListOutcomeContext(ctx, outcome); err != nil {
				if ctx.Err() != nil {
					return
				}
				errs <- serverError{source: "upstream-list", err: err}
				return
			}
		}
	}
}

func (r *trafficRuntime) applyUpstreamListOutcome(outcome fileobservation.Outcome) error {
	return r.applyUpstreamListOutcomeContext(context.Background(), outcome)
}

func (r *trafficRuntime) applyUpstreamListOutcomeContext(ctx context.Context, outcome fileobservation.Outcome) error {
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
		if !sameFileSyncIssue(r.fileSyncIssue, nextFileIssue) {
			r.fileSyncIssue = nextFileIssue
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

	r.providerMu.Lock()
	defer r.providerMu.Unlock()
	r.mu.Lock()
	changed := !upstreamlist.Equal(r.currentUpstreamList, candidate)
	source := r.providerSource
	retryFailed := r.interceptionState == HTTPSInterceptionFailed
	r.mu.Unlock()

	var provider userca.CertificateProvider
	var providerErr error
	if source != nil && (changed || retryFailed) {
		provider, providerErr = source.Project(ctx, candidate)
		if providerErr != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}

	r.mu.Lock()
	projectionChanged := false
	statusChanged := false
	if r.fileSyncIssue != nil {
		r.fileSyncIssue = nil
		statusChanged = true
	}
	if !sameProjectionIssue(r.projectionIssue, nextProjectionIssue) {
		r.projectionIssue = nextProjectionIssue
		statusChanged = true
	}
	if changed {
		r.currentUpstreamList = candidate
		statusChanged = true
	}
	if source != nil && (changed || retryFailed) {
		if providerErr != nil {
			if r.proxyCore != nil {
				r.proxyCore.DeactivateHTTPS()
			}
			r.interceptionState = HTTPSInterceptionFailed
			r.interceptionError = fmt.Errorf("certificate projection failed: %w", providerErr)
		} else {
			if r.proxyCore != nil {
				r.proxyCore.ReplaceProvider(provider)
			}
			r.interceptionState = HTTPSInterceptionActive
			r.interceptionError = nil
		}
	}
	projectionChanged = r.updatePACProjectionLocked()
	warningsChanged := r.updateHTTPSWarningsLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishPACProjection(projectionChanged)
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

func (r *trafficRuntime) currentPACProjection() pacrouting.Projection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pacProjection
}

func (r *trafficRuntime) updatePACProjectionLocked() bool {
	next := pacrouting.Project(r.currentUpstreamList, r.interceptionState == HTTPSInterceptionActive, r.listeners[0].Addr().String(), r.listeners[1].Addr().String())
	if pacrouting.Equal(r.pacProjection, next) {
		return false
	}
	r.pacProjection = next
	r.pacHandler.Set(next)
	return true
}

func closeObservation(observation *fileobservation.Observation) error {
	if observation == nil {
		return nil
	}
	return observation.Close()
}

func (r *trafficRuntime) publishRuntimeChange(kind RuntimeChangeKind) {
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	if kind != HTTPSDeadlineReached {
		// Deadline is a lifecycle signal, not an ordinary status invalidation.
		// Preserve it when a status or warning change races with the provider
		// expiry callback; conflation must not erase the request
		// for Gateway to reassess UserCA.
		select {
		case pending := <-r.runtimeChangeStream.Updates():
			if pending == HTTPSDeadlineReached {
				r.runtimeChangePublisher.Publish(pending)
				return
			}
		default:
		}
	}
	r.runtimeChangePublisher.Publish(kind)
}

func (r *trafficRuntime) SetUninstallWarning(err error) {
	r.mu.Lock()
	r.userCAOperationWarning = &HTTPSWarningDetail{
		Kind:       HTTPSWarningUninstallIncomplete,
		Diagnostic: fmt.Sprintf("Installed User CA uninstall is incomplete: %v.", err),
		Action:     "Run `seamless-cors uninstall` again.",
	}
	changed := r.updateHTTPSWarningsLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(changed)
}

func (r *trafficRuntime) updateHTTPSWarningsLocked() bool {
	next := r.currentHTTPSWarningsLocked()
	if slices.Equal(next, r.httpsWarnings) {
		return false
	}
	r.httpsWarnings = next
	r.httpsWarningsRevision++
	return true
}

func (r *trafficRuntime) publishHTTPSWarningUpdate(changed bool) {
	if !changed {
		return
	}
	// Warning details remain in the immutable runtime snapshot. The
	// notification only invalidates the current warning revision.
	r.publishRuntimeChange(HTTPSWarningsChanged)
}

func (r *trafficRuntime) currentHTTPSWarningsLocked() []HTTPSWarningDetail {
	var warnings []HTTPSWarningDetail
	upstreamList := r.currentUpstreamList
	warnings = append(warnings, httpsRuntimeWarnings(
		upstreamList.HTTPSIntent(),
		r.userCA,
		r.readinessError,
		r.interceptionState,
		r.interceptionError,
	)...)
	if r.userCAOperationWarning != nil && !hasHTTPSWarning(warnings, r.userCAOperationWarning.Kind) {
		warnings = append(warnings, *r.userCAOperationWarning)
	}
	return warnings
}

type runtimeState struct {
	HTTPSWarningsRevision       uint64
	ProxyListen                 string
	PACListen                   string
	UpstreamList                string
	HTTPSReadiness              HTTPSReadinessStatus
	HTTPSInterception           HTTPSInterceptionState
	HTTPSIntent                 bool
	HTTPSWarnings               []HTTPSWarningDetail
	UpstreamCount               int
	UpstreamListWarnings        []UpstreamListWarningDetail
	UpstreamListFileSyncIssue   *FileSyncIssue
	UpstreamListProjectionIssue *UpstreamListProjectionIssue
}

func (r *trafficRuntime) stateLocked() runtimeState {
	upstreamList := r.currentUpstreamList
	readiness := httpsReadinessStatus(r.userCA)
	interception := r.interceptionState
	warnings := append([]HTTPSWarningDetail(nil), r.httpsWarnings...)
	if r.userCA.Usable() && !r.now().Before(r.userCA.ExpiresAt()) {
		readiness = HTTPSReadinessNotReady
		interception = HTTPSInterceptionInactive
		warnings = []HTTPSWarningDetail{{
			Kind:       HTTPSWarningReadinessUnavailable,
			Diagnostic: "Installed User CA has expired.",
			Action:     "Run `seamless-cors install`.",
		}}
	}
	return runtimeState{
		HTTPSWarningsRevision:       r.httpsWarningsRevision,
		ProxyListen:                 r.listeners[0].Addr().String(),
		PACListen:                   r.listeners[1].Addr().String(),
		UpstreamList:                r.upstreamListPath,
		HTTPSReadiness:              readiness,
		HTTPSInterception:           interception,
		HTTPSIntent:                 upstreamList.HTTPSIntent(),
		HTTPSWarnings:               warnings,
		UpstreamCount:               len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
		UpstreamListWarnings:        upstreamListWarningDetails(upstreamList.Warnings),
		UpstreamListFileSyncIssue:   r.fileSyncIssue,
		UpstreamListProjectionIssue: r.projectionIssue,
	}
}

func httpsReadinessStatus(snapshot userca.Snapshot) HTTPSReadinessStatus {
	if snapshot.Usable() {
		return HTTPSReadinessReady
	}
	return HTTPSReadinessNotReady
}

func httpsReadinessWarnings(httpsIntent bool, snapshot userca.Snapshot, assessmentErr error) []HTTPSWarningDetail {
	if assessmentErr != nil {
		return []HTTPSWarningDetail{{
			Kind:       HTTPSWarningReadinessUnavailable,
			Diagnostic: fmt.Sprintf("HTTPS Readiness could not be assessed: %v.", assessmentErr),
			Action:     "Run `seamless-cors install`.",
		}}
	}
	if snapshot.Usable() {
		var warnings []HTTPSWarningDetail
		if snapshot.RenewalDue() {
			warnings = append(warnings, HTTPSWarningDetail{
				Kind:       HTTPSWarningRenewalRecommended,
				Diagnostic: fmt.Sprintf("Installed User CA expires soon (%s).", snapshot.ExpiresAt().Format("2006-01-02")),
				Action:     "Run `seamless-cors install` to renew it.",
			})
		}
		return warnings
	}
	if !httpsIntent {
		return nil
	}
	return []HTTPSWarningDetail{{
		Kind:       HTTPSWarningUnmetIntent,
		Diagnostic: "HTTPS was requested but the Installed User CA is not usable.",
		Action:     "Run `seamless-cors install`.",
	}}
}

func httpsRuntimeWarnings(
	httpsIntent bool,
	snapshot userca.Snapshot,
	assessmentErr error,
	state HTTPSInterceptionState,
	interceptionErr error,
) []HTTPSWarningDetail {
	if state == HTTPSInterceptionFailed {
		return []HTTPSWarningDetail{{
			Kind:       HTTPSWarningInterceptionFailed,
			Diagnostic: fmt.Sprintf("HTTPS interception failed: %v.", interceptionErr),
			Action:     "Save the Upstream List to retry, or run `seamless-cors install`.",
		}}
	}
	return httpsReadinessWarnings(httpsIntent, snapshot, assessmentErr)
}

func hasHTTPSWarning(warnings []HTTPSWarningDetail, kind HTTPSWarningKind) bool {
	for _, warning := range warnings {
		if warning.Kind == kind {
			return true
		}
	}
	return false
}
