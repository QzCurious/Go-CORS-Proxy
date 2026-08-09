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

type ErrorKind uint8

const (
	ErrorRead ErrorKind = iota + 1
	ErrorObservationUncertain
	ErrorObservationStopped
)

type Error struct {
	Kind ErrorKind
	Path string
	Err  error
}

func (e *Error) Error() string {
	operation := "observe"
	if e.Kind == ErrorRead {
		operation = "read"
	}
	return fmt.Sprintf("file observation cannot %s %q: %v", operation, e.Path, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

type Options struct{ Debounce time.Duration }

const defaultDebounce = 100 * time.Millisecond

type Observation struct {
	path           string
	debounce       time.Duration
	watcher        *fsnotify.Watcher
	results        chan Result
	stop           chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
	closeErr       error
	hasFingerprint bool
	fingerprint    [sha256.Size]byte
}

func Open(path string, options Options) (*Observation, error) {
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
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, stopped(absolute, err)
	}
	if err := watcher.Add(filepath.Dir(absolute)); err != nil {
		_ = watcher.Close()
		return nil, stopped(absolute, err)
	}
	o := &Observation{path: absolute, debounce: options.Debounce, watcher: watcher, results: make(chan Result), stop: make(chan struct{}), done: make(chan struct{})}
	go o.observe()
	return o, nil
}

func (o *Observation) Results() <-chan Result { return o.results }

func (o *Observation) Close() error {
	o.closeOnce.Do(func() {
		close(o.stop)
		o.closeErr = o.watcher.Close()
		<-o.done
	})
	return o.closeErr
}

func (o *Observation) observe() {
	defer close(o.done)
	defer close(o.results)
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
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	events, errs := o.watcher.Events, o.watcher.Errors
	for events != nil || errs != nil {
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
				events = nil
				if !o.stopping() {
					o.publish(Result{Err: stopped(o.path, errors.New("filesystem event channel closed"))})
				}
				return
			}
			if o.relevant(event) {
				arm()
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				if !o.stopping() {
					o.publish(Result{Err: stopped(o.path, errors.New("filesystem error channel closed"))})
				}
				return
			}
			o.hasFingerprint = false
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				arm()
				continue
			}
			if !o.publish(Result{Err: &Error{Kind: ErrorObservationUncertain, Path: o.path, Err: err}}) {
				return
			}
		}
	}
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
		return Result{Err: &Error{Kind: ErrorRead, Path: o.path, Err: err}}, true
	}
	if !info.Mode().IsRegular() {
		o.hasFingerprint = false
		return Result{Err: &Error{Kind: ErrorRead, Path: o.path, Err: errors.New("must be an ordinary file")}}, true
	}
	contents, err := os.ReadFile(o.path)
	if err != nil {
		o.hasFingerprint = false
		return Result{Err: &Error{Kind: ErrorRead, Path: o.path, Err: err}}, true
	}
	fingerprint := sha256.Sum256(contents)
	if o.hasFingerprint && o.fingerprint == fingerprint {
		return Result{}, false
	}
	o.hasFingerprint, o.fingerprint = true, fingerprint
	return Result{Contents: contents, Snapshot: true}, true
}

func stopped(path string, err error) error {
	return &Error{Kind: ErrorObservationStopped, Path: path, Err: err}
}
