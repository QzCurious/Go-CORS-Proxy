package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/httpsfacade"
	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
	"github.com/QzCurious/seamless-cors/internal/pacrouting"
	"github.com/QzCurious/seamless-cors/internal/proxy"
	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

type trafficRuntime struct {
	mu                  sync.RWMutex
	upstreamLists       []runtimeUpstreamListSource
	currentUpstreamList upstreamlist.Projection
	userCA              userCAState
	userCAAssessmentErr error
	userCARevision      uint64
	latest              trafficProjectionSemantics
	served              trafficProjectionSemantics
	live                *liveTrafficProjection
	proxyTransport      *http.Transport
	proxy               *http.Server
	pac                 *http.Server
	listeners           []net.Listener
	publishMu           sync.Mutex
	deliveryPublisher   conflatedstream.Publisher[struct{}]
	deliveryStream      conflatedstream.Stream[struct{}]
	changePublisher     conflatedstream.Publisher[RuntimeChangeKind]
	changeStream        conflatedstream.Stream[RuntimeChangeKind]
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

type RuntimeChangeKind uint8

const (
	RuntimeStatusChanged RuntimeChangeKind = iota
	UserCAAssessmentRequested
)

type serverError struct {
	source string
	err    error
}

type trafficProjectionSemantics struct {
	pacContent        string
	httpCORSRoutes    bool
	httpsCORSRoutes   bool
	httpsFacadeRoutes bool
	userCAIdentity    string
}

func (s trafficProjectionSemantics) equivalent(other trafficProjectionSemantics) bool {
	return s.pacContent == other.pacContent &&
		s.httpCORSRoutes == other.httpCORSRoutes &&
		s.httpsCORSRoutes == other.httpsCORSRoutes &&
		s.httpsFacadeRoutes == other.httpsFacadeRoutes &&
		s.userCAIdentity == other.userCAIdentity
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
	}}, proxyTransport, userCAState{}, nil)
}

func newRuntimeFromSources(
	inputs []runtimeUpstreamListInput,
	proxyTransport *http.Transport,
	currentUserCA userCAState,
	userCAAssessmentErr error,
) (*trafficRuntime, error) {
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
		_ = proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}

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

	deliveryPublisher, deliveryStream := conflatedstream.New[struct{}]()
	changePublisher, changeStream := conflatedstream.New[RuntimeChangeKind]()
	live := newLiveTrafficProjection()
	runtime := &trafficRuntime{
		upstreamLists:       sources,
		currentUpstreamList: upstreamlist.Merge(projections...),
		userCA:              currentUserCA,
		userCAAssessmentErr: userCAAssessmentErr,
		userCARevision:      1,
		live:                live,
		proxyTransport:      proxyTransport,
		proxy:               &http.Server{Handler: http.HandlerFunc(live.serveProxy)},
		pac:                 &http.Server{Handler: http.HandlerFunc(live.servePAC)},
		listeners:           []net.Listener{proxyListener, pacListener},
		deliveryPublisher:   deliveryPublisher,
		deliveryStream:      deliveryStream,
		changePublisher:     changePublisher,
		changeStream:        changeStream,
	}
	runtime.switchTrafficProjectionLocked()
	return runtime, nil
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
	if optional {
		if readErr, ok := outcome.(fileobservation.ReadError); ok && errors.Is(readErr.Cause, os.ErrNotExist) {
			return fileobservation.Contents(nil)
		}
	}
	return outcome
}

func defaultProxyTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

func (r *trafficRuntime) AdoptUserCA(current userCAState, assessmentErr error) {
	r.mu.Lock()
	r.userCA = current
	r.userCAAssessmentErr = assessmentErr
	r.userCARevision++
	changed := r.switchTrafficProjectionLocked()
	r.mu.Unlock()
	if changed {
		r.publishDeliveryRequest()
	}
	r.publishRuntimeChange(RuntimeStatusChanged)
}

func (r *trafficRuntime) ExpireUserCA(expectedRevision uint64) bool {
	r.mu.Lock()
	if r.userCARevision != expectedRevision || !r.userCA.Usable {
		r.mu.Unlock()
		return false
	}
	r.userCA = userCAState{}
	r.userCAAssessmentErr = nil
	r.userCARevision++
	changed := r.switchTrafficProjectionLocked()
	r.mu.Unlock()
	if changed {
		r.publishDeliveryRequest()
	}
	r.publishRuntimeChange(RuntimeStatusChanged)
	return true
}

func (r *trafficRuntime) Serve(ctx context.Context) error { return r.ServeReady(ctx, nil) }

