// Package fileprojection projects an ordinary file into an immutable current
// value and keeps that projection synchronized with later file changes.
package fileprojection

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
	"github.com/fsnotify/fsnotify"
)

// Projection maintains the current projection of a file. Its embedded Stream
// provides single-consumer, latest-value updates for post-initial results.
type Projection[T any] struct {
	conflatedstream.Stream[Result[T]]
	resultPublisher conflatedstream.Publisher[Result[T]]

	path     string
	project  ProjectFunc[T]
	equal    EqualFunc[T]
	debounce time.Duration
	watcher  *fsnotify.Watcher

	latest atomic.Pointer[Result[T]]
	stop   chan struct{}
	done   chan struct{}

	hasSuccessfulFingerprint bool
	successfulFingerprint    [sha256.Size]byte

	closeOnce sync.Once
	closeErr  error
}

type Result[T any] struct {
	Value T
	Err   error
}

func Open[T any](path string, project ProjectFunc[T], equal EqualFunc[T], options Options) (*Projection[T], error) {
	if project == nil {
		return nil, errors.New("file projection requires a project function")
	}
	if equal == nil {
		return nil, errors.New("file projection requires an equality function")
	}
	if options.Debounce < 0 {
		return nil, errors.New("file projection debounce must not be negative")
	}
	if options.Debounce == 0 {
		options.Debounce = defaultDebounce
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, &Error{Kind: ErrorObservation, Path: path, Err: err}
	}
	absolutePath = filepath.Clean(absolutePath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, &Error{Kind: ErrorObservation, Path: absolutePath, Err: err}
	}
	if err := watcher.Add(filepath.Dir(absolutePath)); err != nil {
		_ = watcher.Close()
		return nil, &Error{Kind: ErrorObservation, Path: absolutePath, Err: err}
	}

	resultPublisher, resultStream := conflatedstream.New[Result[T]]()
	p := &Projection[T]{
		Stream:          resultStream,
		resultPublisher: resultPublisher,
		path:            absolutePath,
		project:         project,
		equal:           equal,
		debounce:        options.Debounce,
		watcher:         watcher,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
	initial := p.readAndProject()
	if initial.Err != nil {
		_ = watcher.Close()
		return nil, initial.Err
	}
	p.store(initial)

	go p.observe()
	return p, nil
}

func (p *Projection[T]) Current() Result[T] {
	return *p.latest.Load()
}

func (p *Projection[T]) Close() error {
	p.closeOnce.Do(func() {
		close(p.stop)
		p.closeErr = p.watcher.Close()
		<-p.done
	})
	return p.closeErr
}

type ProjectFunc[T any] func([]byte) (T, error)

type EqualFunc[T any] func(T, T) bool

type Options struct {
	// Debounce is the quiet period after the latest relevant filesystem event.
	// Zero uses 100ms.
	Debounce time.Duration
}

const defaultDebounce = 100 * time.Millisecond

type ErrorKind uint8

const (
	ErrorRead ErrorKind = iota + 1
	ErrorProject
	ErrorObservation
)

type Error struct {
	Kind ErrorKind
	Path string
	Err  error
}

func (e *Error) Error() string {
	var operation string
	switch e.Kind {
	case ErrorRead:
		operation = "read"
	case ErrorProject:
		operation = "project"
	case ErrorObservation:
		operation = "observe"
	default:
		operation = "process"
	}
	return fmt.Sprintf("file projection cannot %s %q: %v", operation, e.Path, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func failure[T any](kind ErrorKind, path string, err error) Result[T] {
	return Result[T]{Err: &Error{Kind: kind, Path: path, Err: err}}
}

func (p *Projection[T]) observe() {
	defer close(p.done)
	defer p.resultPublisher.Close()

	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(p.debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(p.debounce)
		}
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-p.stop:
			return
		case <-timerC:
			timerC = nil
			p.reconcile()
		case event, ok := <-p.watcher.Events:
			if !ok {
				if !p.stopping() {
					p.stopObservation(errors.New("watcher event stream closed"))
				}
				return
			}
			if p.relevant(event) {
				arm()
			}
		case err, ok := <-p.watcher.Errors:
			if !ok {
				if !p.stopping() {
					p.stopObservation(errors.New("watcher error stream closed"))
				}
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				arm()
				continue
			}
			if !p.stopping() {
				p.stopObservation(err)
			}
			return
		}
	}
}

func (p *Projection[T]) stopping() bool {
	select {
	case <-p.stop:
		return true
	default:
		return false
	}
}

func (p *Projection[T]) relevant(event fsnotify.Event) bool {
	if filepath.Clean(event.Name) != p.path {
		return false
	}
	const relevant = fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename | fsnotify.Chmod
	return event.Op&relevant != 0
}

func (p *Projection[T]) reconcile() {
	result := p.readAndProject()
	if result.Err != nil {
		p.hasSuccessfulFingerprint = false
		p.publish(result)
		return
	}

	previous := p.latest.Load()
	if previous.Err == nil && p.equal(previous.Value, result.Value) {
		return
	}
	p.publish(result)
}

func (p *Projection[T]) readAndProject() Result[T] {
	data, err := readOrdinaryFile(p.path)
	if err != nil {
		return failure[T](ErrorRead, p.path, err)
	}
	fingerprint := sha256.Sum256(data)
	if p.hasSuccessfulFingerprint && p.successfulFingerprint == fingerprint {
		return p.Current()
	}

	value, err := p.project(data)
	if err != nil {
		return failure[T](ErrorProject, p.path, err)
	}
	p.successfulFingerprint = fingerprint
	p.hasSuccessfulFingerprint = true
	return Result[T]{Value: value}
}

func (p *Projection[T]) stopObservation(err error) {
	p.hasSuccessfulFingerprint = false
	p.publish(failure[T](ErrorObservation, p.path, err))
}

func (p *Projection[T]) publish(result Result[T]) {
	p.store(result)
	p.resultPublisher.Publish(result)
}

func (p *Projection[T]) store(result Result[T]) {
	snapshot := new(Result[T])
	*snapshot = result
	p.latest.Store(snapshot)
}

func readOrdinaryFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("must be an ordinary file")
	}
	return os.ReadFile(path)
}
