package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/corsproxy"
	"github.com/QzCurious/seamless-cors/internal/liveconfig"
	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type trafficRuntime struct {
	mu                          sync.RWMutex
	currentSnapshot             liveconfig.Snapshot
	userCA                      userCASnapshot
	readinessError              error
	interceptionState           HTTPSInterceptionState
	interceptionError           error
	userCAOperationWarning      *HTTPSWarningDetail
	proxyCore                   *corsproxy.Core
	proxyHandler                *dynamicHTTPHandler
	proxyConfigured             bool
	httpsWarnings               []HTTPSWarningDetail
	proxy                       *http.Server
	pacHandler                  *pacrouting.DynamicHandler
	pac                         *http.Server
	listeners                   []net.Listener
	liveConfig                  *liveconfig.Config
	pacVersion                  uint64
	pacUpdates                  chan string
	httpsWarningUpdates         chan []HTTPSWarningDetail
	upstreamListEntriesRevision uint64
	now                         func() time.Time
}

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

func newRuntime(config *liveconfig.Config, snapshot liveconfig.Snapshot) (*trafficRuntime, error) {
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

	pacBody := pacrouting.Generate(pacrouting.Options{
		ProxyListen:  proxyListen,
		HTTPSActive:  false,
		UpstreamList: snapshot.UpstreamList(),
	})
	pacHandler := pacrouting.NewDynamicHandler(pacBody)
	proxyHandler := &dynamicHTTPHandler{current: http.NotFoundHandler()}
	return &trafficRuntime{
		currentSnapshot:             snapshot,
		liveConfig:                  config,
		proxyHandler:                proxyHandler,
		pacHandler:                  pacHandler,
		proxy:                       &http.Server{Handler: proxyHandler},
		pac:                         &http.Server{Handler: pacHandler},
		listeners:                   []net.Listener{proxyListener, pacListener},
		pacVersion:                  1,
		pacUpdates:                  make(chan string, 1),
		httpsWarningUpdates:         make(chan []HTTPSWarningDetail, 1),
		upstreamListEntriesRevision: snapshot.UpstreamListEntriesRevision(),
		now:                         time.Now,
	}, nil
}

func (r *trafficRuntime) SetInitialHTTPSReadiness(snapshot userCASnapshot, assessmentErr error) error {
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
	r.httpsWarnings = r.currentHTTPSWarningsLocked()
	r.pacHandler.Set(r.generatedPACLocked())
	r.mu.Unlock()
	return nil
}

// RecoverHTTPS applies a successful UserCA install to a live runtime. It
// returns a new PAC URL only when HTTPS routing changed from inactive to active.
func (r *trafficRuntime) RecoverHTTPS(snapshot userCASnapshot) (string, error) {
	generation, ok := proxyGeneration(snapshot)
	if !ok {
		return "", fmt.Errorf("HTTPS Readiness Recovery requires an Installed User CA")
	}
	r.mu.Lock()
	core := r.proxyCore
	wasActive := r.interceptionState == HTTPSInterceptionActive
	current := r.userCA
	r.mu.Unlock()
	if core == nil {
		return "", fmt.Errorf("HTTPS proxy is not configured")
	}
	if wasActive && sameUserCA(current, snapshot) {
		r.mu.Lock()
		r.userCA = snapshot
		r.readinessError = nil
		r.interceptionError = nil
		warnings, warningsChanged := r.updateHTTPSWarningsLocked()
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warnings, warningsChanged)
		return "", nil
	}
	if err := core.ActivateHTTPS(*generation); err != nil {
		return "", err
	}
	r.mu.Lock()
	r.userCA = snapshot
	r.readinessError = nil
	r.interceptionState = HTTPSInterceptionActive
	r.interceptionError = nil
	r.proxyConfigured = true
	warnings, warningsChanged := r.updateHTTPSWarningsLocked()
	if wasActive {
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warnings, warningsChanged)
		return "", nil
	}
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.pacHandler.Set(r.generatedPACLocked())
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warnings, warningsChanged)
	return nextURL, nil
}

func sameUserCA(left, right userCASnapshot) bool {
	if left.usable != right.usable || !left.usable {
		return left.usable == right.usable
	}
	leftCert := left.certificate
	rightCert := right.certificate
	if len(leftCert.Certificate) == 0 || len(rightCert.Certificate) == 0 {
		return false
	}
	return slices.Equal(leftCert.Certificate[0], rightCert.Certificate[0])
}

// DeactivateHTTPS is the live-uninstall linearization companion: new CONNECT
// requests tunnel directly and HTTPS PAC routes are withdrawn immediately.
func (r *trafficRuntime) DeactivateHTTPS(snapshot userCASnapshot) string {
	r.mu.Lock()
	wasActive := r.interceptionState == HTTPSInterceptionActive
	if r.proxyCore != nil {
		r.proxyCore.DeactivateHTTPS()
	}
	r.userCA = snapshot
	r.readinessError = nil
	r.interceptionState = HTTPSInterceptionInactive
	r.interceptionError = nil
	warnings, warningsChanged := r.updateHTTPSWarningsLocked()
	if !wasActive {
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warnings, warningsChanged)
		return ""
	}
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.pacHandler.Set(r.generatedPACLocked())
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warnings, warningsChanged)
	return nextURL
}

