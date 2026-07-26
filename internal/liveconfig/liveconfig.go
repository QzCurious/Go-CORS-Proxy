package liveconfig

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const defaultConfigFileName = "config.yaml"
const defaultDomainList = "# One domain or origin per line.\n# api.dev.example.com\n"

type Snapshot struct {
	caTrusted                 bool
	configPath                string
	domainListPath            string
	domainListEntries         []DomainListEntry
	domainListEntriesRevision uint64
	caTrustPending            bool
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

func (s Snapshot) CATrustPending() bool {
	return s.caTrustPending
}

func (s Snapshot) ConfigPath() string {
	return s.configPath
}

func (s Snapshot) DomainListPath() string {
	return s.domainListPath
}

type fileConfig struct {
	DomainList string `yaml:"domain-list"`
	CATrusted  bool   `yaml:"ca-trusted"`
}

type loadResult struct {
	Config     fileConfig
	ConfigData []byte
	ConfigPath string
	DomainPath string
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

func Open(configPath string) (*Source, error) {
	loaded, err := loadOrBootstrap(configPath)
	if err != nil {
		return nil, err
	}
	if err := bootstrapDomainList(loaded.DomainPath); err != nil {
		return nil, err
	}
	entries, domainData, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, err
	}
	snapshot := snapshotFromLoadResult(loaded, entries, 1, false, loaded.Config.CATrusted)
	source := newSource(loaded.Config, snapshot, loaded.ConfigData, domainData)
	return source, nil
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
	return snapshotFromLoadResult(loaded, entries, 1, false, loaded.Config.CATrusted), nil
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

func (s *Source) Reload() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchStarted {
		return Snapshot{}, errors.New("Live Configuration Reload must be called before Watch")
	}
	loaded, err := loadExisting(s.current.configPath)
	if err != nil {
		return Snapshot{}, err
	}
	entries, domainData, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return Snapshot{}, err
	}
	revision := s.current.domainListEntriesRevision
	if !sameDomainListEntries(s.current.domainListEntries, entries) {
		revision++
	}
	snapshot := snapshotFromLoadResult(loaded, entries, revision, false, loaded.Config.CATrusted)
	s.current = snapshot
	s.desiredConfig = loaded.Config
	s.baselineCATrusted = loaded.Config.CATrusted
	s.configFingerprint = sha256.Sum256(loaded.ConfigData)
	s.domainFingerprint = sha256.Sum256(domainData)
	return snapshot, nil
}

func (s *Source) Watch(ctx context.Context, apply func(Snapshot)) error {
	if apply == nil {
		return errors.New("Live Configuration Watch requires an apply function")
	}
	if err := s.startWatch(); err != nil {
		return err
	}
	events := make(chan watchEvent, 1)
	go s.watch(ctx, events)
	for event := range events {
		if event.err != nil {
			return event.err
		}
		apply(event.snapshot)
	}
	return nil
}

func sameSemanticSnapshot(left, right Snapshot) bool {
	if left.caTrusted != right.caTrusted ||
		left.configPath != right.configPath ||
		left.domainListPath != right.domainListPath ||
		left.caTrustPending != right.caTrustPending ||
		!sameDomainListEntries(left.domainListEntries, right.domainListEntries) {
		return false
	}
	return true
}

func snapshotFromLoadResult(loaded loadResult, entries []DomainListEntry, domainListEntriesRevision uint64, caTrustPending bool, activeCATrusted bool) Snapshot {
	return Snapshot{
		caTrusted:                 activeCATrusted,
		configPath:                loaded.ConfigPath,
		domainListPath:            loaded.DomainPath,
		domainListEntries:         append([]DomainListEntry(nil), entries...),
		domainListEntriesRevision: domainListEntriesRevision,
		caTrustPending:            caTrustPending,
	}
}

func homeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors"), nil
}

func defaultConfigPath() (string, error) {
	home, err := homeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultConfigFileName), nil
}

func loadOrBootstrap(configPath string) (loadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = defaultConfigPath()
		if err != nil {
			return loadResult{}, err
		}
	}
	configPath, err := absolutePath(configPath)
	if err != nil {
		return loadResult{}, err
	}

	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return loadResult{}, err
		}
		if err := bootstrap(configPath); err != nil {
			return loadResult{}, err
		}
	}

	return loadExisting(configPath)
}

func loadExisting(configPath string) (loadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = defaultConfigPath()
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
	domainPath, err := expandPath(cfg.DomainList)
	if err != nil {
		return loadResult{}, err
	}
	cfg.DomainList = domainPath
	if err := validateFileConfig(cfg); err != nil {
		return loadResult{}, err
	}
	return loadResult{
		Config:     cfg,
		ConfigData: data,
		ConfigPath: configPath,
		DomainPath: cfg.DomainList,
	}, nil
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		DomainList: "~/.seamless-cors/domains.txt",
		CATrusted:  false,
	}
}

func validateFileConfig(cfg fileConfig) error {
	if cfg.DomainList == "" {
		return fmt.Errorf("domain-list is required")
	}
	return nil
}

func expandPath(path string) (string, error) {
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

func bootstrap(configPath string) error {
	home := filepath.Dir(configPath)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(commentedDefaultConfig()), 0o600)
}

func bootstrapDomainList(path string) error {
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
	if _, err := file.WriteString(defaultDomainList); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func commentedDefaultConfig() string {
	return `# One domain or origin per line.
domain-list: ~/.seamless-cors/domains.txt

# Enable trusted HTTPS interception through the Installed User CA.
ca-trusted: false
`
}
