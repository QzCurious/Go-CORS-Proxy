// Package systempac safely coordinates current-user System PAC settings.
package systempac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
)

// Module is the complete System PAC integration seam used by Gateway.
type Module interface {
	Deliver(context.Context, string) (State, error)
	Observe(context.Context, string) (State, error)
	Cleanup(context.Context) ([]ServiceState, error)
}

type ServiceState struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	Ownership Ownership `json:"ownership"`
}

type State struct {
	Generation            uint64         `json:"generation,omitempty"`
	Services              []ServiceState `json:"services"`
	RoutesCurrentEndpoint bool           `json:"routesCurrentEndpoint"`
}

type SystemPAC struct {
	mu           sync.Mutex
	generation   uint64
	listServices func(context.Context) ([]networkservice.Service, error)
}

func New() *SystemPAC { return &SystemPAC{listServices: networkservice.List} }

var _ Module = (*SystemPAC)(nil)

func (m *SystemPAC) Deliver(ctx context.Context, endpoint string) (State, error) {
	if endpoint == "" {
		return State{}, fmt.Errorf("System PAC endpoint is empty")
	}

	// Valid delivery attempts consume a generation and hold serialization through
	// their complete discover-observe-mutate-verify protocol.
	m.mu.Lock()
	defer m.mu.Unlock()

	m.generation++
	state := State{Generation: m.generation}
	services, err := m.discover(ctx)
	var operationErrs []error
	if err != nil {
		operationErrs = append(operationErrs, err)
	}
	before, observationErr := observeServices(ctx, services, false)
	if observationErr != nil {
		operationErrs = append(operationErrs, observationErr)
	}
	nextURL := publicationURL(endpoint, m.generation)
	for i, service := range services {
		if before[i].Ownership != OwnershipEmpty && before[i].Ownership != OwnershipOwned {
			continue
		}
		if err := service.SetPAC(ctx, nextURL); err != nil {
			operationErrs = append(operationErrs, MutationError{ServiceName: service.Name(), Cause: err})
		}
	}
	verified, verificationErr := observeServices(ctx, services, true)
	state.Services = verified
	state.RoutesCurrentEndpoint = routesEndpoint(verified, endpoint)
	if verificationErr != nil {
		operationErrs = append(operationErrs, verificationErr)
	}
	return state, errors.Join(operationErrs...)
}

func (m *SystemPAC) Observe(ctx context.Context, endpoint string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	services, err := m.discover(ctx)
	observed, observationErr := observeServices(ctx, services, false)
	return State{Services: observed, RoutesCurrentEndpoint: routesEndpoint(observed, endpoint)}, errors.Join(err, observationErr)
}

func (m *SystemPAC) Cleanup(ctx context.Context) ([]ServiceState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	services, err := m.discover(ctx)
	var operationErrs []error
	if err != nil {
		operationErrs = append(operationErrs, err)
	}
	before, observationErr := observeServices(ctx, services, false)
	if observationErr != nil {
		operationErrs = append(operationErrs, observationErr)
	}
	for i, service := range services {
		state := before[i]
		if !state.Enabled || state.Ownership != OwnershipOwned {
			continue
		}
		if err := service.DisablePAC(ctx); err != nil {
			operationErrs = append(operationErrs, MutationError{ServiceName: service.Name(), Cause: err})
		}
	}
	verified, verificationErr := observeServices(ctx, services, true)
	if verificationErr != nil {
		operationErrs = append(operationErrs, verificationErr)
	}
	var residue []string
	for _, state := range verified {
		if state.Enabled && state.Ownership == OwnershipOwned {
			residue = append(residue, state.Name)
		}
	}
	if len(residue) > 0 {
		operationErrs = append(operationErrs, ResidueError{Services: residue})
	}
	err = errors.Join(operationErrs...)
	return verified, err
}

func (m *SystemPAC) discover(ctx context.Context) ([]networkservice.Service, error) {
	services, err := m.listServices(ctx)
	if err != nil {
		return services, DiscoveryError{Cause: err}
	}
	return services, nil
}

func observeServices(ctx context.Context, services []networkservice.Service, verification bool) ([]ServiceState, error) {
	states := make([]ServiceState, len(services))
	var errs []error
	for i, service := range services {
		states[i] = ServiceState{Name: service.Name(), Ownership: OwnershipUnknown}
		setting, err := service.PAC(ctx)
		if err != nil {
			if verification {
				errs = append(errs, VerificationError{ServiceName: service.Name(), Cause: err})
			} else {
				errs = append(errs, ObservationError{ServiceName: service.Name(), Cause: err})
			}
			continue
		}
		states[i] = ServiceState{Name: service.Name(), URL: setting.URL, Enabled: setting.Enabled, Ownership: ownership(setting.URL)}
	}
	return states, errors.Join(errs...)
}

func routesEndpoint(states []ServiceState, endpoint string) bool {
	if endpoint == "" {
		return false
	}
	for _, state := range states {
		if state.Enabled && state.Ownership == OwnershipOwned && servesEndpoint(state.URL, endpoint) {
			return true
		}
	}
	return false
}

type Ownership string

const (
	OwnershipUnknown Ownership = "unknown"
	OwnershipEmpty   Ownership = "empty"
	OwnershipOwned   Ownership = "owned"
	OwnershipForeign Ownership = "foreign"
)

const marker = "seamless-cors.pac"

func publicationURL(endpoint string, generation uint64) string {
	u := url.URL{Scheme: "http", Host: endpoint, Path: "/" + marker}
	q := u.Query()
	q.Set("v", strconv.FormatUint(generation, 10))
	u.RawQuery = q.Encode()
	return u.String()
}

func ownership(raw string) Ownership {
	if strings.TrimSpace(raw) == "" {
		return OwnershipEmpty
	}
	if ownedURL(raw) {
		return OwnershipOwned
	}
	return OwnershipForeign
}

func ownedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || path.Base(u.EscapedPath()) != marker {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

func servesEndpoint(raw string, endpoint string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && ownedURL(raw) && u.Host == endpoint
}

type DiscoveryError struct{ Cause error }

func (e DiscoveryError) Error() string { return fmt.Sprintf("discover Network Services: %v", e.Cause) }
func (e DiscoveryError) Unwrap() error { return e.Cause }

type ObservationError struct {
	ServiceName string
	Cause       error
}

func (e ObservationError) Error() string {
	return fmt.Sprintf("observe System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e ObservationError) Unwrap() error { return e.Cause }

type MutationError struct {
	ServiceName string
	Cause       error
}

func (e MutationError) Error() string {
	return fmt.Sprintf("update System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e MutationError) Unwrap() error { return e.Cause }

type VerificationError struct {
	ServiceName string
	Cause       error
}

func (e VerificationError) Error() string {
	return fmt.Sprintf("verify System PAC for %s: %v", e.ServiceName, e.Cause)
}
func (e VerificationError) Unwrap() error { return e.Cause }

type ResidueError struct{ Services []string }

func (e ResidueError) Error() string {
	return "active owned System PAC remains on " + strings.Join(e.Services, ", ")
}
