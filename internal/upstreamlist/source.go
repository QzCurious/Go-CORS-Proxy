package upstreamlist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

type Source struct {
	observation *fileobservation.Observation
	transitions chan Transition
	done        chan struct{}
	closing     chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

type Transition interface{ upstreamListTransition() }
type Projection struct{ List UpstreamList }
type InvalidFormat struct{ Err error }

type ObservationErrorKind string

const (
	ObservationReadFailed ObservationErrorKind = "read-failed"
	ObservationUncertain  ObservationErrorKind = "observation-uncertain"
	ObservationStopped    ObservationErrorKind = "observation-stopped"
)

type ObservationError struct {
	Kind ObservationErrorKind
	Err  error
}

func (Projection) upstreamListTransition()       {}
func (InvalidFormat) upstreamListTransition()    {}
func (ObservationError) upstreamListTransition() {}

func Open(path string) *Source {
	path = filepath.Clean(path)
	s := &Source{transitions: make(chan Transition, 1), done: make(chan struct{}), closing: make(chan struct{})}
	observation, err := fileobservation.Open(path, fileobservation.Options{})
	if err != nil {
		s.transitions <- ObservationError{Kind: ObservationStopped, Err: err}
		close(s.transitions)
		close(s.done)
		return s
	}
	s.observation = observation
	first, ok := <-observation.Results()
	if !ok {
		s.transitions <- ObservationError{Kind: ObservationStopped, Err: errors.New("upstream list observation stopped before its first result")}
		close(s.transitions)
		close(s.done)
		return s
	}
	go s.translate(first)
	return s
}

func (s *Source) Transitions() <-chan Transition { return s.transitions }

func (s *Source) Close() error {
	s.closeOnce.Do(func() {
		close(s.closing)
		if s.observation != nil {
			s.closeErr = s.observation.Close()
		}
		<-s.done
	})
	return s.closeErr
}

type BootstrapFingerprint string
type BootstrapAssessment struct {
	Required                 bool
	Path                     string
	DefaultContents          string
	MissingParentDirectories []string
	Fingerprint              BootstrapFingerprint
}

const defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

func AssessBootstrap(path string) BootstrapAssessment {
	path = filepath.Clean(path)
	assessment := BootstrapAssessment{Path: path, DefaultContents: defaultUpstreamList}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return assessment
	}
	assessment.Required = true
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if _, err := os.Lstat(parent); err == nil {
			break
		}
		assessment.MissingParentDirectories = append(assessment.MissingParentDirectories, parent)
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
	}
	sum := sha256.Sum256([]byte(path + "\x00" + defaultUpstreamList))
	assessment.Fingerprint = BootstrapFingerprint(hex.EncodeToString(sum[:]))
	return assessment
}

func Bootstrap(path string) error {
	return createFile(filepath.Clean(path), defaultUpstreamList)
}

type creationError struct {
	path string
	err  error
}

func (e *creationError) Error() string {
	return fmt.Sprintf("create upstream list %q: %v", e.path, e.err)
}
func (e *creationError) Unwrap() error { return e.err }

func createFile(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return &creationError{path, err}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return &creationError{path, err}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return &creationError{path, err}
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return &creationError{path, err}
	}
	if err := file.Close(); err != nil {
		return &creationError{path, err}
	}
	return nil
}

func (s *Source) translate(first fileobservation.Result) {
	defer close(s.done)
	defer close(s.transitions)
	handle := func(result fileobservation.Result) {
		if result.Err != nil {
			s.transitions <- observationError(result.Err)
			return
		}
		list, err := decodeAndDeduplicate(result.Contents)
		if err != nil {
			s.transitions <- InvalidFormat{Err: err}
			return
		}
		s.transitions <- Projection{List: list}
	}
	handle(first)
	for result := range s.observation.Results() {
		handle(result)
	}
	select {
	case <-s.closing:
	default:
		s.transitions <- ObservationError{Kind: ObservationStopped, Err: errors.New("upstream list observation stopped")}
	}
}

func observationError(err error) ObservationError {
	kind := ObservationStopped
	var observed *fileobservation.Error
	if errors.As(err, &observed) {
		switch observed.Kind {
		case fileobservation.ErrorRead:
			kind = ObservationReadFailed
		case fileobservation.ErrorObservationUncertain:
			kind = ObservationUncertain
		case fileobservation.ErrorObservationStopped:
			kind = ObservationStopped
		}
	}
	return ObservationError{Kind: kind, Err: err}
}

func decodeAndDeduplicate(data []byte) (UpstreamList, error) {
	parsed, err := decode(data)
	if err != nil {
		return UpstreamList{}, err
	}
	return deduplicate(parsed), nil
}

func deduplicate(parsed parsedUpstreamList) UpstreamList {
	var hosts []HostSelector
	seenHosts := make(map[HostSelector]struct{}, len(parsed.HostSelectors))
	for _, selector := range parsed.HostSelectors {
		if _, ok := seenHosts[selector]; !ok {
			seenHosts[selector] = struct{}{}
			hosts = append(hosts, selector)
		}
	}
	var origins []OriginSelector
	seenOrigins := make(map[OriginSelector]struct{}, len(parsed.OriginSelectors))
	for _, selector := range parsed.OriginSelectors {
		if _, ok := seenOrigins[selector]; !ok {
			seenOrigins[selector] = struct{}{}
			origins = append(origins, selector)
		}
	}
	return UpstreamList{HostSelectors: hosts, OriginSelectors: origins, Warnings: parsed.Warnings}
}
