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

	"seamless-cors/internal/domainlist"

	"gopkg.in/yaml.v3"
)

const DefaultConfigFileName = "config.yaml"
const DefaultDomainListFileName = "domains.txt"

type Config struct {
	caTrusted        bool
	configPath       string
	domainListPath   string
	entries          []domainlist.Entry
	pendingLifecycle []string
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
	Config Config
	Err    error
}

type Source struct {
	mu                sync.RWMutex
	config            Config
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

func LoadOrBootstrap(configPath string, stdout io.Writer) (*Source, Config, error) {
	loaded, err := loadOrBootstrap(configPath, stdout)
	if err != nil {
		return nil, Config{}, err
	}
	entries, domainData, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, Config{}, err
	}
	configData, err := readRegularFile(loaded.ConfigPath)
	if err != nil {
		return nil, Config{}, err
	}
	live := configFromLoadResult(loaded, entries, nil, loaded.Config.CATrusted)
	source := newSource(loaded.Config, live, configData, domainData)
	return source, live, nil
}

func LoadExisting(configPath string) (Config, error) {
	loaded, err := loadExisting(configPath)
	if err != nil {
		return Config{}, err
	}
	entries, _, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return Config{}, err
	}
	return configFromLoadResult(loaded, entries, nil, loaded.Config.CATrusted), nil
}

func newSource(desired fileConfig, live Config, configData, domainData []byte) *Source {
	return &Source{
		config:            live,
		desiredConfig:     desired,
		baselineCATrusted: live.CATrusted(),
		configFingerprint: sha256.Sum256(configData),
		domainFingerprint: sha256.Sum256(domainData),
	}
}

func (s *Source) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *Source) Watch(ctx context.Context) <-chan Event {
	events := make(chan Event, 1)
	go s.watch(ctx, events)
	return events
}

func sameSemanticConfig(left, right Config) bool {
	if left.caTrusted != right.caTrusted ||
		left.configPath != right.configPath ||
		left.domainListPath != right.domainListPath ||
		!sameStrings(left.pendingLifecycle, right.pendingLifecycle) ||
		len(left.entries) != len(right.entries) {
		return false
	}
	type entryIdentity struct {
		scheme   string
		host     string
		port     string
		wildcard bool
	}
	entries := make(map[entryIdentity]struct{}, len(left.entries))
	for _, entry := range left.entries {
		entries[entryIdentity{entry.Scheme, entry.Host, entry.Port, entry.Wildcard}] = struct{}{}
	}
	for _, entry := range right.entries {
		if _, ok := entries[entryIdentity{entry.Scheme, entry.Host, entry.Port, entry.Wildcard}]; !ok {
			return false
		}
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

func configFromLoadResult(loaded loadResult, entries []domainlist.Entry, pending []string, activeCATrusted bool) Config {
	return Config{
		caTrusted:        activeCATrusted,
		configPath:       loaded.ConfigPath,
		domainListPath:   loaded.DomainPath,
		entries:          append([]domainlist.Entry(nil), entries...),
		pendingLifecycle: append([]string(nil), pending...),
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

func loadDomainList(path string) ([]domainlist.Entry, []byte, error) {
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

func formatDomainErrors(errs []domainlist.LineError) string {
	var lines []string
	for _, err := range errs {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
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

func (c Config) CATrusted() bool {
	return c.caTrusted
}

func (c Config) Entries() []domainlist.Entry {
	return append([]domainlist.Entry(nil), c.entries...)
}

func (c Config) PendingLifecycle() []string {
	return append([]string(nil), c.pendingLifecycle...)
}

func (c Config) ConfigPath() string {
	return c.configPath
}

func (c Config) DomainListPath() string {
	return c.domainListPath
}
