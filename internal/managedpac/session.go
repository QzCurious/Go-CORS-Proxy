package managedpac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrManagedPACLeaseLost = errors.New("managed PAC lease lost")
	ErrSessionClosed       = errors.New("managed PAC session closed")
)

type Ownership string

const (
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

type ServiceAssessment struct {
	ServiceName string
	Enabled     bool
	PACURL      string
	Ownership   Ownership
}

type Assessment struct {
	ServiceSet          []string
	Services            []ServiceAssessment
	ReplacementRequired bool
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

func Assess(ctx context.Context, settings SystemSettings) (Assessment, error) {
	states, err := settings.Snapshot(ctx)
	if err != nil {
		return Assessment{}, err
	}
	managedStates := serviceAssessments(states)
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
		Services:            managedStates,
		ReplacementRequired: replacementRequired,
	}, nil
}

func serviceAssessments(states []ServiceSnapshot) []ServiceAssessment {
	out := make([]ServiceAssessment, 0, len(states))
	for _, state := range states {
		out = append(out, ServiceAssessment{
			ServiceName: state.ServiceName,
			Enabled:     state.Enabled,
			PACURL:      state.PACURL,
			Ownership:   OwnershipForURL(state.PACURL),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServiceName < out[j].ServiceName
	})
	return out
}

type Session struct {
	mutationMu   sync.Mutex
	mu           sync.RWMutex
	settings     SystemSettings
	services     []string
	currentURL   string
	attemptedURL string
	closed       bool
}

type StartResult struct {
	InstalledServices []string
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

func Start(ctx context.Context, settings SystemSettings, services []string, pacURL string) (*Session, StartResult, error) {
	session, err := Prepare(settings, services, pacURL)
	if err != nil {
		return nil, StartResult{}, err
	}
	result, err := session.Install(ctx)
	if err != nil {
		return nil, StartResult{}, err
	}
	return session, result, nil
}

// Prepare creates the mutation owner before the first platform write. This
// lets lifecycle cancellation close the sequence even while install races it.
func Prepare(settings SystemSettings, services []string, pacURL string) (*Session, error) {
	selected := sortedStrings(services)
	if len(selected) == 0 {
		return nil, fmt.Errorf("managed PAC service set is empty")
	}
	return &Session{settings: settings, services: selected, currentURL: pacURL}, nil
}

func (s *Session) Install(ctx context.Context) (StartResult, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.isClosed() {
		return StartResult{}, ErrSessionClosed
	}
	result, err := s.settings.Apply(ctx, s.currentURL, s.services)
	if err != nil {
		return StartResult{}, err
	}
	installed := result.AppliedServices
	if len(installed) == 0 {
		return StartResult{}, fmt.Errorf("managed PAC install updated no services")
	}
	installed = sortedStrings(installed)
	return StartResult{InstalledServices: installed}, nil
}

func (s *Session) Refresh(ctx context.Context, nextURL string) error {
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
	states, err := s.settings.Snapshot(ctx)
	if err != nil {
		return RefreshError{FromURL: currentURL, ToURL: nextURL, Err: fmt.Errorf("managed PAC lease inspection failed: %w", err)}
	}
	if _, err := selectedOwnedDrift(states, currentURL, services); err != nil {
		return RefreshError{FromURL: currentURL, ToURL: nextURL, Err: err}
	}
	if _, err := s.settings.Apply(ctx, nextURL, services); err != nil {
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

func (s *Session) RequireLease(ctx context.Context) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.isClosed() {
		return ErrSessionClosed
	}
	currentURL := s.CurrentURL()
	services := s.Services()
	states, err := s.settings.Snapshot(ctx)
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
	if _, err := s.settings.Apply(ctx, currentURL, sortedStrings(reattach)); err != nil {
		return fmt.Errorf("managed PAC reattachment failed: %w", err)
	}
	return nil
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

func selectedOwnedDrift(states []ServiceSnapshot, currentURL string, services []string) ([]string, error) {
	selected := stringSet(services)
	var reattach []string
	for _, state := range states {
		if _, ok := selected[state.ServiceName]; !ok {
			continue
		}
		if state.Enabled && state.PACURL == currentURL {
			continue
		}
		if OwnershipForURL(state.PACURL) != OwnershipOwned {
			return nil, ErrManagedPACLeaseLost
		}
		reattach = append(reattach, state.ServiceName)
	}
	return sortedStrings(reattach), nil
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
