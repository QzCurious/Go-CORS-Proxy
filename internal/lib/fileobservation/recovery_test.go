package fileobservation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type fakeEventWatcher struct {
	events chan fsnotify.Event
	errs   chan error
}

func newFakeEventWatcher() *fakeEventWatcher {
	return &fakeEventWatcher{events: make(chan fsnotify.Event, 1), errs: make(chan error, 1)}
}

func (w *fakeEventWatcher) observationWatcher() eventWatcher {
	return eventWatcher{events: w.events, errs: w.errs, close: func() {}}
}

func TestWatcherFailureRebuildsAndRereadsCurrentContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := newFakeEventWatcher(), newFakeEventWatcher()
	watchers := []eventWatcher{first.observationWatcher(), second.observationWatcher()}
	observation := open(path, time.Millisecond, func(string) (eventWatcher, error) {
		watcher := watchers[0]
		watchers = watchers[1:]
		return watcher, nil
	})
	defer observation.Close()
	assertContentsOutcome(t, waitInternalOutcome(t, observation.Outcomes()), "value")

	first.errs <- errors.New("watcher uncertain")
	assertContentsOutcome(t, waitInternalOutcome(t, observation.Outcomes()), "value")
}

func TestWatcherRecoveryReportsReadErrorAndContinues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := newFakeEventWatcher(), newFakeEventWatcher()
	watchers := []eventWatcher{first.observationWatcher(), second.observationWatcher()}
	observation := open(path, time.Millisecond, func(string) (eventWatcher, error) {
		watcher := watchers[0]
		watchers = watchers[1:]
		return watcher, nil
	})
	defer observation.Close()
	waitInternalOutcome(t, observation.Outcomes())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	first.errs <- errors.New("watcher uncertain")
	outcome := waitInternalOutcome(t, observation.Outcomes())
	readErr, ok := outcome.(ReadError)
	if !ok {
		t.Fatalf("recovery outcome = %#v", outcome)
	}
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("recovery cause = %v", readErr)
	}
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	second.events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
	assertContentsOutcome(t, waitInternalOutcome(t, observation.Outcomes()), "after")
}

func TestWatcherRebuildFailureStopsObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := newFakeEventWatcher()
	calls := 0
	observation := open(path, time.Millisecond, func(string) (eventWatcher, error) {
		calls++
		if calls == 1 {
			return first.observationWatcher(), nil
		}
		return eventWatcher{}, errors.New("cannot rebuild watcher")
	})
	defer observation.Close()
	waitInternalOutcome(t, observation.Outcomes())

	first.errs <- errors.New("watcher uncertain")
	outcome := waitInternalOutcome(t, observation.Outcomes())
	if _, ok := outcome.(ObservationStoppedError); !ok {
		t.Fatalf("recovery outcome = %#v", outcome)
	}
}

func waitInternalOutcome(t *testing.T, outcomes <-chan Outcome) Outcome {
	t.Helper()
	select {
	case outcome, ok := <-outcomes:
		if !ok {
			t.Fatal("outcomes closed")
		}
		return outcome
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observation outcome")
		return nil
	}
}

func assertContentsOutcome(t *testing.T, outcome Outcome, want string) {
	t.Helper()
	contents, ok := outcome.(Contents)
	if !ok || string(contents) != want {
		t.Fatalf("outcome = %#v, want contents %q", outcome, want)
	}
}
