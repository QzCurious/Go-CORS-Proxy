package liveconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"

	"gopkg.in/yaml.v3"
)

const configFileName = "config.yaml"
const upstreamListFileName = "upstreams.txt"
const defaultUpstreamList = "# One upstream host or origin per line.\n# api.dev.example.com\n"

type Snapshot struct {
	caTrusted                   bool
	configPath                  string
	upstreamListPath            string
	upstreamList                upstreamlist.UpstreamList
	upstreamListEntriesRevision uint64
}

func (s Snapshot) CATrusted() bool {
	return s.caTrusted
}

func (s Snapshot) UpstreamList() upstreamlist.UpstreamList {
	return cloneUpstreamList(s.upstreamList)
}

func (s Snapshot) UpstreamListEntriesRevision() uint64 {
	return s.upstreamListEntriesRevision
}

func (s Snapshot) ConfigPath() string {
	return s.configPath
}

func (s Snapshot) UpstreamListPath() string {
	return s.upstreamListPath
}

type fileConfig struct {
	CATrusted bool `yaml:"ca-trusted"`
}

type loadResult struct {
	Config       fileConfig
	ConfigPath   string
	UpstreamPath string
}

type Config struct {
	mu                 sync.RWMutex
	configPath         string
	upstreamListPath   string
	current            Snapshot
	hasCurrent         bool
	observationStarted bool
}

// Create bootstraps the fixed Live Configuration sources without reading or
// validating them.
func Create() (*Config, error) {
	dir, err := homeConfigDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(dir, configFileName)
	upstreamListPath := filepath.Join(dir, upstreamListFileName)
	if err := bootstrapFile(configPath, commentedDefaultConfig()); err != nil {
		return nil, err
	}
	if err := bootstrapFile(upstreamListPath, defaultUpstreamList); err != nil {
		return nil, err
	}
	return &Config{
		configPath:       configPath,
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
	loaded, err := loadExisting(c.configPath, c.upstreamListPath)
	if err != nil {
		if observed {
			return Snapshot{}, invalidConfigError(c.configPath, err)
		}
		return Snapshot{}, err
	}
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
	next := snapshotFromLoadResult(loaded, decoded, revision)
	if c.hasCurrent && !upstreamlist.SameEntries(c.current.upstreamList, next.upstreamList) {
		next.upstreamListEntriesRevision++
	}
	return next, nil
}

func sameSemanticSnapshot(left, right Snapshot) bool {
	if left.caTrusted != right.caTrusted ||
		left.configPath != right.configPath ||
		left.upstreamListPath != right.upstreamListPath ||
		left.upstreamListEntriesRevision != right.upstreamListEntriesRevision ||
		!slices.Equal(left.upstreamList.Warnings, right.upstreamList.Warnings) {
		return false
	}
	return true
}

func snapshotFromLoadResult(loaded loadResult, decoded upstreamlist.UpstreamList, upstreamListEntriesRevision uint64) Snapshot {
	return Snapshot{
		caTrusted:                   loaded.Config.CATrusted,
		configPath:                  loaded.ConfigPath,
		upstreamListPath:            loaded.UpstreamPath,
		upstreamList:                cloneUpstreamList(decoded),
		upstreamListEntriesRevision: upstreamListEntriesRevision,
	}
}

func cloneUpstreamList(source upstreamlist.UpstreamList) upstreamlist.UpstreamList {
	cloned := upstreamlist.UpstreamList{
		HostSelectors:   append([]upstreamlist.HostSelector(nil), source.HostSelectors...),
		OriginSelectors: append([]upstreamlist.OriginSelector(nil), source.OriginSelectors...),
		Warnings:        append([]upstreamlist.Warning(nil), source.Warnings...),
	}
	return cloned
}

func homeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors"), nil
}

func loadExisting(configPath, upstreamListPath string) (loadResult, error) {
	data, err := readRegularFile(configPath)
	if err != nil {
		return loadResult{}, err
	}
	return parseFileConfig(configPath, upstreamListPath, data)
}

func parseFileConfig(configPath, upstreamListPath string, data []byte) (loadResult, error) {
	var cfg fileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return loadResult{}, fmt.Errorf("invalid config.yaml: %w", err)
	}
	return loadResult{
		Config:       cfg,
		ConfigPath:   configPath,
		UpstreamPath: upstreamListPath,
	}, nil
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

func commentedDefaultConfig() string {
	return `# Enable trusted HTTPS interception through the Installed User CA.
ca-trusted: false
`
}
