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
	httpsMu                 sync.Mutex
	mu                      sync.RWMutex
	upstreamListPath        string
	upstreamListObservation *fileobservation.Observation
	currentUpstreamList     upstreamlist.Projection
	fileSyncIssue           *FileSyncIssue
	projectionIssue         *UpstreamListProjectionIssue
	userCA                  userca.Snapshot
	readinessError          error
	userCAOperationWarning  *HTTPSWarningDetail
	proxyCore               *corsproxy.Core
	proxyHandler            *dynamicHTTPHandler
	proxyConfigured         bool
	httpsWarnings           []HTTPSWarningDetail
	proxy                   *http.Server
	pacContent              string
	pacHandler              *livePACHandler
	pac                     *http.Server
	listeners               []net.Listener
	publishMu               sync.Mutex
	pacProjectionPublisher  conflatedstream.Publisher[string]
	pacProjectionStream     conflatedstream.Stream[string]
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
	pacContent := pacrouting.Project(initialList, false, proxyListen)
	pacHandler := newLivePACHandler(pacContent)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	pacProjectionPublisher, pacProjectionStream := conflatedstream.New[string]()
	runtimeChangePublisher, runtimeChangeStream := conflatedstream.New[RuntimeChangeKind]()
	return &trafficRuntime{
		upstreamListPath:        upstreamListPath,
		upstreamListObservation: observation,
		currentUpstreamList:     initialList,
		fileSyncIssue:           fileIssue,
		projectionIssue:         projectionIssue,
		proxyHandler:            proxyHandler,
		pacContent:              pacContent,
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

func (r *trafficRuntime) SetInitialHTTPSReadiness(_ context.Context, assessment userca.Assessment, assessmentErr error) error {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	snapshot := assessment.Snapshot()
	certificate, certificateOK := assessment.Certificate()
	if assessmentErr != nil {
		certificate = nil
		certificateOK = false
	} else if snapshot.Usable() && !certificateOK {
		assessmentErr = fmt.Errorf("usable UserCA assessment omitted signing material")
	}
	proxyHandler := corsproxy.New(corsproxy.Options{
		Certificate: certificate,
	})
	r.proxyHandler.Set(proxyHandler)
	r.mu.Lock()
	r.userCA = snapshot
	r.readinessError = assessmentErr
	r.proxyCore = proxyHandler
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

// RecoverHTTPS atomically adopts fresh UserCA signing material into a live
// runtime before publishing the complete Managed PAC desired state.
func (r *trafficRuntime) RecoverHTTPS(_ context.Context, assessment userca.Assessment) error {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	snapshot := assessment.Snapshot()
	certificate, ok := assessment.Certificate()
	if !ok {
		return fmt.Errorf("HTTPS Readiness Recovery requires a usable UserCA assessment")
	}
	r.mu.Lock()
	core := r.proxyCore
	r.mu.Unlock()
	if core == nil {
		return fmt.Errorf("HTTPS proxy is not configured")
	}
	core.ReplaceCertificate(certificate)
	r.mu.Lock()
	r.userCA = snapshot
	r.readinessError = nil
	r.proxyConfigured = true
	warningsChanged := r.updateHTTPSWarningsLocked()
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishPACProjection(projectionChanged)
	return nil
}

// DeactivateHTTPS is the live-uninstall linearization companion: new CONNECT
// requests tunnel directly and HTTPS PAC routes are withdrawn immediately.
func (r *trafficRuntime) DeactivateHTTPS(snapshot userca.Snapshot, assessmentErr error) {
	r.httpsMu.Lock()
	defer r.httpsMu.Unlock()
	r.mu.Lock()
	desiredChanged := r.httpsReadyLocked()
	if r.proxyCore != nil {
		r.proxyCore.DeactivateHTTPS()
	}
	r.userCA = snapshot
	r.readinessError = assessmentErr
	warningsChanged := r.updateHTTPSWarningsLocked()
	projectionChanged := r.updatePACProjectionLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	if desiredChanged {
		r.publishPACProjection(projectionChanged)
	}
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
	if r.upstreamListObservation != nil {
		r.upstreamListObservation.Close()
	}
	return errors.Join(
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

func (r *trafficRuntime) applyUpstreamListOutcomeContext(_ context.Context, outcome fileobservation.Outcome) error {
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
	r.currentUpstreamList = candidate
	statusChanged = true
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
	warnings = append(warnings, httpsReadinessWarnings(
		upstreamList.HTTPSIntent(),
		r.userCA,
		r.readinessError,
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
	HTTPSIntent                 bool
	HTTPSWarnings               []HTTPSWarningDetail
	UpstreamCount               int
	UpstreamListWarnings        []UpstreamListWarningDetail
	UpstreamListFileSyncIssue   *FileSyncIssue
	UpstreamListProjectionIssue *UpstreamListProjectionIssue
}

func (r *trafficRuntime) stateLocked() runtimeState {
	upstreamList := r.currentUpstreamList
	readiness := HTTPSReadinessNotReady
	if r.httpsReadyLocked() {
		readiness = HTTPSReadinessReady
	}
	warnings := append([]HTTPSWarningDetail(nil), r.httpsWarnings...)
	if r.userCA.Usable() && !r.now().Before(r.userCA.ExpiresAt()) {
		readiness = HTTPSReadinessNotReady
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
		HTTPSIntent:                 upstreamList.HTTPSIntent(),
		HTTPSWarnings:               warnings,
		UpstreamCount:               len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
		UpstreamListWarnings:        upstreamListWarningDetails(upstreamList.Warnings),
		UpstreamListFileSyncIssue:   r.fileSyncIssue,
		UpstreamListProjectionIssue: r.projectionIssue,
	}
}

func (r *trafficRuntime) httpsReadyLocked() bool {
	return r.readinessError == nil && r.userCA.Usable()
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

func hasHTTPSWarning(warnings []HTTPSWarningDetail, kind HTTPSWarningKind) bool {
	for _, warning := range warnings {
		if warning.Kind == kind {
			return true
		}
	}
	return false
}
