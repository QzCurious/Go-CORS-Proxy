// Package fileobservation continuously observes the complete contents of one
// ordinary file. It owns filesystem mechanics only; callers interpret bytes.
package fileobservation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Observation struct {
	path        string
	debounce    time.Duration
	openWatcher watcherFactory
	outcomes    chan Outcome
	stop        chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
}

func Open(path string) *Observation {
	return open(path, defaultDebounce, openFSNotifyWatcher)
}

func (o *Observation) Outcomes() <-chan Outcome { return o.outcomes }

func (o *Observation) Close() {
	o.closeOnce.Do(func() {
		close(o.stop)
		<-o.done
	})
}

// Outcome is one complete fact produced by an Observation. Observations publish
// exactly Contents, ReadError, or ObservationStoppedError. Outcomes must be
// inspected with a type switch and must not be compared directly.
type Outcome interface {
	outcome()
}

// Contents is the complete contents observed during one successful read.
type Contents []byte

func (Contents) outcome() {}

type ReadError struct {
	Path  string
	Cause error
}

func (e ReadError) Error() string {
	return fmt.Sprintf("file observation cannot read %q: %v", e.Path, e.Cause)
}

func (e ReadError) Unwrap() error { return e.Cause }

func (ReadError) outcome() {}

type ObservationStoppedError struct {
	Path  string
	Cause error
}

func (e ObservationStoppedError) Error() string {
	return fmt.Sprintf("file observation stopped for %q: %v", e.Path, e.Cause)
}

func (e ObservationStoppedError) Unwrap() error { return e.Cause }

func (ObservationStoppedError) outcome() {}

const defaultDebounce = 100 * time.Millisecond

type eventWatcher struct {
	events <-chan fsnotify.Event
	errs   <-chan error
	close  func()
}

type watcherFactory func(string) (eventWatcher, error)

func open(path string, debounce time.Duration, openWatcher watcherFactory) *Observation {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	o := &Observation{path: path, debounce: debounce, openWatcher: openWatcher, outcomes: make(chan Outcome), stop: make(chan struct{}), done: make(chan struct{})}
	go o.observe(err)
	return o
}

func openFSNotifyWatcher(directory string) (eventWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return eventWatcher{}, err
	}
	if err := watcher.Add(directory); err != nil {
		_ = watcher.Close()
		return eventWatcher{}, err
	}
	return eventWatcher{
		events: watcher.Events,
		errs:   watcher.Errors,
		close:  func() { _ = watcher.Close() },
	}, nil
}

func (o *Observation) observe(openErr error) {
	defer close(o.done)
	defer close(o.outcomes)
	if openErr != nil {
		o.publish(stopped(o.path, openErr))
		return
	}
	watcher, err := o.openWatcher(filepath.Dir(o.path))
	if err != nil {
		o.publish(stopped(o.path, err))
		return
	}
	defer func() { watcher.close() }()
	if !o.publish(o.read()) {
		return
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(o.debounce)
		} else {
			timer.Reset(o.debounce)
		}
		timerC = timer.C
	}
	disarm := func() {
		if timer != nil {
			timer.Stop()
		}
		timerC = nil
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	events, errs := watcher.events, watcher.errs
	recoverObservation := func(cause error) bool {
		disarm()
		next, err := o.openWatcher(filepath.Dir(o.path))
		if err != nil {
			o.publish(stopped(o.path, fmt.Errorf("rebuild after watcher failure %v: %w", cause, err)))
			return false
		}
		previous := watcher
		watcher = next
		events, errs = watcher.events, watcher.errs
		previous.close()
		return o.publish(o.read())
	}
	for {
		select {
		case <-o.stop:
			return
		case <-timerC:
			timerC = nil
			if !o.publish(o.read()) {
				return
			}
		case event, ok := <-events:
			if !ok {
				if !recoverObservation(errors.New("filesystem event channel closed")) {
					return
				}
				continue
			}
			if o.parentLost(event) {
				if !recoverObservation(errors.New("watched directory removed or renamed")) {
					return
				}
				continue
			}
			if o.relevant(event) {
				arm()
			}
		case err, ok := <-errs:
			if !ok {
				if !recoverObservation(errors.New("filesystem error channel closed")) {
					return
				}
				continue
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				arm()
				continue
			}
			if !recoverObservation(err) {
				return
			}
		}
	}
}

func (o *Observation) publish(outcome Outcome) bool {
	select {
	case o.outcomes <- outcome:
		return true
	case <-o.stop:
		return false
	}
}

func (o *Observation) parentLost(event fsnotify.Event) bool {
	return filepath.Clean(event.Name) == filepath.Dir(o.path) && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0
}

func (o *Observation) relevant(event fsnotify.Event) bool {
	name := filepath.Clean(event.Name)
	if name == filepath.Dir(o.path) {
		return event.Op&fsnotify.Chmod != 0
	}
	return name == o.path && event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0
}

// Debouncing handles the common source of duplicate reads, making raw-byte
// deduplication unnecessary.
func (o *Observation) read() Outcome {
	info, err := os.Lstat(o.path)
	if err != nil {
		return ReadError{Path: o.path, Cause: err}
	}
	if !info.Mode().IsRegular() {
		return ReadError{Path: o.path, Cause: errors.New("must be an ordinary file")}
	}
	contents, err := os.ReadFile(o.path)
	if err != nil {
		return ReadError{Path: o.path, Cause: err}
	}
	return Contents(contents)
}

func stopped(path string, err error) ObservationStoppedError {
	return ObservationStoppedError{Path: path, Cause: err}
}
