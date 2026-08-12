package upstreamlist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

type Source struct {
	observation *fileobservation.Observation
	publisher   conflatedstream.Publisher[Transition]
	stream      conflatedstream.Stream[Transition]
	done        chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

type Transition interface{ upstreamListTransition() }
type ListAccepted struct{ List UpstreamList }
type SourceDegraded struct{ Err error }

func (ListAccepted) upstreamListTransition()   {}
func (SourceDegraded) upstreamListTransition() {}

func (e SourceDegraded) Error() string { return e.Err.Error() }
func (e SourceDegraded) Unwrap() error { return e.Err }

func Open(path string) *Source {
	path = filepath.Clean(path)
	publisher, stream := conflatedstream.New[Transition]()
	s := &Source{publisher: publisher, stream: stream, done: make(chan struct{})}
	observation, err := fileobservation.Open(path, fileobservation.Options{})
	if err != nil {
		publisher.Publish(SourceDegraded{Err: err})
		publisher.Close()
		close(s.done)
		return s
	}
	s.observation = observation
	first, ok := <-observation.Results()
	if !ok {
		publisher.Publish(SourceDegraded{Err: errors.New("upstream list observation stopped before its first result")})
		publisher.Close()
		close(s.done)
		return s
	}
	go s.translate(first)
	return s
}

func (s *Source) Transitions() <-chan Transition { return s.stream.Updates() }

func (s *Source) Close() error {
	s.closeOnce.Do(func() {
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
	defer s.publisher.Close()
	var last UpstreamList
	hasLast, degraded := false, false
	handle := func(result fileobservation.Result) {
		if result.Err != nil {
			degraded = true
			s.publisher.Publish(SourceDegraded{Err: result.Err})
			return
		}
		list, err := decodeAndDeduplicate(result.Contents)
		if err != nil {
			degraded = true
			s.publisher.Publish(SourceDegraded{Err: err})
			return
		}
		if !degraded && hasLast && sameUpstreamList(last, list) {
			return
		}
		last, hasLast, degraded = list, true, false
		s.publisher.Publish(ListAccepted{List: list})
	}
	handle(first)
	for result := range s.observation.Results() {
		handle(result)
	}
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

func sameUpstreamList(left, right UpstreamList) bool {
	if !sameHostSelectors(left.HostSelectors, right.HostSelectors) || !sameOriginSelectors(left.OriginSelectors, right.OriginSelectors) || len(left.Warnings) != len(right.Warnings) {
		return false
	}
	for i := range left.Warnings {
		if left.Warnings[i] != right.Warnings[i] {
			return false
		}
	}
	return true
}
func sameHostSelectors(left, right []HostSelector) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[HostSelector]struct{}, len(left))
	for _, v := range left {
		set[v] = struct{}{}
	}
	for _, v := range right {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}
func sameOriginSelectors(left, right []OriginSelector) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[OriginSelector]struct{}, len(left))
	for _, v := range left {
		set[v] = struct{}{}
	}
	for _, v := range right {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}
