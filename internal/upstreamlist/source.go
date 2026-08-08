package upstreamlist

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/fileprojection"
)

const defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

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

// State is a complete current-state snapshot. List remains the last-known-
// good semantic value while Diagnostic is non-nil.
type State struct {
	List       UpstreamList
	Diagnostic *Diagnostic
}

// Source adapts a generic File Projection into a semantic Upstream List
// source with last-known-good behavior.
type Source struct {
	projection *fileprojection.Projection[UpstreamList]
	updates    *conflatedstream.Stream[State]
	done       chan struct{}

	mu      sync.RWMutex
	current State

	closeOnce sync.Once
	closeErr  error
}

// Open bootstraps path, validates its initial contents, and begins observing
// subsequent changes. The caller owns the path policy and Source lifetime.
func Open(path string) (*Source, error) {
	path = filepath.Clean(path)
	if err := bootstrapFile(path, defaultUpstreamList); err != nil {
		return nil, err
	}
	projection, err := fileprojection.Open(
		path,
		decodeAndDeduplicate,
		sameUpstreamList,
		fileprojection.Options{},
	)
	if err != nil {
		return nil, err
	}

	s := &Source{
		projection: projection,
		updates:    conflatedstream.New[State](),
		done:       make(chan struct{}),
		current:    State{List: projection.Current().Value},
	}
	go s.translate()
	return s, nil
}

func (s *Source) Current() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Updates returns the single-consumer, latest-value stream of post-initial
// complete source states.
func (s *Source) Updates() <-chan State { return s.updates.Updates() }

func (s *Source) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.projection.Close()
		<-s.done
	})
	return s.closeErr
}

func (s *Source) translate() {
	defer close(s.done)
	defer s.updates.Close()
	for result := range s.projection.Updates() {
		s.mu.Lock()
		state := State{List: s.current.List}
		if result.Err == nil {
			state.List = result.Value
		} else {
			state.Diagnostic = diagnosticFor(result.Err)
		}
		s.current = state
		s.mu.Unlock()
		s.updates.Publish(state)
	}
}

func diagnosticFor(err error) *Diagnostic {
	kind := DiagnosticObservationStopped
	var projectionError *fileprojection.Error
	if errors.As(err, &projectionError) {
		switch projectionError.Kind {
		case fileprojection.ErrorRead:
			kind = DiagnosticSourceUnavailable
		case fileprojection.ErrorProject:
			kind = DiagnosticInvalidSource
		}
	}
	return &Diagnostic{Kind: kind, Err: err}
}

func decodeAndDeduplicate(data []byte) (UpstreamList, error) {
	parsed, err := decode(data)
	if err != nil {
		return UpstreamList{}, err
	}
	return deduplicate(parsed), nil
}

func deduplicate(parsed parsedUpstreamList) UpstreamList {
	var hostSelectors []HostSelector
	seenHosts := make(map[HostSelector]struct{}, len(parsed.HostSelectors))
	for _, selector := range parsed.HostSelectors {
		if _, ok := seenHosts[selector]; ok {
			continue
		}
		seenHosts[selector] = struct{}{}
		hostSelectors = append(hostSelectors, selector)
	}

	var originSelectors []OriginSelector
	seenOrigins := make(map[OriginSelector]struct{}, len(parsed.OriginSelectors))
	for _, selector := range parsed.OriginSelectors {
		if _, ok := seenOrigins[selector]; ok {
			continue
		}
		seenOrigins[selector] = struct{}{}
		originSelectors = append(originSelectors, selector)
	}

	return UpstreamList{
		HostSelectors:   hostSelectors,
		OriginSelectors: originSelectors,
		Warnings:        append([]Warning(nil), parsed.Warnings...),
	}
}

func sameUpstreamList(left, right UpstreamList) bool {
	if !sameHostSelectors(left.HostSelectors, right.HostSelectors) ||
		!sameOriginSelectors(left.OriginSelectors, right.OriginSelectors) {
		return false
	}
	if len(left.Warnings) != len(right.Warnings) {
		return false
	}
	for index := range left.Warnings {
		if left.Warnings[index] != right.Warnings[index] {
			return false
		}
	}
	return true
}

func sameHostSelectors(left, right []HostSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[HostSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
}

func sameOriginSelectors(left, right []OriginSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[OriginSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
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
