package managedpac

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"seamless-cors/internal/platform"
)

var (
	ErrManagedPACLeaseLost = errors.New("managed PAC lease lost")
	ErrSessionClosed       = errors.New("managed PAC session closed")
)

type Adapter interface {
	ApplyPAC(url string, services []string) ([]platform.PACServiceUpdate, error)
	CurrentPACState() ([]platform.PACServiceState, error)
}

type Ownership string

const (
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type ServiceState struct {
	ServiceName string
	Enabled     bool
	URL         string
	Ownership   Ownership
}

type Assessment struct {
	ServiceSet          []string
	States              []ServiceState
	ReplacementRequired bool
}

func Assess(adapter Adapter) (Assessment, error) {
	states, err := adapter.CurrentPACState()
	if err != nil {
		return Assessment{}, err
	}
	managedStates := serviceStates(states)
	serviceSet := make([]string, 0, len(managedStates))
	replacementRequired := false
	for _, state := range managedStates {
		serviceSet = append(serviceSet, state.ServiceName)
		if state.Ownership == OwnershipForeign {
			replacementRequired = true
		}
	}
	sort.Strings(serviceSet)
	return Assessment{
		ServiceSet:          serviceSet,
		States:              managedStates,
		ReplacementRequired: replacementRequired,
	}, nil
}

type Session struct {
	mutationMu   sync.Mutex
	mu           sync.RWMutex
	adapter      Adapter
	services     []string
	currentURL   string
	attemptedURL string
	closed       bool
}

type StartResult struct {
	InstalledServices []string
}

func Start(adapter Adapter, services []string, pacURL string) (*Session, StartResult, error) {
	session, err := Prepare(adapter, services, pacURL)
	if err != nil {
		return nil, StartResult{}, err
	}
	result, err := session.Install()
	if err != nil {
		return nil, StartResult{}, err
	}
	return session, result, nil
}

// Prepare creates the mutation owner before the first platform write. This
// lets lifecycle cancellation close the sequence even while install races it.
func Prepare(adapter Adapter, services []string, pacURL string) (*Session, error) {
	selected := sortedStrings(services)
	if len(selected) == 0 {
		return nil, fmt.Errorf("managed PAC service set is empty")
	}
	return &Session{adapter: adapter, services: selected, currentURL: pacURL}, nil
}

func (s *Session) Install() (StartResult, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.isClosed() {
		return StartResult{}, ErrSessionClosed
	}
	updates, err := s.adapter.ApplyPAC(s.currentURL, s.services)
	if err != nil {
		return StartResult{}, err
	}
	installed := appliedServices(updates)
	if len(installed) == 0 {
		return StartResult{}, fmt.Errorf("managed PAC install updated no services")
	}
	installed = sortedStrings(installed)
	return StartResult{InstalledServices: installed}, nil
}

func (s *Session) Refresh(nextURL string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.isClosed() {
		return ErrSessionClosed
	}
	s.mu.Lock()
	s.attemptedURL = nextURL
	services := append([]string(nil), s.services...)
	currentURL := s.currentURL
	s.mu.Unlock()
	states, err := s.adapter.CurrentPACState()
	if err != nil {
		return RefreshError{FromURL: currentURL, ToURL: nextURL, Err: fmt.Errorf("managed PAC lease inspection failed: %w", err)}
	}
	if _, err := selectedOwnedDrift(states, currentURL, services); err != nil {
		return RefreshError{FromURL: currentURL, ToURL: nextURL, Err: err}
	}
	if _, err := s.adapter.ApplyPAC(nextURL, services); err != nil {
		return RefreshError{
			FromURL: currentURL,
			ToURL:   nextURL,
			Err:     err,
		}
	}
	s.mu.Lock()
	s.currentURL = nextURL
	s.attemptedURL = ""
	s.mu.Unlock()
	return nil
}

func appliedServices(updates []platform.PACServiceUpdate) []string {
	var applied []string
	for _, update := range updates {
		if update.Outcome == platform.PACApplyOutcomeApplied {
			applied = append(applied, update.ServiceName)
		}
	}
	return applied
}

func (s *Session) RequireLease() error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.isClosed() {
		return ErrSessionClosed
	}
	currentURL := s.CurrentURL()
	services := s.Services()
	states, err := s.adapter.CurrentPACState()
	if err != nil {
		return fmt.Errorf("managed PAC lease inspection failed: %w", err)
	}
	reattach, err := selectedOwnedDrift(states, currentURL, services)
	if err != nil {
		return err
	}
	if len(reattach) == 0 {
		return nil
	}
	if _, err := s.adapter.ApplyPAC(currentURL, sortedStrings(reattach)); err != nil {
		return fmt.Errorf("managed PAC reattachment failed: %w", err)
	}
	return nil
}

func selectedOwnedDrift(states []platform.PACServiceState, currentURL string, services []string) ([]string, error) {
	selected := stringSet(services)
	var reattach []string
	for _, state := range states {
		if _, ok := selected[state.Name]; !ok {
			continue
		}
		if state.Enabled && state.URL == currentURL {
			continue
		}
		if OwnershipForURL(state.URL) != OwnershipOwned {
			return nil, ErrManagedPACLeaseLost
		}
		reattach = append(reattach, state.Name)
	}
	return sortedStrings(reattach), nil
}

func (s *Session) Close() {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *Session) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Session) CurrentURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentURL
}

func (s *Session) AttemptedURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attemptedURL
}

func (s *Session) Services() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.services...)
}

type RefreshError struct {
	FromURL string
	ToURL   string
	Err     error
}

func (e RefreshError) Error() string {
	return fmt.Sprintf("managed PAC refresh failed from %q to %q: %v", e.FromURL, e.ToURL, e.Err)
}

func (e RefreshError) Unwrap() error {
	return e.Err
}

func serviceStates(states []platform.PACServiceState) []ServiceState {
	out := make([]ServiceState, 0, len(states))
	for _, state := range states {
		out = append(out, ServiceState{
			ServiceName: state.Name,
			Enabled:     state.Enabled,
			URL:         state.URL,
			Ownership:   OwnershipForURL(state.URL),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServiceName < out[j].ServiceName
	})
	return out
}

func OwnershipForURL(raw string) Ownership {
	if raw == "" || raw == "(null)" {
		return OwnershipEmpty
	}
	if IsOwnedURL(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
