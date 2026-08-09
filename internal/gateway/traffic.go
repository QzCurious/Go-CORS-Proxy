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
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
	"github.com/QzCurious/seamless-cors/internal/userca"
)

type trafficRuntime struct {
	mu                     sync.RWMutex
	upstreamListPath       string
	upstreamListSource     *upstreamlist.Source
	currentUpstreamList    upstreamlist.UpstreamList
	upstreamListDiagnostic *upstreamlist.Diagnostic
	userCA                 userca.Snapshot
	readinessError         error
	interceptionState      HTTPSInterceptionState
	interceptionError      error
	userCAOperationWarning *HTTPSWarningDetail
	proxyCore              *corsproxy.Core
	proxyHandler           *dynamicHTTPHandler
	proxyConfigured        bool
	httpsWarnings          []HTTPSWarningDetail
	proxy                  *http.Server
	pacRouting             *pacrouting.Routing
	pac                    *http.Server
	listeners              []net.Listener
	publishMu              sync.Mutex
	desiredStatePublisher  conflatedstream.Publisher[managedpac.DesiredState]
	desiredStateStream     conflatedstream.Stream[managedpac.DesiredState]
	runtimeChangePublisher conflatedstream.Publisher[RuntimeChangeKind]
	runtimeChangeStream    conflatedstream.Stream[RuntimeChangeKind]
	httpsWarningsRevision  uint64
	now                    func() time.Time
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

func newRuntime(upstreamListPath string, source *upstreamlist.Source, initial upstreamlist.Transition) (*trafficRuntime, error) {
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

	pacRouting := pacrouting.NewRouting(proxyListen)
	var initialList upstreamlist.UpstreamList
	var initialDiagnostic *upstreamlist.Diagnostic
	switch transition := initial.(type) {
	case upstreamlist.ListAccepted:
		initialList = transition.List
	case upstreamlist.DiagnosticReported:
		initialDiagnostic = &transition.Diagnostic
	}
	pacRouting.Apply(initialList.HostSelectors, initialList.OriginSelectors, false)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	desiredStatePublisher, desiredStateStream := conflatedstream.New[managedpac.DesiredState]()
	runtimeChangePublisher, runtimeChangeStream := conflatedstream.New[RuntimeChangeKind]()
	return &trafficRuntime{
		upstreamListPath:       upstreamListPath,
		upstreamListSource:     source,
		currentUpstreamList:    initialList,
		upstreamListDiagnostic: initialDiagnostic,
		proxyHandler:           proxyHandler,
		pacRouting:             pacRouting,
		proxy:                  &http.Server{Handler: proxyHandler},
		pac:                    &http.Server{Handler: pacRouting.Handler()},
		listeners:              []net.Listener{proxyListener, pacListener},
		desiredStatePublisher:  desiredStatePublisher,
		desiredStateStream:     desiredStateStream,
		runtimeChangePublisher: runtimeChangePublisher,
		runtimeChangeStream:    runtimeChangeStream,
		now:                    time.Now,
	}, nil
}

func (r *trafficRuntime) SetInitialHTTPSReadiness(assessment userca.Assessment, assessmentErr error) error {
	snapshot := assessment.Snapshot()
	provider, providerOK := assessment.Provider()
	if assessmentErr != nil {
		provider = nil
		providerOK = false
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
	r.readinessError = assessmentErr
	r.proxyCore = proxyHandler
	r.interceptionState = HTTPSInterceptionInactive
	if providerOK && assessmentErr == nil {
		r.interceptionState = HTTPSInterceptionActive
	}
	r.interceptionError = nil
	r.proxyConfigured = true
	nextWarnings := r.currentHTTPSWarningsLocked()
	if !slices.Equal(nextWarnings, r.httpsWarnings) {
		r.httpsWarnings = nextWarnings
		r.httpsWarningsRevision++
	}
	r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, r.interceptionState == HTTPSInterceptionActive)
	r.mu.Unlock()
	r.publishDesiredState()
	return nil
}

// RecoverHTTPS atomically adopts a fresh UserCA provider into a live runtime
// and publishes the complete Managed PAC desired state when interception is
// active.
func (r *trafficRuntime) RecoverHTTPS(assessment userca.Assessment) error {
	snapshot := assessment.Snapshot()
	provider, ok := assessment.Provider()
	if !ok {
		return fmt.Errorf("HTTPS Readiness Recovery requires a usable UserCA assessment")
	}
	r.mu.Lock()
	core := r.proxyCore
	wasActive := r.interceptionState == HTTPSInterceptionActive
	r.mu.Unlock()
	if core == nil {
		return fmt.Errorf("HTTPS proxy is not configured")
	}
	core.ReplaceProvider(provider)
	r.mu.Lock()
	r.userCA = snapshot
	r.readinessError = nil
	r.interceptionState = HTTPSInterceptionActive
	r.interceptionError = nil
	r.proxyConfigured = true
	warningsChanged := r.updateHTTPSWarningsLocked()
	if wasActive {
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warningsChanged)
		return nil
	}
	r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, true)
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishDesiredState()
	return nil
}

