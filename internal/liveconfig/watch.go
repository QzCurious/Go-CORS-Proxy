package liveconfig

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"seamless-cors/internal/domain"

	"github.com/fsnotify/fsnotify"
)

const changeDebounce = 100 * time.Millisecond
const invalidConfirmation = time.Second

type targetRole uint8

const (
	configTarget targetRole = iota
	domainTarget
	candidateDomainTarget
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

func (s *Source) watch(ctx context.Context, output chan Event) {
	defer close(output)
	if err := s.startWatch(); err != nil {
		publishLatest(output, Event{Err: err})
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		publishLatest(output, Event{Err: fmt.Errorf("Live Configuration observation failed: %w", err)})
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
	current := s.Config()
	if err := state.addTarget(current.ConfigPath(), configTarget); err != nil {
		publishLatest(output, Event{Err: err})
		return
	}
	if err := state.addTarget(current.DomainListPath(), domainTarget); err != nil {
		publishLatest(output, Event{Err: err})
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
				publishLatest(output, Event{Err: errors.New("Live Configuration observation stopped unexpectedly")})
				return
			}
			state.handleFilesystemEvent(event)
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				publishLatest(output, Event{Err: errors.New("Live Configuration observation stopped unexpectedly")})
				return
			}
			if errors.Is(watchErr, fsnotify.ErrEventOverflow) {
				state.scheduleAll()
				continue
			}
			publishLatest(output, Event{Err: fmt.Errorf("Live Configuration observation failed: %w", watchErr)})
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

func (w *watcherState) reconcileAndPublish(output chan Event) bool {
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

func (s *Source) startWatch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchStarted {
		return errors.New("Live Configuration Watch may only be called once")
	}
	s.watchStarted = true
	return nil
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

func (w *watcherState) confirmOrPublish(output chan Event, err error) bool {
	var invalid *invalidSourceError
	if !errors.As(err, &invalid) {
		publishLatest(output, Event{Err: err})
		return true
	}
	if confirmation, ok := w.confirmations[invalid.path]; ok {
		if confirmation.matured {
			publishLatest(output, Event{Err: err})
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

func (w *watcherState) reconcile() (Event, bool, error) {
	current, desired, configFingerprint, domainFingerprint := w.source.snapshot()
	configData, err := readRegularFile(current.ConfigPath())
	if err != nil {
		return Event{}, false, invalidConfigError(current.ConfigPath(), err)
	}
	nextConfigFingerprint := sha256.Sum256(configData)
	configChanged := nextConfigFingerprint != configFingerprint
	loaded := loadResult{
		Config:     desired,
		ConfigPath: current.ConfigPath(),
		DomainPath: current.DomainListPath(),
	}
	if configChanged {
		loaded, err = parseFileConfig(current.ConfigPath(), configData)
		if err != nil {
			return Event{}, false, invalidConfigError(current.ConfigPath(), err)
		}
	}
	if err := w.setCandidateTarget(loaded.DomainPath); err != nil {
		return Event{}, false, err
	}
	domainData, err := readRegularFile(loaded.DomainPath)
	if err != nil {
		return Event{}, false, invalidDomainError(loaded.DomainPath, err)
	}
	nextDomainFingerprint := sha256.Sum256(domainData)
	domainChanged := nextDomainFingerprint != domainFingerprint || loaded.DomainPath != current.DomainListPath()
	if !configChanged && !domainChanged {
		if err := w.commitDomainTarget(current.DomainListPath()); err != nil {
			return Event{}, false, err
		}
		return Event{}, false, nil
	}
	entries := current.Entries()
	if domainChanged {
		entries, err = parseDomainList(domainData)
		if err != nil {
			return Event{}, false, invalidDomainError(loaded.DomainPath, err)
		}
	}
	next := configFromLoadResult(
		loaded,
		entries,
		lifecycleChanges(loaded.Config.CATrusted, w.source.baselineCATrusted),
		w.source.baselineCATrusted,
	)
	if err := w.commitDomainTarget(loaded.DomainPath); err != nil {
		return Event{}, false, err
	}
	semanticChanged := w.source.commit(next, loaded.Config, nextConfigFingerprint, nextDomainFingerprint)
	if !semanticChanged {
		return Event{}, false, nil
	}
	return Event{Config: next}, true, nil
}

func (s *Source) snapshot() (Config, fileConfig, [sha256.Size]byte, [sha256.Size]byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config, s.desiredConfig, s.configFingerprint, s.domainFingerprint
}

func (s *Source) commit(next Config, desired fileConfig, configFingerprint, domainFingerprint [sha256.Size]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	semanticChanged := !sameSemanticConfig(s.config, next)
	if semanticChanged {
		s.config = next
	}
	s.desiredConfig = desired
	s.configFingerprint = configFingerprint
	s.domainFingerprint = domainFingerprint
	return semanticChanged
}

func (w *watcherState) addTarget(path string, role targetRole) error {
	path = filepath.Clean(path)
	if target, ok := w.targets[path]; ok {
		if role != candidateDomainTarget {
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
		if target.role != candidateDomainTarget || targetPath == path {
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
	return w.addTarget(path, candidateDomainTarget)
}

func (w *watcherState) commitDomainTarget(path string) error {
	path = filepath.Clean(path)
	for targetPath, target := range w.targets {
		switch {
		case targetPath == w.source.Config().ConfigPath():
			target.role = configTarget
		case targetPath == path:
			target.role = domainTarget
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

func invalidDomainError(path string, err error) error {
	return &invalidSourceError{path: path, err: fmt.Errorf("Fatal Domain List Error: %w", err)}
}

func publishLatest(output chan Event, event Event) {
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

func parseDomainList(data []byte) ([]domain.Entry, error) {
	entries, errs := domain.ParseList(string(data))
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Domain List:\n%s", formatDomainErrors(errs))
	}
	return entries, nil
}
