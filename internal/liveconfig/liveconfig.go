package liveconfig

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const DefaultConfigFileName = "config.yaml"
const DefaultDomainListFileName = "domains.txt"

type Snapshot struct {
	caTrusted                 bool
	configPath                string
	domainListPath            string
	domainListEntries         []DomainListEntry
	domainListEntriesRevision uint64
	pendingLifecycle          []string
}

type fileConfig struct {
	DomainList string `yaml:"domain-list"`
	CATrusted  bool   `yaml:"ca-trusted"`
}

type loadResult struct {
	Config       fileConfig
	ConfigPath   string
	DomainPath   string
	Bootstrapped bool
}

type Event struct {
	Snapshot Snapshot
	Err      error
}

type Source struct {
	mu                sync.RWMutex
	current           Snapshot
	desiredConfig     fileConfig
	baselineCATrusted bool
	configFingerprint [sha256.Size]byte
	domainFingerprint [sha256.Size]byte
	watchStarted      bool
}

func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors"), nil
}

func DefaultConfigPath() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultConfigFileName), nil
}

func RuntimeDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "runtime"), nil
}

func CADir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "ca"), nil
}

func LoadOrBootstrap(configPath string, stdout io.Writer) (*Source, Snapshot, error) {
	loaded, err := loadOrBootstrap(configPath, stdout)
	if err != nil {
		return nil, Snapshot{}, err
	}
	entries, domainData, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, Snapshot{}, err
	}
	configData, err := readRegularFile(loaded.ConfigPath)
	if err != nil {
		return nil, Snapshot{}, err
	}
	snapshot := snapshotFromLoadResult(loaded, entries, 1, nil, loaded.Config.CATrusted)
	source := newSource(loaded.Config, snapshot, configData, domainData)
	return source, snapshot, nil
}

func LoadExisting(configPath string) (Snapshot, error) {
	loaded, err := loadExisting(configPath)
	if err != nil {
		return Snapshot{}, err
	}
	entries, _, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshotFromLoadResult(loaded, entries, 1, nil, loaded.Config.CATrusted), nil
}

func newSource(desired fileConfig, snapshot Snapshot, configData, domainData []byte) *Source {
	return &Source{
		current:           snapshot,
		desiredConfig:     desired,
		baselineCATrusted: snapshot.CATrusted(),
		configFingerprint: sha256.Sum256(configData),
		domainFingerprint: sha256.Sum256(domainData),
	}
}

func (s *Source) Current() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *Source) Watch(ctx context.Context) <-chan Event {
	events := make(chan Event, 1)
	go s.watch(ctx, events)
	return events
}

func sameSemanticSnapshot(left, right Snapshot) bool {
	if left.caTrusted != right.caTrusted ||
		left.configPath != right.configPath ||
		left.domainListPath != right.domainListPath ||
		!sameStrings(left.pendingLifecycle, right.pendingLifecycle) ||
		!sameDomainListEntries(left.domainListEntries, right.domainListEntries) {
		return false
	}
	return true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func snapshotFromLoadResult(loaded loadResult, entries []DomainListEntry, domainListEntriesRevision uint64, pending []string, activeCATrusted bool) Snapshot {
	return Snapshot{
		caTrusted:                 activeCATrusted,
		configPath:                loaded.ConfigPath,
		domainListPath:            loaded.DomainPath,
		domainListEntries:         append([]DomainListEntry(nil), entries...),
		domainListEntriesRevision: domainListEntriesRevision,
		pendingLifecycle:          append([]string(nil), pending...),
	}
}

func loadOrBootstrap(configPath string, stdout io.Writer) (loadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return loadResult{}, err
		}
	}
	configPath, err := absolutePath(configPath)
	if err != nil {
		return loadResult{}, err
	}

	var bootstrapped bool
	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return loadResult{}, err
		}
		if err := bootstrap(configPath); err != nil {
			return loadResult{}, err
		}
		bootstrapped = true
		if stdout != nil {
			home, _ := HomeDir()
			fmt.Fprintf(stdout, "Created:\n  %s\n  %s\n\n", configPath, filepath.Join(home, DefaultDomainListFileName))
		}
	}

	loaded, err := loadExisting(configPath)
	if err != nil {
		return loadResult{}, err
	}
	loaded.Bootstrapped = bootstrapped
	return loaded, nil
}

func loadExisting(configPath string) (loadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return loadResult{}, err
		}
	}
	configPath, err := absolutePath(configPath)
	if err != nil {
		return loadResult{}, err
	}
	data, err := readRegularFile(configPath)
	if err != nil {
		return loadResult{}, err
	}
	return parseFileConfig(configPath, data)
}

func parseFileConfig(configPath string, data []byte) (loadResult, error) {
	cfg := defaultFileConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return loadResult{}, fmt.Errorf("invalid config.yaml: %w", err)
	}
	domainPath, err := ExpandPath(cfg.DomainList)
	if err != nil {
		return loadResult{}, err
	}
	cfg.DomainList = domainPath
	if err := validateFileConfig(cfg); err != nil {
		return loadResult{}, err
	}
	return loadResult{
		Config:     cfg,
		ConfigPath: configPath,
		DomainPath: cfg.DomainList,
	}, nil
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		DomainList: "~/.seamless-cors/domains.txt",
		CATrusted:  true,
	}
}

func validateFileConfig(cfg fileConfig) error {
	if cfg.DomainList == "" {
		return fmt.Errorf("domain-list is required")
	}
	return nil
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return absolutePath(os.ExpandEnv(path))
}

func loadDomainList(path string) ([]DomainListEntry, []byte, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, nil, err
	}
	entries, err := parseDomainList(data)
	if err != nil {
		return nil, nil, err
	}
	return entries, data, nil
}

func absolutePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
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

func lifecycleChanges(nextCATrusted, baselineCATrusted bool) []string {
	if nextCATrusted != baselineCATrusted {
		return []string{"ca-trusted"}
	}
	return nil
}

func bootstrap(configPath string) error {
	home := filepath.Dir(configPath)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	domainPath := filepath.Join(home, DefaultDomainListFileName)
	if _, err := os.Stat(domainPath); os.IsNotExist(err) {
		if err := os.WriteFile(domainPath, []byte("# One domain or origin per line.\n# api.dev.example.com\n"), 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(configPath, []byte(commentedDefaultConfig()), 0o600)
}

func commentedDefaultConfig() string {
	return `# One domain or origin per line.
domain-list: ~/.seamless-cors/domains.txt

# Enable trusted HTTPS interception through the Installed User CA.
ca-trusted: true
`
}

func (s Snapshot) CATrusted() bool {
	return s.caTrusted
}

func (s Snapshot) DomainListEntries() []DomainListEntry {
	return append([]DomainListEntry(nil), s.domainListEntries...)
}

func (s Snapshot) DomainListEntriesRevision() uint64 {
	return s.domainListEntriesRevision
}

func (s Snapshot) PendingLifecycle() []string {
	return append([]string(nil), s.pendingLifecycle...)
}

func (s Snapshot) ConfigPath() string {
	return s.configPath
}

func (s Snapshot) DomainListPath() string {
	return s.domainListPath
}