func (r *trafficRuntime) ServeReady(ctx context.Context, ready chan<- struct{}) error {
	errs := make(chan serverError, len(r.upstreamLists)+2)
	for index := range r.upstreamLists {
		go r.watchUpstreamList(ctx, index, errs)
	}
	go func() { errs <- serverError{source: "proxy", err: r.proxy.Serve(r.listeners[0])} }()
	go func() { errs <- serverError{source: "pac", err: r.pac.Serve(r.listeners[1])} }()
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

func (r *trafficRuntime) Close() error { return r.CloseTraffic() }

func (r *trafficRuntime) CloseTraffic() error {
	for index := range r.upstreamLists {
		if r.upstreamLists[index].observation != nil {
			r.upstreamLists[index].observation.Close()
		}
	}
	err := errors.Join(r.proxy.Close(), r.pac.Close())
	r.proxyTransport.CloseIdleConnections()
	return err
}

func (r *trafficRuntime) PACListen() string { return r.listeners[1].Addr().String() }

func (r *trafficRuntime) RuntimeChanges() <-chan RuntimeChangeKind { return r.changeStream.Updates() }

func (r *trafficRuntime) DeliveryRequests() <-chan struct{} { return r.deliveryStream.Updates() }

func (r *trafficRuntime) snapshot() runtimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked()
}

func (r *trafficRuntime) interceptionActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.userCA.Usable && r.userCA.Identity != "" && r.userCA.Identity == r.served.userCAIdentity &&
		(r.served.httpsCORSRoutes || r.served.httpsFacadeRoutes)
}

func (r *trafficRuntime) watchUpstreamList(ctx context.Context, sourceIndex int, errs chan<- serverError) {
	source := &r.upstreamLists[sourceIndex]
	if source.observation == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case outcome, ok := <-source.observation.Outcomes():
			if !ok {
				return
			}
			if err := r.applyUpstreamListSourceOutcomeContext(sourceIndex, outcome); err != nil {
				if ctx.Err() == nil {
					errs <- serverError{source: string(source.kind) + "-upstream-list", err: err}
				}
				return
			}
		}
	}
}

func (r *trafficRuntime) applyUpstreamListOutcome(outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(0, outcome)
}

func (r *trafficRuntime) applyUpstreamListOutcomeContext(_ context.Context, outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(0, outcome)
}

func (r *trafficRuntime) applyUpstreamListSourceOutcome(sourceIndex int, outcome fileobservation.Outcome) error {
	return r.applyUpstreamListSourceOutcomeContext(sourceIndex, outcome)
}

