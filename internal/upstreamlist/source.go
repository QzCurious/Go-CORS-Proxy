package upstreamlist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QzCurious/seamless-cors/internal/latestvalue"
	"github.com/fsnotify/fsnotify"
)

const (
	defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

	changeDebounce      = 100 * time.Millisecond
	invalidConfirmation = time.Second
)

// DiagnosticKind classifies a runtime problem reported by a watched source.
type DiagnosticKind uint8

const (
	DiagnosticInvalidSource DiagnosticKind = iota + 1
	DiagnosticSourceUnavailable
	DiagnosticObservationStopped
)

// Diagnostic describes a non-fatal runtime problem with the source.
type Diagnostic struct {
	Kind DiagnosticKind
	Err  error
}

// Clone returns an independent diagnostic value.
func (d *Diagnostic) Clone() *Diagnostic {
	if d == nil {
		return nil
	}
	return &Diagnostic{Kind: d.Kind, Err: d.Err}
}

// SameDiagnostics reports whether two diagnostics describe the same source
// health state.
func SameDiagnostics(left, right *Diagnostic) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind {
		return false
	}
	if left.Err == nil || right.Err == nil {
		return left.Err == right.Err
	}
	return left.Err.Error() == right.Err.Error()
}

// State is a complete current-state snapshot. List remains the last-known-
// good semantic value while Diagnostic is non-nil.
type State struct {
	List       UpstreamList
	Diagnostic *Diagnostic
}

// Source is a watched, file-backed semantic Upstream List source. The path is
// supplied by Gateway; Source does not choose an application default.
type Source struct {
	mu         sync.RWMutex
	path       string
	current    UpstreamList
	hasCurrent bool
}

// New bootstraps path and returns a source without reading or validating the
// file. The caller owns the path policy; Source only cleans the supplied path.
func New(path string) (*Source, error) {
	path = filepath.Clean(path)
	if err := bootstrapFile(path, defaultUpstreamList); err != nil {
		return nil, err
	}
	return &Source{path: path}, nil
}

// Current returns the newest successfully validated semantic value. The first
// call reads and validates the ordinary file; later calls use the cache.
func (s *Source) Current() (UpstreamList, error) {
	s.mu.RLock()
	if s.hasCurrent {
		current := s.current.Clone()
		s.mu.RUnlock()
		return current, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasCurrent {
		return s.current.Clone(), nil
	}
	current, err := loadUpstreamList(s.path)
	if err != nil {
		return UpstreamList{}, err
	}
	s.current = current.Clone()
	s.hasCurrent = true
	return s.current.Clone(), nil
}

// Updates starts one source observation and returns complete state snapshots.
// Current must have succeeded before Updates is called. The returned channel
// is receive-only to consumers and has capacity one.
func (s *Source) Updates(ctx context.Context) (<-chan State, error) {
	s.mu.RLock()
	initialized := s.hasCurrent
	s.mu.RUnlock()
	if !initialized {
		return nil, errors.New("Upstream List source must be initialized with Current before Updates")
	}

	output := make(chan State, 1)
	go s.observe(ctx, output)
	return output, nil
}

type sourceObserver struct {
	source *Source
	target string
	dir    string

	debounceTimer *time.Timer
	debounceC     <-chan time.Time

	confirmationTimer   *time.Timer
	confirmationC       <-chan time.Time
	confirmationOpen    bool
	confirmationMatured bool

	publishedDiagnostic *Diagnostic
}

func (s *Source) observe(ctx context.Context, output chan State) {
	defer close(output)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		publishObservationStopped(output, s, fmt.Errorf("Upstream List observation could not start: %w", err))
		return
	}
	defer watcher.Close()

	observer := &sourceObserver{
		source: s,
		target: s.path,
		dir:    filepath.Dir(s.path),
	}
	defer observer.close()

	if err := watcher.Add(observer.dir); err != nil {
		publishObservationStopped(output, s, fmt.Errorf("Upstream List cannot observe %s: %w", observer.dir, err))
		return
	}

	// The watcher is established before this reconciliation. This closes the
	// Current-to-watch race: an edit after Current is either observed by
	// fsnotify or found by this first full reread.
	if observer.reconcile(ctx, output) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-observer.debounceC:
			observer.clearDebounce()
			if observer.reconcile(ctx, output) {
				return
			}
		case <-observer.confirmationC:
			observer.clearConfirmation()
			observer.confirmationMatured = true
			if observer.reconcile(ctx, output) {
				return
			}
		case event, ok := <-watcher.Events:
			if !ok {
				publishObservationStopped(output, s, errors.New("Upstream List observation stopped unexpectedly; later file edits will no longer be applied automatically"))
				return
			}
			observer.handleEvent(event)
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				publishObservationStopped(output, s, errors.New("Upstream List observation stopped unexpectedly; later file edits will no longer be applied automatically"))
				return
			}
			if errors.Is(watchErr, fsnotify.ErrEventOverflow) {
				observer.scheduleReconciliation()
				continue
			}
			publishObservationStopped(output, s, fmt.Errorf("Upstream List observation stopped unexpectedly; later file edits will no longer be applied automatically: %w", watchErr))
			return
		}
	}
}

