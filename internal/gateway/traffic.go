package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/latestvalue"
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
	desiredStates          chan managedpac.DesiredState
	publishMu              sync.Mutex
	runtimeChanges         chan RuntimeChangeKind
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

func newRuntime(upstreamListPath string, source *upstreamlist.Source, initial upstreamlist.UpstreamList) (*trafficRuntime, error) {
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
	pacRouting.Apply(initial.HostSelectors, initial.OriginSelectors, false)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	return &trafficRuntime{
		upstreamListPath:    upstreamListPath,
		upstreamListSource:  source,
		currentUpstreamList: initial.Clone(),
		proxyHandler:        proxyHandler,
		pacRouting:          pacRouting,
		proxy:               &http.Server{Handler: proxyHandler},
		pac:                 &http.Server{Handler: pacRouting.Handler()},
		listeners:           []net.Listener{proxyListener, pacListener},
		desiredStates:       make(chan managedpac.DesiredState, 1),
		runtimeChanges:      make(chan RuntimeChangeKind, 1),
		now:                 time.Now,
	}, nil
}

func (r *trafficRuntime) SetInitialHTTPSReadiness(snapshot userca.Snapshot, assessmentErr error) error {
	generation, generationOK := proxyGeneration(snapshot)
	proxyHandler, err := corsproxy.New(corsproxy.Options{
		InterceptHTTPS:  generationOK,
		HTTPSGeneration: generation,
		OnHTTPSFailure:  r.handleHTTPSFailure,
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
	if generationOK {
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

// RecoverHTTPS applies a successful UserCA install to a live runtime and
// publishes the complete Managed PAC desired state when interception becomes
// active.
func (r *trafficRuntime) RecoverHTTPS(snapshot userca.Snapshot) error {
	generation, ok := proxyGeneration(snapshot)
	if !ok {
		return fmt.Errorf("HTTPS Readiness Recovery requires an Installed User CA")
	}
	r.mu.Lock()
	core := r.proxyCore
	wasActive := r.interceptionState == HTTPSInterceptionActive
	current := r.userCA
	r.mu.Unlock()
	if core == nil {
		return fmt.Errorf("HTTPS proxy is not configured")
	}
	if wasActive && sameUserCA(current, snapshot) {
		r.mu.Lock()
		r.userCA = snapshot
		r.readinessError = nil
		r.interceptionError = nil
		warningsChanged := r.updateHTTPSWarningsLocked()
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warningsChanged)
		return nil
	}
	if err := core.ActivateHTTPS(*generation); err != nil {
		return err
	}
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

func sameUserCA(left, right userca.Snapshot) bool {
	if left.Usable() != right.Usable() || !left.Usable() {
		return left.Usable() == right.Usable()
	}
	leftCert, _ := left.TLSCertificate()
	rightCert, _ := right.TLSCertificate()
	if len(leftCert.Certificate) == 0 || len(rightCert.Certificate) == 0 {
		return false
	}
	return slices.Equal(leftCert.Certificate[0], rightCert.Certificate[0])
}

// DeactivateHTTPS is the live-uninstall linearization companion: new CONNECT
// requests tunnel directly and HTTPS PAC routes are withdrawn immediately.
func (r *trafficRuntime) DeactivateHTTPS(snapshot userca.Snapshot) {
	r.mu.Lock()
	desiredChanged := r.interceptionState == HTTPSInterceptionActive
	if r.proxyCore != nil {
		r.proxyCore.DeactivateHTTPS()
	}
	r.userCA = snapshot
	r.readinessError = nil
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
	r.mu.Lock()
	if r.interceptionState != HTTPSInterceptionActive {
		r.mu.Unlock()
		return
	}
	switch failure.Kind {
	case corsproxy.HTTPSFailureReadiness:
		r.userCA = userca.Snapshot{}
		r.interceptionState = HTTPSInterceptionInactive
	default:
		r.interceptionState = HTTPSInterceptionFailed
	}
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
		if err := r.SetInitialHTTPSReadiness(userca.Snapshot{}, nil); err != nil {
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
	_ = r.proxy.Close()
	return r.pac.Close()
}

func (r *trafficRuntime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *trafficRuntime) RuntimeChanges() <-chan RuntimeChangeKind {
	return r.runtimeChanges
}

func (r *trafficRuntime) DesiredStates() <-chan managedpac.DesiredState {
	return r.desiredStates
}

func (r *trafficRuntime) snapshot() runtimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *trafficRuntime) watchUpstreamList(ctx context.Context, errs chan<- serverError) {
	updates, err := r.upstreamListSource.Updates(ctx)
	if err != nil {
		select {
		case errs <- serverError{source: "upstream-list", err: err}:
		case <-ctx.Done():
		}
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-updates:
			if !ok {
				return
			}
			r.applyUpstreamListState(state)
		}
	}
}

func (r *trafficRuntime) applyUpstreamListState(state upstreamlist.State) {
	r.mu.Lock()
	r.currentUpstreamList = state.List.Clone()
	r.upstreamListDiagnostic = state.Diagnostic.Clone()
	routeChanged := r.pacRouting.Apply(r.currentUpstreamList.HostSelectors, r.currentUpstreamList.OriginSelectors, r.interceptionState == HTTPSInterceptionActive)
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
	latestvalue.Publish(r.desiredStates, desired)
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
	latestvalue.Publish(r.runtimeChanges, kind)
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

func proxyGeneration(snapshot userca.Snapshot) (*corsproxy.HTTPSGeneration, bool) {
	certificate, usable := snapshot.TLSCertificate()
	if !usable {
		return nil, false
	}
	return &corsproxy.HTTPSGeneration{
		Certificate: certificate,
		ExpiresAt:   snapshot.ExpiresAt(),
	}, true
}

func hasHTTPSWarning(warnings []HTTPSWarningDetail, kind HTTPSWarningKind) bool {
	for _, warning := range warnings {
		if warning.Kind == kind {
			return true
		}
	}
	return false
}
