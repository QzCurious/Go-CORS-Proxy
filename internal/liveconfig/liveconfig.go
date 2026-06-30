package liveconfig

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"seamless-cors/internal/domain"

	"gopkg.in/yaml.v3"
)

const DefaultConfigFileName = "config.yaml"
const DefaultDomainListFileName = "domains.txt"

type Config struct {
	caTrusted        bool
	configPath       string
	domainListPath   string
	entries          []domain.Entry
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
	lastConfigText    string
	lastDomainText    string
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
	entries, domainText, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return nil, Config{}, err
	}
	configText, err := readText(loaded.ConfigPath)
	if err != nil {
		return nil, Config{}, err
	}
	live := configFromLoadResult(loaded, entries, nil, loaded.Config.CATrusted)
	source := newSource(loaded.Config, live, configText, domainText)
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

func newSource(desired fileConfig, live Config, configText, domainText string) *Source {
	live = cloneConfig(live)
	return &Source{
		config:            live,
		desiredConfig:     desired,
		baselineCATrusted: live.CATrusted(),
		lastConfigText:    configText,
		lastDomainText:    domainText,
	}
}

func (s *Source) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

func (s *Source) Watch(ctx context.Context, interval time.Duration) <-chan Event {
	events := make(chan Event, 1)
	live := s.Config()
	if live.ConfigPath() == "" && live.DomainListPath() == "" {
		close(events)
		return events
	}
	go s.watch(ctx, interval, events)
	return events
}

func (s *Source) watch(ctx context.Context, interval time.Duration, events chan<- Event) {
	defer close(events)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			event, changed := s.poll()
			if !changed {
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
			if event.Err != nil {
				return
			}
		}
	}
}

func (s *Source) poll() (Event, bool) {
	current := s.Config()
	var configText string
	var configChanged bool
	if current.ConfigPath() != "" {
		var configErr error
		configText, configErr = readText(current.ConfigPath())
		if configErr != nil {
			return Event{Err: fmt.Errorf("Fatal Config Error: %w", configErr)}, true
		}
		configChanged = configText != s.lastConfigText
	}
	domainPath := current.DomainListPath()
	var loaded loadResult
	if configChanged {
		var err error
		loaded, err = loadExisting(current.ConfigPath())
		if err != nil {
			return Event{Err: fmt.Errorf("Fatal Config Error: %w", err)}, true
		}
		domainPath = loaded.DomainPath
	} else {
		s.mu.RLock()
		desired := s.desiredConfig
		s.mu.RUnlock()
		loaded = loadResult{
			Config:     desired,
			ConfigPath: current.ConfigPath(),
			DomainPath: current.DomainListPath(),
		}
	}
	domainText, domainErr := readText(domainPath)
	if domainErr != nil {
		return Event{Err: fmt.Errorf("Fatal Domain List Error: %w", domainErr)}, true
	}
	domainChanged := domainText != s.lastDomainText || domainPath != current.DomainListPath()
	if !configChanged && !domainChanged {
		return Event{}, false
	}
	entries, errs := domain.ParseList(domainText)
	if len(errs) > 0 {
		return Event{Err: fmt.Errorf("Fatal Domain List Error: invalid Domain List:\n%s", formatDomainErrors(errs))}, true
	}
	next := configFromLoadResult(loaded, entries, lifecycleChanges(loaded.Config.CATrusted, s.baselineCATrusted), s.baselineCATrusted)
	s.mu.Lock()
	s.config = next
	s.desiredConfig = loaded.Config
	s.lastConfigText = configText
	s.lastDomainText = domainText
	s.mu.Unlock()
	return Event{Config: cloneConfig(next)}, true
}

func configFromLoadResult(loaded loadResult, entries []domain.Entry, pending []string, activeCATrusted bool) Config {
	return Config{
		caTrusted:        activeCATrusted,
		configPath:       loaded.ConfigPath,
		domainListPath:   loaded.DomainPath,
		entries:          append([]domain.Entry(nil), entries...),
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
	data, err := os.ReadFile(configPath)
	if err != nil {
		return loadResult{}, err
	}
	cfg := defaultFileConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return loadResult{}, fmt.Errorf("invalid config.yaml: %w", err)
	}
	cfg.DomainList, err = ExpandPath(cfg.DomainList)
	if err != nil {
		return loadResult{}, err
	}
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
	return os.ExpandEnv(path), nil
}

func loadDomainList(path string) ([]domain.Entry, string, error) {
	text, err := readText(path)
	if err != nil {
		return nil, "", err
	}
	entries, errs := domain.ParseList(text)
	if len(errs) > 0 {
		return nil, "", fmt.Errorf("invalid Domain List:\n%s", formatDomainErrors(errs))
	}
	return entries, text, nil
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func lifecycleChanges(nextCATrusted, baselineCATrusted bool) []string {
	if nextCATrusted != baselineCATrusted {
		return []string{"ca-trusted"}
	}
	return nil
}

func formatDomainErrors(errs []domain.LineError) string {
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

func (c Config) Entries() []domain.Entry {
	return append([]domain.Entry(nil), c.entries...)
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

func cloneConfig(live Config) Config {
	live.entries = append([]domain.Entry(nil), live.entries...)
	live.pendingLifecycle = append([]string(nil), live.pendingLifecycle...)
	return live
}
