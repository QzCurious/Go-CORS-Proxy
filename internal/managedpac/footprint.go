package managedpac

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"

	"seamless-cors/internal/platform"
)

const FootprintFileName = "seamless-cors.pac"

type FootprintAdapter interface {
	CurrentPACState() ([]platform.PACServiceState, error)
	ClearPACIfMatches(expected []platform.PACServiceState) error
}

type FootprintInspection struct {
	OwnedStates []platform.PACServiceState
}

func (i FootprintInspection) Needed() bool {
	return len(i.OwnedStates) > 0
}

func InspectFootprint(adapter interface {
	CurrentPACState() ([]platform.PACServiceState, error)
}) (FootprintInspection, error) {
	states, err := adapter.CurrentPACState()
	if err != nil {
		return FootprintInspection{}, fmt.Errorf("managed PAC footprint inspection failed: %w", err)
	}
	owned := make([]platform.PACServiceState, 0, len(states))
	for _, state := range states {
		if state.Enabled && IsOwnedURL(state.URL) {
			owned = append(owned, state)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].Name < owned[j].Name })
	return FootprintInspection{OwnedStates: owned}, nil
}

func ClearFootprint(adapter FootprintAdapter) error {
	inspection, err := InspectFootprint(adapter)
	if err != nil {
		return err
	}
	if !inspection.Needed() {
		return nil
	}
	if err := adapter.ClearPACIfMatches(inspection.OwnedStates); err != nil {
		return fmt.Errorf("managed PAC footprint clear failed: %w", err)
	}
	after, err := InspectFootprint(adapter)
	if err != nil {
		return fmt.Errorf("managed PAC footprint verification failed: %w", err)
	}
	if after.Needed() {
		services := make([]string, 0, len(after.OwnedStates))
		for _, state := range after.OwnedStates {
			services = append(services, state.Name)
		}
		return fmt.Errorf("managed PAC footprint remains on services: %s", strings.Join(services, ", "))
	}
	return nil
}

func IsOwnedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return path.Base(u.EscapedPath()) == FootprintFileName
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && path.Base(u.EscapedPath()) == FootprintFileName
}
