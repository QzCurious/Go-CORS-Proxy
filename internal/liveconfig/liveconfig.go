package liveconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const upstreamListFileName = "upstreams.txt"
const defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

type Snapshot struct {
	upstreamListPath            string
	upstreamList                upstreamlist.UpstreamList
	upstreamListEntriesRevision uint64
}

func (s Snapshot) UpstreamList() upstreamlist.UpstreamList {
	return cloneUpstreamList(s.upstreamList)
}

func (s Snapshot) UpstreamListEntriesRevision() uint64 {
	return s.upstreamListEntriesRevision
}

func (s Snapshot) UpstreamListPath() string {
	return s.upstreamListPath
}

type Config struct {
	mu                 sync.RWMutex
	upstreamListPath   string
	current            Snapshot
	hasCurrent         bool
	observationStarted bool
}

// Create bootstraps the fixed Upstream List without reading or validating it.
func Create() (*Config, error) {
	dir, err := homeConfigDir()
	if err != nil {
		return nil, err
	}
	upstreamListPath := filepath.Join(dir, upstreamListFileName)
	if err := bootstrapFile(upstreamListPath, defaultUpstreamList); err != nil {
		return nil, err
	}
	return &Config{
		upstreamListPath: upstreamListPath,
	}, nil
}

// Snapshot returns the newest cached semantic snapshot. The first call reads
// and validates the fixed sources to populate the cache.
func (c *Config) Snapshot() (Snapshot, error) {
	c.mu.RLock()
	if c.hasCurrent {
		current := c.current
		c.mu.RUnlock()
		return current, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasCurrent {
		return c.current, nil
	}
	snapshot, err := c.readFromSourceLocked(false)
	if err != nil {
		return Snapshot{}, err
	}
	c.current = snapshot
	c.hasCurrent = true
	return snapshot, nil
}

// Observe publishes a new snapshot whenever the configured meaning changes.
// Filesystem events are implementation details and are never exposed.
// It blocks until ctx is cancelled or observation fails.
func (c *Config) Observe(ctx context.Context, apply func(Snapshot)) error {
	if apply == nil {
		return fmt.Errorf("Live Configuration Observe requires an apply function")
	}
	if err := c.startObservation(); err != nil {
		return err
	}
	events := make(chan watchEvent, 1)
	go c.observe(ctx, events)
	for event := range events {
		if event.err != nil {
			return event.err
		}
		apply(event.snapshot)
	}
	return nil
}

type refreshResult struct {
	snapshot Snapshot
	changed  bool
}

func (c *Config) refreshObserved() (refreshResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next, err := c.readFromSourceLocked(true)
	if err != nil {
		return refreshResult{}, err
	}
	if c.hasCurrent && sameSemanticSnapshot(c.current, next) {
		return refreshResult{snapshot: c.current}, nil
	}
	c.current = next
	c.hasCurrent = true
	return refreshResult{snapshot: next, changed: true}, nil
}

func (c *Config) readFromSourceLocked(observed bool) (Snapshot, error) {
	decoded, err := loadUpstreamList(c.upstreamListPath)
	if err != nil {
		if observed {
			return Snapshot{}, invalidUpstreamError(c.upstreamListPath, err)
		}
		return Snapshot{}, err
	}

	revision := uint64(1)
	if c.hasCurrent {
		revision = c.current.upstreamListEntriesRevision
	}
	next := snapshotFromUpstreamList(c.upstreamListPath, decoded, revision)
	if c.hasCurrent && !upstreamlist.SameEntries(c.current.upstreamList, next.upstreamList) {
		next.upstreamListEntriesRevision++
	}
	return next, nil
}

func sameSemanticSnapshot(left, right Snapshot) bool {
	if left.upstreamListPath != right.upstreamListPath ||
		left.upstreamListEntriesRevision != right.upstreamListEntriesRevision ||
		!slices.Equal(left.upstreamList.Warnings(), right.upstreamList.Warnings()) {
		return false
	}
	return true
}

func snapshotFromUpstreamList(path string, decoded upstreamlist.UpstreamList, upstreamListEntriesRevision uint64) Snapshot {
	return Snapshot{
		upstreamListPath:            path,
		upstreamList:                cloneUpstreamList(decoded),
		upstreamListEntriesRevision: upstreamListEntriesRevision,
	}
}

func cloneUpstreamList(source upstreamlist.UpstreamList) upstreamlist.UpstreamList {
	entries := source.Entries()
	return upstreamlist.NewUpstreamList(
		upstreamlist.NewEntries(entries.HostSelectors(), entries.OriginSelectors()),
		source.Warnings(),
	)
}

func homeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors"), nil
}

func loadUpstreamList(path string) (upstreamlist.UpstreamList, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return upstreamlist.UpstreamList{}, err
	}
	decoded, err := upstreamlist.Decode(data)
	if err != nil {
		return upstreamlist.UpstreamList{}, err
	}
	return decoded, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be an ordinary file", path)
	}
	return os.ReadFile(path)
}

func bootstrapFile(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
