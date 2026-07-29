package liveconfig

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const changeDebounce = 100 * time.Millisecond
const invalidConfirmation = time.Second

type targetState struct {
	generation uint64
	timer      *time.Timer
}

type settledTarget struct {
	path       string
	generation uint64
}

type invalidSourceError struct {
	path string
	err  error
}

func (e *invalidSourceError) Error() string {
	return e.err.Error()
}

func (e *invalidSourceError) Unwrap() error {
	return e.err
}

type watcherState struct {
	config                 *Config
	watcher                *fsnotify.Watcher
	targets                map[string]*targetState
	watchedDirs            map[string]struct{}
	settled                chan settledTarget
	confirmed              chan settledTarget
	done                   chan struct{}
	confirmations          map[string]*confirmationState
	confirmationGeneration uint64
}

type confirmationState struct {
	generation uint64
	timer      *time.Timer
	matured    bool
}

type watchEvent struct {
	snapshot Snapshot
	err      error
}

func (c *Config) startObservation() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observationStarted {
		return errors.New("Live Configuration Observe may only be called once")
	}
	c.observationStarted = true
	return nil
}

func (c *Config) observe(ctx context.Context, output chan watchEvent) {
	defer close(output)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		publishLatest(output, watchEvent{err: fmt.Errorf("Live Configuration observation failed: %w", err)})
		return
	}
	defer watcher.Close()

	state := &watcherState{
		config:        c,
		watcher:       watcher,
		targets:       make(map[string]*targetState),
		watchedDirs:   make(map[string]struct{}),
		settled:       make(chan settledTarget, 4),
		confirmed:     make(chan settledTarget, 4),
		done:          make(chan struct{}),
		confirmations: make(map[string]*confirmationState),
	}
	defer state.close()
	if err := state.addTarget(c.configPath); err != nil {
		publishLatest(output, watchEvent{err: err})
		return
	}
	if err := state.addTarget(c.upstreamListPath); err != nil {
		publishLatest(output, watchEvent{err: err})
		return
	}
	if state.reconcileAndPublish(output) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				publishLatest(output, watchEvent{err: errors.New("Live Configuration observation stopped unexpectedly")})
				return
			}
			state.handleFilesystemEvent(event)
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				publishLatest(output, watchEvent{err: errors.New("Live Configuration observation stopped unexpectedly")})
				return
			}
			if errors.Is(watchErr, fsnotify.ErrEventOverflow) {
				state.scheduleAll()
				continue
			}
			publishLatest(output, watchEvent{err: fmt.Errorf("Live Configuration observation failed: %w", watchErr)})
			return
		case settled := <-state.settled:
			target, ok := state.targets[settled.path]
			if !ok || target.generation != settled.generation {
				continue
			}
			target.timer = nil
			if state.reconcileAndPublish(output) {
				return
			}
		case confirmed := <-state.confirmed:
			confirmation, ok := state.confirmations[confirmed.path]
			if !ok || confirmation.generation != confirmed.generation {
				continue
			}
			confirmation.timer = nil
			confirmation.matured = true
			if state.reconcileAndPublish(output) {
				return
			}
		}
	}
}

func (w *watcherState) reconcileAndPublish(output chan watchEvent) bool {
	event, changed, err := w.reconcile()
	if err != nil {
		return w.confirmOrPublish(output, err)
	}
	w.cancelAllConfirmations()
	if changed {
		publishLatest(output, event)
	}
	return false
}

func (w *watcherState) handleFilesystemEvent(event fsnotify.Event) {
	path := filepath.Clean(event.Name)
	target, ok := w.targets[path]
	if !ok {
		return
	}
	w.cancelConfirmation(path)
	w.scheduleTarget(path, target)
}

func (w *watcherState) scheduleAll() {
	for path, target := range w.targets {
		w.cancelConfirmation(path)
		w.scheduleTarget(path, target)
	}
}

func (w *watcherState) scheduleTarget(path string, target *targetState) {
	target.generation++
	if target.timer != nil {
		target.timer.Stop()
	}
	generation := target.generation
	target.timer = time.AfterFunc(changeDebounce, func() {
		select {
		case w.settled <- settledTarget{path: path, generation: generation}:
		case <-w.done:
		}
	})
}

func (w *watcherState) close() {
	close(w.done)
	for _, target := range w.targets {
		if target.timer != nil {
			target.timer.Stop()
		}
	}
	w.cancelAllConfirmations()
}

func (w *watcherState) confirmOrPublish(output chan watchEvent, err error) bool {
	var invalid *invalidSourceError
	if !errors.As(err, &invalid) {
		publishLatest(output, watchEvent{err: err})
		return true
	}
	if confirmation, ok := w.confirmations[invalid.path]; ok {
		if confirmation.matured {
			publishLatest(output, watchEvent{err: err})
			return true
		}
		return false
	}
	w.confirmationGeneration++
	generation := w.confirmationGeneration
	confirmation := &confirmationState{generation: generation}
	confirmation.timer = time.AfterFunc(invalidConfirmation, func() {
		select {
		case w.confirmed <- settledTarget{path: invalid.path, generation: generation}:
		case <-w.done:
		}
	})
	w.confirmations[invalid.path] = confirmation
	return false
}

func (w *watcherState) cancelConfirmation(path string) {
	confirmation, ok := w.confirmations[path]
	if !ok {
		return
	}
	if confirmation.timer != nil {
		confirmation.timer.Stop()
	}
	delete(w.confirmations, path)
}

func (w *watcherState) cancelAllConfirmations() {
	for path := range w.confirmations {
		w.cancelConfirmation(path)
	}
}

func (w *watcherState) reconcile() (watchEvent, bool, error) {
	result, err := w.config.refreshObserved()
	if err != nil {
		return watchEvent{}, false, err
	}
	if !result.changed {
		return watchEvent{}, false, nil
	}
	return watchEvent{snapshot: result.snapshot}, true, nil
}

func (w *watcherState) addTarget(path string) error {
	path = filepath.Clean(path)
	if _, ok := w.targets[path]; ok {
		return nil
	}
	dir := filepath.Dir(path)
	if _, ok := w.watchedDirs[dir]; !ok {
		if err := w.watcher.Add(dir); err != nil {
			return fmt.Errorf("Live Configuration cannot observe %s: %w", dir, err)
		}
		w.watchedDirs[dir] = struct{}{}
	}
	w.targets[path] = &targetState{}
	return nil
}

func invalidConfigError(path string, err error) error {
	return &invalidSourceError{path: path, err: fmt.Errorf("Fatal Config Error: %w", err)}
}

func invalidUpstreamError(path string, err error) error {
	return &invalidSourceError{path: path, err: fmt.Errorf("Fatal Upstream List Error: %w", err)}
}

func publishLatest(output chan watchEvent, event watchEvent) {
	select {
	case output <- event:
		return
	default:
	}
	select {
	case <-output:
	default:
	}
	output <- event
}
