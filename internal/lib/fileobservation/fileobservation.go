// Package fileobservation continuously observes the complete contents of one
// ordinary file. It owns filesystem mechanics only; callers interpret bytes.
package fileobservation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Result struct {
	Contents []byte
	Err      error
	Snapshot bool
}

type ReadError struct {
	Path  string
	Cause error
}

func (e *ReadError) Error() string {
	return fmt.Sprintf("file observation cannot read %q: %v", e.Path, e.Cause)
}

func (e *ReadError) Unwrap() error { return e.Cause }

type ObservationStoppedError struct {
	Path  string
	Cause error
}

func (e *ObservationStoppedError) Error() string {
	return fmt.Sprintf("file observation stopped for %q: %v", e.Path, e.Cause)
}

func (e *ObservationStoppedError) Unwrap() error { return e.Cause }

type Options struct{ Debounce time.Duration }

const defaultDebounce = 100 * time.Millisecond

type eventWatcher interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Close() error
}

type fsnotifyEventWatcher struct{ watcher *fsnotify.Watcher }

func (w fsnotifyEventWatcher) Events() <-chan fsnotify.Event { return w.watcher.Events }
func (w fsnotifyEventWatcher) Errors() <-chan error          { return w.watcher.Errors }
func (w fsnotifyEventWatcher) Close() error                  { return w.watcher.Close() }

type watcherFactory func(string) (eventWatcher, error)

type Observation struct {
	path           string
	debounce       time.Duration
	watcher        eventWatcher
	openWatcher    watcherFactory
	results        chan Result
	stop           chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
	closeErr       error
	hasFingerprint bool
	fingerprint    [sha256.Size]byte
}

func Open(path string, options Options) (*Observation, error) {
	return open(path, options, openFSNotifyWatcher)
}

func open(path string, options Options, openWatcher watcherFactory) (*Observation, error) {
	if options.Debounce < 0 {
		return nil, errors.New("file observation debounce must not be negative")
	}
	if options.Debounce == 0 {
		options.Debounce = defaultDebounce
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, stopped(path, err)
	}
	absolute = filepath.Clean(absolute)
	watcher, err := openWatcher(filepath.Dir(absolute))
	if err != nil {
		return nil, stopped(absolute, err)
	}
	o := &Observation{path: absolute, debounce: options.Debounce, watcher: watcher, openWatcher: openWatcher, results: make(chan Result), stop: make(chan struct{}), done: make(chan struct{})}
	go o.observe()
	return o, nil
}

func openFSNotifyWatcher(directory string) (eventWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(directory); err != nil {
		_ = watcher.Close()
		return nil, err
	}
	return fsnotifyEventWatcher{watcher: watcher}, nil
}

func (o *Observation) Results() <-chan Result { return o.results }

func (o *Observation) Close() error {
	o.closeOnce.Do(func() {
		close(o.stop)
		<-o.done
	})
	return o.closeErr
}

func (o *Observation) observe() {
	defer close(o.done)
	defer close(o.results)
	defer func() { o.closeErr = o.watcher.Close() }()
	if result, changed := o.read(); changed && !o.publish(result) {
		return
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(o.debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(o.debounce)
		}
		timerC = timer.C
	}
	disarm := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	events, errs := o.watcher.Events(), o.watcher.Errors()
	recoverObservation := func(cause error) bool {
		disarm()
		if err := o.rebuildWatcher(cause); err != nil {
			o.publish(Result{Err: stopped(o.path, err)})
			return false
		}
		events, errs = o.watcher.Events(), o.watcher.Errors()
		if result, changed := o.read(); changed {
			return o.publish(result)
		}
		return true
	}
	for {
		select {
		case <-o.stop:
			return
		case <-timerC:
			timerC = nil
			if result, changed := o.read(); changed && !o.publish(result) {
				return
			}
		case event, ok := <-events:
			if !ok {
				if o.stopping() || !recoverObservation(errors.New("filesystem event channel closed")) {
					return
				}
				continue
			}
			if o.relevant(event) {
				arm()
			}
		case err, ok := <-errs:
			if !ok {
				if o.stopping() || !recoverObservation(errors.New("filesystem error channel closed")) {
					return
				}
				continue
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				o.hasFingerprint = false
				arm()
				continue
			}
			if !recoverObservation(err) {
				return
			}
		}
	}
}

func (o *Observation) rebuildWatcher(cause error) error {
	_ = o.watcher.Close()
	next, err := o.openWatcher(filepath.Dir(o.path))
	if err != nil {
		return fmt.Errorf("rebuild after watcher failure %v: %w", cause, err)
	}
	o.watcher = next
	o.hasFingerprint = false
	return nil
}

func (o *Observation) stopping() bool {
	select {
	case <-o.stop:
		return true
	default:
		return false
	}
}

func (o *Observation) publish(result Result) bool {
	select {
	case o.results <- result:
		return true
	case <-o.stop:
		return false
	}
}

func (o *Observation) relevant(event fsnotify.Event) bool {
	if filepath.Clean(event.Name) != o.path {
		return false
	}
	return event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0
}

func (o *Observation) read() (Result, bool) {
	info, err := os.Lstat(o.path)
	if err != nil {
		o.hasFingerprint = false
		return Result{Err: &ReadError{Path: o.path, Cause: err}}, true
	}
	if !info.Mode().IsRegular() {
		o.hasFingerprint = false
		return Result{Err: &ReadError{Path: o.path, Cause: errors.New("must be an ordinary file")}}, true
	}
	contents, err := os.ReadFile(o.path)
	if err != nil {
		o.hasFingerprint = false
		return Result{Err: &ReadError{Path: o.path, Cause: err}}, true
	}
	fingerprint := sha256.Sum256(contents)
	if o.hasFingerprint && o.fingerprint == fingerprint {
		return Result{}, false
	}
	o.hasFingerprint, o.fingerprint = true, fingerprint
	return Result{Contents: contents, Snapshot: true}, true
}

func stopped(path string, err error) error {
	return &ObservationStoppedError{Path: path, Cause: err}
}