func (o *sourceObserver) handleEvent(event fsnotify.Event) {
	if filepath.Clean(event.Name) != o.target {
		return
	}
	o.cancelConfirmation()
	o.scheduleReconciliation()
}

func (o *sourceObserver) scheduleReconciliation() {
	if o.debounceTimer == nil {
		o.debounceTimer = time.NewTimer(changeDebounce)
		o.debounceC = o.debounceTimer.C
		return
	}
	if !o.debounceTimer.Stop() {
		select {
		case <-o.debounceTimer.C:
		default:
		}
	}
	o.debounceTimer.Reset(changeDebounce)
}

func (o *sourceObserver) clearDebounce() {
	o.debounceTimer = nil
	o.debounceC = nil
}

func (o *sourceObserver) reconcile(ctx context.Context, output chan State) bool {
	if ctx.Err() != nil {
		return true
	}

	list, diagnostic := loadObservedUpstreamList(o.target)
	if diagnostic != nil {
		o.handleSourceError(output, *diagnostic)
		return false
	}

	o.cancelConfirmation()
	o.confirmationMatured = false

	o.source.mu.Lock()
	previous := o.source.current.Clone()
	changed := !Same(previous, list)
	if changed {
		// The cache is deliberately replaced before the complete state is
		// published so Current and Updates observe one ordering.
		o.source.current = list.Clone()
	}
	current := o.source.current.Clone()
	o.source.mu.Unlock()

	if !changed && o.publishedDiagnostic == nil {
		return false
	}
	state := State{List: current}
	if o.publishedDiagnostic != nil {
		o.publishedDiagnostic = nil
	}
	latestvalue.Publish(output, state)
	return false
}

func (o *sourceObserver) handleSourceError(output chan State, diagnostic Diagnostic) {
	if !o.confirmationOpen {
		o.beginConfirmation()
		return
	}
	if !o.confirmationMatured {
		return
	}

	if SameDiagnostics(o.publishedDiagnostic, &diagnostic) {
		return
	}
	o.publishedDiagnostic = diagnostic.Clone()
	latestvalue.Publish(output, o.source.currentState(&diagnostic))
}

func (o *sourceObserver) beginConfirmation() {
	o.cancelConfirmation()
	o.confirmationOpen = true
	o.confirmationMatured = false
	o.confirmationTimer = time.NewTimer(invalidConfirmation)
	o.confirmationC = o.confirmationTimer.C
}

func (o *sourceObserver) cancelConfirmation() {
	if o.confirmationTimer != nil {
		if !o.confirmationTimer.Stop() {
			select {
			case <-o.confirmationTimer.C:
			default:
			}
		}
	}
	o.confirmationTimer = nil
	o.confirmationC = nil
	o.confirmationOpen = false
	o.confirmationMatured = false
}

func (o *sourceObserver) clearConfirmation() {
	o.confirmationTimer = nil
	o.confirmationC = nil
}

func (s *Source) currentState(diagnostic *Diagnostic) State {
	s.mu.RLock()
	list := s.current.Clone()
	s.mu.RUnlock()
	return State{List: list, Diagnostic: diagnostic.Clone()}
}

func publishObservationStopped(output chan State, source *Source, err error) {
	diagnostic := &Diagnostic{
		Kind: DiagnosticObservationStopped,
		Err:  err,
	}
	latestvalue.Publish(output, source.currentState(diagnostic))
}

func (o *sourceObserver) close() {
	if o.debounceTimer != nil {
		o.debounceTimer.Stop()
	}
	if o.confirmationTimer != nil {
		o.confirmationTimer.Stop()
	}
}

func loadObservedUpstreamList(path string) (UpstreamList, *Diagnostic) {
	data, err := readRegularFile(path)
	if err != nil {
		return UpstreamList{}, &Diagnostic{Kind: DiagnosticSourceUnavailable, Err: err}
	}
	list, err := Decode(data)
	if err != nil {
		return UpstreamList{}, &Diagnostic{Kind: DiagnosticInvalidSource, Err: err}
	}
	return list, nil
}

func loadUpstreamList(path string) (UpstreamList, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return UpstreamList{}, err
	}
	return Decode(data)
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be an ordinary file", path)
	}
	return os.ReadFile(path)
}

func bootstrapFile(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
