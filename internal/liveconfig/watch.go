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

type targetRole uint8

const (
	configTarget targetRole = iota
	upstreamTarget
	candidateUpstreamTarget
)

type targetState struct {
	role       targetRole
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
	source                 *Source
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

func (s *Source) startObservation() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observationStarted {
		return errors.New("Live Configuration Observe may only be called once")
	}
	s.observationStarted = true
	return nil
}

func (s *Source) observe(ctx context.Context, output chan watchEvent) {
	defer close(output)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		publishLatest(output, watchEvent{err: fmt.Errorf("Live Configuration observation failed: %w", err)})
		return
	}
	defer watcher.Close()

	state := &watcherState{
		source:        s,
		watcher:       watcher,
		targets:       make(map[string]*targetState),
		watchedDirs:   make(map[string]struct{}),
		settled:       make(chan settledTarget, 4),
		confirmed:     make(chan settledTarget, 4),
		done:          make(chan struct{}),
		confirmations: make(map[string]*confirmationState),
	}
	defer state.close()
	current := s.Current()
	if err := state.addTarget(current.ConfigPath(), configTarget); err != nil {
		publishLatest(output, watchEvent{err: err})
		return
	}
	if err := state.addTarget(current.UpstreamListPath(), upstreamTarget); err != nil {
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
	result, err := w.source.refreshObserved()
	if result.upstreamPath != "" {
		if targetErr := w.setCandidateTarget(result.upstreamPath); targetErr != nil {
			return watchEvent{}, false, invalidConfigError(w.source.Current().ConfigPath(), targetErr)
		}
	}
	if err != nil {
		return watchEvent{}, false, err
	}
	if err := w.commitUpstreamTarget(result.snapshot.UpstreamListPath()); err != nil {
		return watchEvent{}, false, err
	}
	if !result.changed {
		return watchEvent{}, false, nil
	}
	return watchEvent{snapshot: result.snapshot}, true, nil
}

func (w *watcherState) addTarget(path string, role targetRole) error {
	path = filepath.Clean(path)
	if target, ok := w.targets[path]; ok {
		if role != candidateUpstreamTarget {
			target.role = role
		}
		return nil
	}
	dir := filepath.Dir(path)
	if _, ok := w.watchedDirs[dir]; !ok {
		if err := w.watcher.Add(dir); err != nil {
			return fmt.Errorf("Live Configuration cannot observe %s: %w", dir, err)
		}
		w.watchedDirs[dir] = struct{}{}
	}
	w.targets[path] = &targetState{role: role}
	return nil
}

func (w *watcherState) setCandidateTarget(path string) error {
	path = filepath.Clean(path)
	for targetPath, target := range w.targets {
		if target.role != candidateUpstreamTarget || targetPath == path {
			continue
		}
		if target.timer != nil {
			target.timer.Stop()
		}
		delete(w.targets, targetPath)
	}
	if err := w.removeUnusedDirectories(); err != nil {
		return err
	}
	return w.addTarget(path, candidateUpstreamTarget)
}

func (w *watcherState) commitUpstreamTarget(path string) error {
	path = filepath.Clean(path)
	for targetPath, target := range w.targets {
		switch {
		case targetPath == w.source.Current().ConfigPath():
			target.role = configTarget
		case targetPath == path:
			target.role = upstreamTarget
		default:
			if target.timer != nil {
				target.timer.Stop()
			}
			delete(w.targets, targetPath)
		}
	}
	return w.removeUnusedDirectories()
}

func (w *watcherState) removeUnusedDirectories() error {
	for dir := range w.watchedDirs {
		if w.directoryHasTarget(dir) {
			continue
		}
		if err := w.watcher.Remove(dir); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			return fmt.Errorf("Live Configuration cannot stop observing %s: %w", dir, err)
		}
		delete(w.watchedDirs, dir)
	}
	return nil
}

func (w *watcherState) directoryHasTarget(dir string) bool {
	for path := range w.targets {
		if filepath.Dir(path) == dir {
			return true
		}
	}
	return false
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