func (r *trafficRuntime) handleHTTPSFailure(failure corsproxy.HTTPSFailure) {
	r.mu.Lock()
	if r.interceptionState != HTTPSInterceptionActive {
		r.mu.Unlock()
		return
	}
	switch failure.Kind {
	case corsproxy.HTTPSFailureReadiness:
		r.userCA = userCASnapshot{}
		r.interceptionState = HTTPSInterceptionInactive
	default:
		r.interceptionState = HTTPSInterceptionFailed
	}
	r.interceptionError = failure.Err
	warnings, warningsChanged := r.updateHTTPSWarningsLocked()
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.pacHandler.Set(r.generatedPACLocked())
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warnings, warningsChanged)
	r.publishPACUpdate(nextURL)
}

func (r *trafficRuntime) Serve(ctx context.Context) error {
	return r.ServeReady(ctx, nil)
}

// ServeReady reports when both bound traffic listeners have entered their
// serving goroutines. Callers may then safely publish the PAC URL.
func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	if !r.proxyConfigured {
		if err := r.SetInitialHTTPSReadiness(userCASnapshot{}, nil); err != nil {
			return err
		}
	}
	errs := make(chan serverError, 3)
	go r.watchLiveConfig(ctx, errs)
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

func (r *trafficRuntime) PACURL() string {
	r.mu.RLock()
	version := r.pacVersion
	r.mu.RUnlock()
	return r.pacURL(version)
}

func (r *trafficRuntime) PACListen() string {
	return r.listeners[1].Addr().String()
}

func (r *trafficRuntime) PACURLUpdates() <-chan string {
	return r.pacUpdates
}

func (r *trafficRuntime) HTTPSWarningUpdates() <-chan []HTTPSWarningDetail {
	return r.httpsWarningUpdates
}

func (r *trafficRuntime) snapshot() runtimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *trafficRuntime) watchLiveConfig(ctx context.Context, errs chan<- serverError) {
	err := r.liveConfig.Observe(ctx, func(snapshot liveconfig.Snapshot) {
		r.applyLiveConfig(snapshot)
	})
	if err == nil {
		return
	}
	select {
	case errs <- serverError{source: "live-config", err: err}:
	case <-ctx.Done():
	}
}

func (r *trafficRuntime) applyLiveConfig(snapshot liveconfig.Snapshot) {
	r.mu.Lock()
	routingInputsChanged := snapshot.UpstreamListEntriesRevision() != r.upstreamListEntriesRevision
	r.currentSnapshot = snapshot
	warnings, warningsChanged := r.updateHTTPSWarningsLocked()
	if !routingInputsChanged {
		r.mu.Unlock()
		r.publishHTTPSWarningUpdate(warnings, warningsChanged)
		return
	}
	r.upstreamListEntriesRevision = snapshot.UpstreamListEntriesRevision()
	r.pacVersion++
	nextURL := r.pacURL(r.pacVersion)
	r.pacHandler.Set(r.generatedPACLocked())
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warnings, warningsChanged)
	r.publishPACUpdate(nextURL)
}

func (r *trafficRuntime) publishPACUpdate(nextURL string) {
	select {
	case r.pacUpdates <- nextURL:
	default:
		select {
		case <-r.pacUpdates:
		default:
		}
		r.pacUpdates <- nextURL
	}
}

func (r *trafficRuntime) SetUninstallWarning(err error) {
	r.mu.Lock()
	r.userCAOperationWarning = &HTTPSWarningDetail{
		Kind:       HTTPSWarningUninstallIncomplete,
		Diagnostic: fmt.Sprintf("Installed User CA uninstall is incomplete: %v.", err),
		Action:     "Run `seamless-cors uninstall` again.",
	}
	warnings, changed := r.updateHTTPSWarningsLocked()
	r.mu.Unlock()
	r.publishHTTPSWarningUpdate(warnings, changed)
}

func (r *trafficRuntime) updateHTTPSWarningsLocked() ([]HTTPSWarningDetail, bool) {
	next := r.currentHTTPSWarningsLocked()
	if slices.Equal(next, r.httpsWarnings) {
		return nil, false
	}
	r.httpsWarnings = next
	return append([]HTTPSWarningDetail(nil), next...), true
}

func (r *trafficRuntime) publishHTTPSWarningUpdate(warnings []HTTPSWarningDetail, changed bool) {
	if !changed {
		return
	}
	select {
	case r.httpsWarningUpdates <- warnings:
	default:
		select {
		case <-r.httpsWarningUpdates:
		default:
		}
		r.httpsWarningUpdates <- warnings
	}
}