// DeactivateHTTPS is the live-uninstall linearization companion: new CONNECT
// requests tunnel directly and HTTPS PAC routes are withdrawn immediately.
func (r *trafficRuntime) DeactivateHTTPS(snapshot userca.Snapshot, assessmentErr error) {
	r.mu.Lock()
	desiredChanged := r.interceptionState == HTTPSInterceptionActive
	if r.proxyCore != nil {
		r.proxyCore.DeactivateHTTPS()
	}
	r.userCA = snapshot
	r.readinessError = assessmentErr
	r.interceptionState = HTTPSInterceptionInactive
	r.interceptionError = nil
	warningsChanged := r.updateHTTPSWarningsLocked()
	r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, false)
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	if desiredChanged {
		r.publishDesiredState()
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
	r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, r.interceptionState == HTTPSInterceptionActive)
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	r.publishDesiredState()
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	if !r.proxyConfigured {
		if err := r.SetInitialHTTPSReadiness(userca.Assessment{}, nil); err != nil {
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
		r.upstreamListSource.Close(),
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

func (r *trafficRuntime) DesiredStates() <-chan managedpac.DesiredState {
	return r.desiredStateStream.Updates()
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
	updates := r.upstreamListSource.Transitions()
	for {
		select {
		case <-ctx.Done():
			return
		case transition, ok := <-updates:
			if !ok {
				return
			}
			r.applyUpstreamListTransition(transition)
		}
	}
}

func (r *trafficRuntime) applyUpstreamListTransition(transition upstreamlist.Transition) {
	r.mu.Lock()
	routeChanged := false
	switch transition := transition.(type) {
	case upstreamlist.ListAccepted:
		r.currentUpstreamList = transition.List
		r.upstreamListDiagnostic = nil
		routeChanged = r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, r.interceptionState == HTTPSInterceptionActive)
	case upstreamlist.DiagnosticReported:
		r.upstreamListDiagnostic = &transition.Diagnostic
	}
	warningsChanged := r.updateHTTPSWarningsLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warningsChanged)
	if routeChanged {
		r.publishDesiredState()
	}
	// Source emits only changed lists or diagnostics, so every received state
	// invalidates the complete runtime status snapshot.
	r.publishRuntimeChange(RuntimeStatusChanged)
}

func (r *trafficRuntime) publishDesiredState() {
	desired := r.currentDesiredState()
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	r.desiredStatePublisher.Publish(desired)
}

func (r *trafficRuntime) currentDesiredState() managedpac.DesiredState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return managedpac.NewDesiredState(
		r.currentUpstreamList,
		r.interceptionState == HTTPSInterceptionActive,
		r.listeners[0].Addr().String(),
		r.listeners[1].Addr().String(),
	)
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
	HTTPSWarningsRevision  uint64
	ProxyListen            string
	PACListen              string
	UpstreamList           string
	HTTPSReadiness         HTTPSReadinessStatus
	HTTPSInterception      HTTPSInterceptionState
	HTTPSIntent            bool
	HTTPSWarnings          []HTTPSWarningDetail
	UpstreamCount          int
	UpstreamListWarnings   []UpstreamListWarningDetail
	UpstreamListDiagnostic *UpstreamListDiagnosticDetail
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
		HTTPSWarningsRevision:  r.httpsWarningsRevision,
		ProxyListen:            r.listeners[0].Addr().String(),
		PACListen:              r.listeners[1].Addr().String(),
		UpstreamList:           r.upstreamListPath,
		HTTPSReadiness:         readiness,
		HTTPSInterception:      interception,
		HTTPSIntent:            upstreamList.HTTPSIntent(),
		HTTPSWarnings:          warnings,
		UpstreamCount:          len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
		UpstreamListWarnings:   upstreamListWarningDetails(upstreamList.Warnings),
		UpstreamListDiagnostic: upstreamListDiagnosticDetail(r.upstreamListDiagnostic),
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
			Action:     "Run `seamless-cors install`.",
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
