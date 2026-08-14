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

func (w *fakeEventWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *fakeEventWatcher) Errors() <-chan error          { return w.errs }
func (w *fakeEventWatcher) Close() error                  { return nil }

func TestWatcherFailureRebuildsAndRereadsCurrentContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := newFakeEventWatcher(), newFakeEventWatcher()
	watchers := []eventWatcher{first, second}
	observation, err := open(path, Options{Debounce: time.Millisecond}, func(string) (eventWatcher, error) {
		watcher := watchers[0]
		watchers = watchers[1:]
		return watcher, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	assertContentsResult(t, waitInternalResult(t, observation.Results()), "value")

	first.errs <- errors.New("watcher uncertain")
	assertContentsResult(t, waitInternalResult(t, observation.Results()), "value")
}

func TestWatcherRecoveryReportsReadErrorAndContinues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := newFakeEventWatcher(), newFakeEventWatcher()
	watchers := []eventWatcher{first, second}
	observation, err := open(path, Options{Debounce: time.Millisecond}, func(string) (eventWatcher, error) {
		watcher := watchers[0]
		watchers = watchers[1:]
		return watcher, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	waitInternalResult(t, observation.Results())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	first.errs <- errors.New("watcher uncertain")
	result := waitInternalResult(t, observation.Results())
	var readErr *ReadError
	if !errors.As(result.Err, &readErr) {
		t.Fatalf("recovery result = %#v", result)
	}
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	second.events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
	assertContentsResult(t, waitInternalResult(t, observation.Results()), "after")
}

func TestWatcherRebuildFailureStopsObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := newFakeEventWatcher()
	calls := 0
	observation, err := open(path, Options{Debounce: time.Millisecond}, func(string) (eventWatcher, error) {
		calls++
		if calls == 1 {
			return first, nil
		}
		return nil, errors.New("cannot rebuild watcher")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	waitInternalResult(t, observation.Results())

	first.errs <- errors.New("watcher uncertain")
	result := waitInternalResult(t, observation.Results())
	var stoppedErr *ObservationStoppedError
	if !errors.As(result.Err, &stoppedErr) {
		t.Fatalf("recovery result = %#v", result)
	}
}

func waitInternalResult(t *testing.T, results <-chan Result) Result {
	t.Helper()
	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("results closed")
		}
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observation result")
		return Result{}
	}
}

func assertContentsResult(t *testing.T, result Result, want string) {
	t.Helper()
	if result.Err != nil || !result.Snapshot || string(result.Contents) != want {
		t.Fatalf("result = %#v, want contents %q", result, want)
	}
}