func (r *trafficRuntime) currentHTTPSWarningsLocked() []HTTPSWarningDetail {
	var warnings []HTTPSWarningDetail
	warnings = append(warnings, httpsRuntimeWarnings(
		r.currentSnapshot.UpstreamList(),
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

func (r *trafficRuntime) generatedPACLocked() string {
	return pacrouting.Generate(pacrouting.Options{
		ProxyListen:  r.listeners[0].Addr().String(),
		HTTPSActive:  r.interceptionState == HTTPSInterceptionActive,
		UpstreamList: r.currentSnapshot.UpstreamList(),
	})
}

func (r *trafficRuntime) pacURL(version uint64) string {
	u := url.URL{
		Scheme:   "http",
		Host:     r.listeners[1].Addr().String(),
		Path:     "/" + managedpac.FootprintFileName,
		RawQuery: fmt.Sprintf("v=%d", version),
	}
	return u.String()
}

type runtimeState struct {
	ProxyListen          string
	PACListen            string
	UpstreamList         string
	HTTPSReadiness       HTTPSReadinessStatus
	HTTPSInterception    HTTPSInterceptionState
	HTTPSIntent          bool
	HTTPSWarnings        []HTTPSWarningDetail
	UpstreamCount        int
	UpstreamListWarnings []UpstreamListWarningDetail
}

func (r *trafficRuntime) stateLocked() runtimeState {
	upstreamList := r.currentSnapshot.UpstreamList()
	readiness := httpsReadinessStatus(r.userCA)
	interception := r.interceptionState
	warnings := append([]HTTPSWarningDetail(nil), r.httpsWarnings...)
	if r.userCA.usable && !r.now().Before(r.userCA.expiresAt) {
		readiness = HTTPSReadinessNotReady
		interception = HTTPSInterceptionInactive
		warnings = []HTTPSWarningDetail{{
			Kind:       HTTPSWarningReadinessUnavailable,
			Diagnostic: "Installed User CA has expired.",
			Action:     "Run `seamless-cors install`.",
		}}
	}
	return runtimeState{
		ProxyListen:          r.listeners[0].Addr().String(),
		PACListen:            r.listeners[1].Addr().String(),
		UpstreamList:         r.currentSnapshot.UpstreamListPath(),
		HTTPSReadiness:       readiness,
		HTTPSInterception:    interception,
		HTTPSIntent:          hasHTTPSIntent(upstreamList),
		HTTPSWarnings:        warnings,
		UpstreamCount:        len(upstreamList.HostSelectors) + len(upstreamList.OriginSelectors),
		UpstreamListWarnings: upstreamListWarningDetails(upstreamList.Warnings),
	}
}

func hasHTTPSIntent(list upstreamlist.UpstreamList) bool {
	for _, selector := range list.OriginSelectors {
		if selector.Scheme == "https" {
			return true
		}
	}
	return false
}

func httpsReadinessStatus(snapshot userCASnapshot) HTTPSReadinessStatus {
	if snapshot.usable {
		return HTTPSReadinessReady
	}
	return HTTPSReadinessNotReady
}

func httpsReadinessWarnings(list upstreamlist.UpstreamList, snapshot userCASnapshot, assessmentErr error) []HTTPSWarningDetail {
	if assessmentErr != nil {
		return []HTTPSWarningDetail{{
			Kind:       HTTPSWarningReadinessUnavailable,
			Diagnostic: fmt.Sprintf("HTTPS Readiness could not be assessed: %v.", assessmentErr),
			Action:     "Run `seamless-cors install`.",
		}}
	}
	if snapshot.usable {
		var warnings []HTTPSWarningDetail
		if snapshot.renewalDue {
			warnings = append(warnings, HTTPSWarningDetail{
				Kind:       HTTPSWarningRenewalRecommended,
				Diagnostic: fmt.Sprintf("Installed User CA expires soon (%s).", snapshot.expiresAt.Format("2006-01-02")),
				Action:     "Run `seamless-cors install` to renew it.",
			})
		}
		return warnings
	}
	if !hasHTTPSIntent(list) {
		return nil
	}
	return []HTTPSWarningDetail{{
		Kind:       HTTPSWarningUnmetIntent,
		Diagnostic: "HTTPS was requested but the Installed User CA is not usable.",
		Action:     "Run `seamless-cors install`.",
	}}
}

func httpsRuntimeWarnings(
	list upstreamlist.UpstreamList,
	snapshot userCASnapshot,
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
	return httpsReadinessWarnings(list, snapshot, assessmentErr)
}

func proxyGeneration(snapshot userCASnapshot) (*corsproxy.HTTPSGeneration, bool) {
	if !snapshot.usable {
		return nil, false
	}
	return &corsproxy.HTTPSGeneration{
		Certificate: snapshot.certificate,
		ExpiresAt:   snapshot.expiresAt,
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