func (r *trafficRuntime) applyUpstreamListSourceOutcomeContext(sourceIndex int, outcome fileobservation.Outcome) error {
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

	if observationError != nil {
		next, err := classifyFileSyncIssue(observationError)
		if err != nil {
			return err
		}
		r.mu.Lock()
		changed := !sameFileSyncIssue(source.fileSyncIssue, next)
		source.fileSyncIssue = next
		r.mu.Unlock()
		if changed {
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
	source.fileSyncIssue = nil
	source.projectionIssue = nextProjectionIssue
	source.projection = candidate
	projections := make([]upstreamlist.Projection, 0, len(r.upstreamLists))
	for index := range r.upstreamLists {
		projections = append(projections, r.upstreamLists[index].projection)
	}
	r.currentUpstreamList = upstreamlist.Merge(projections...)
	trafficChanged := r.switchTrafficProjectionLocked()
	requestAssessment := !r.userCA.Usable || r.userCAAssessmentErr != nil
	r.mu.Unlock()
	if trafficChanged {
		r.publishDeliveryRequest()
	}
	if requestAssessment {
		r.publishRuntimeChange(UserCAAssessmentRequested)
	} else {
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

func (r *trafficRuntime) switchTrafficProjectionLocked() bool {
	hasHost, hasHTTPOrigin, hasHTTPSOrigin := selectorFacts(r.currentUpstreamList)
	httpDemand := hasHost || hasHTTPOrigin
	httpsDemand := hasHTTPSOrigin || (hasHost && r.userCA.Usable)
	trustedHTTPS := r.userCAAssessmentErr == nil && r.userCA.Usable && r.userCA.Identity != "" && r.userCA.SigningMaterial() != nil &&
		(httpsDemand || hasHTTPOrigin)

	canonical := canonicalUpstreams(r.currentUpstreamList)
	facades := httpsfacade.Projection{}
	if trustedHTTPS {
		facades = httpsfacade.Project(canonical.OriginSelectors)
	}
	pacContent := pacrouting.Project(
		canonical,
		facades,
		trustedHTTPS,
		r.listeners[0].Addr().String(),
	)
	identity := ""
	if trustedHTTPS {
		identity = r.userCA.Identity
	}
	semantics := trafficProjectionSemantics{
		pacContent:        pacContent,
		httpCORSRoutes:    httpDemand,
		httpsCORSRoutes:   trustedHTTPS && httpsDemand,
		httpsFacadeRoutes: trustedHTTPS && len(facades.Routes()) > 0,
		userCAIdentity:    identity,
	}
	r.latest = semantics
	if r.served.equivalent(semantics) && r.live.current.Load() != nil {
		return false
	}
	next := &servedTrafficProjection{
		pacContent: pacContent,
		proxy:      proxy.New(r.proxyTransport, r.userCA.SigningMaterialIf(trustedHTTPS), facades),
	}
	r.live.Store(next)
	r.served = semantics
	return true
}

func canonicalUpstreams(projection upstreamlist.Projection) upstreamlist.Projection {
	hosts := append([]upstreamlist.HostSelector(nil), projection.HostSelectors...)
	origins := append([]upstreamlist.OriginSelector(nil), projection.OriginSelectors...)
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Hostname != hosts[j].Hostname {
			return hosts[i].Hostname < hosts[j].Hostname
		}
		return !hosts[i].Wildcard && hosts[j].Wildcard
	})
	sort.Slice(origins, func(i, j int) bool {
		if origins[i].Scheme != origins[j].Scheme {
			return origins[i].Scheme < origins[j].Scheme
		}
		if origins[i].Hostname != origins[j].Hostname {
			return origins[i].Hostname < origins[j].Hostname
		}
		return origins[i].Port < origins[j].Port
	})
	return upstreamlist.Projection{HostSelectors: hosts, OriginSelectors: origins}
}

func selectorFacts(projection upstreamlist.Projection) (hasHost, hasHTTPOrigin, hasHTTPSOrigin bool) {
	hasHost = len(projection.HostSelectors) > 0
	for _, selector := range projection.OriginSelectors {
		hasHTTPOrigin = hasHTTPOrigin || selector.Scheme == "http"
		hasHTTPSOrigin = hasHTTPSOrigin || selector.Scheme == "https"
	}
	return hasHost, hasHTTPOrigin, hasHTTPSOrigin
}

func (r *trafficRuntime) publishDeliveryRequest() {
	r.publishMu.Lock()
	r.deliveryPublisher.Publish(struct{}{})
	r.publishMu.Unlock()
}

func (r *trafficRuntime) publishRuntimeChange(kind RuntimeChangeKind) {
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	if kind == RuntimeStatusChanged {
		select {
		case pending := <-r.changeStream.Updates():
			if pending == UserCAAssessmentRequested {
				r.changePublisher.Publish(pending)
				return
			}
		default:
		}
	}
	r.changePublisher.Publish(kind)
}

type runtimeState struct {
	ProxyListen              string
	PACListen                string
	UpstreamLists            []UpstreamListSourceDetail
	UpstreamCount            int
	HTTPDemand               bool
	HTTPSDemand              bool
	ServedHTTPCORS           bool
	ServedHTTPSCORS          bool
	ServedHTTPSFacade        bool
	TrafficProjectionCurrent bool
	UserCAUsable             bool
	UserCAIdentityMatches    bool
	UserCARevision           uint64
}

func (r *trafficRuntime) stateLocked() runtimeState {
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
	hasHost, hasHTTPOrigin, hasHTTPSOrigin := selectorFacts(r.currentUpstreamList)
	state := runtimeState{
		ProxyListen:              r.listeners[0].Addr().String(),
		PACListen:                r.listeners[1].Addr().String(),
		UpstreamLists:            sources,
		UpstreamCount:            len(r.currentUpstreamList.HostSelectors) + len(r.currentUpstreamList.OriginSelectors),
		HTTPDemand:               hasHost || hasHTTPOrigin,
		HTTPSDemand:              hasHTTPSOrigin || (hasHost && r.userCA.Usable),
		ServedHTTPCORS:           r.served.httpCORSRoutes,
		ServedHTTPSCORS:          r.served.httpsCORSRoutes,
		ServedHTTPSFacade:        r.served.httpsFacadeRoutes,
		TrafficProjectionCurrent: r.served.equivalent(r.latest),
		UserCAUsable:             r.userCA.Usable,
		UserCAIdentityMatches:    r.userCA.Usable && r.userCA.Identity != "" && r.userCA.Identity == r.served.userCAIdentity,
		UserCARevision:           r.userCARevision,
	}
	return state
}
