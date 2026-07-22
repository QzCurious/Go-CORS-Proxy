package managedpac

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
)

const FootprintFileName = "seamless-cors.pac"

type FootprintInspection struct {
	OwnedSnapshots []ServiceSnapshot
}

func (i FootprintInspection) Needed() bool {
	return len(i.OwnedSnapshots) > 0
}

func InspectFootprint(ctx context.Context, settings interface {
	Snapshot(context.Context) ([]ServiceSnapshot, error)
}) (FootprintInspection, error) {
	states, err := settings.Snapshot(ctx)
	if err != nil {
		return FootprintInspection{}, fmt.Errorf("managed PAC footprint inspection failed: %w", err)
	}
	owned := make([]ServiceSnapshot, 0, len(states))
	for _, state := range states {
		if state.Enabled && IsOwnedURL(state.PACURL) {
			owned = append(owned, state)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].ServiceName < owned[j].ServiceName })
	return FootprintInspection{OwnedSnapshots: owned}, nil
}

func ClearFootprint(ctx context.Context, settings SystemSettings) error {
	inspection, err := InspectFootprint(ctx, settings)
	if err != nil {
		return err
	}
	if !inspection.Needed() {
		return nil
	}
	if err := settings.ClearIfUnchanged(ctx, inspection.OwnedSnapshots); err != nil {
		return fmt.Errorf("managed PAC footprint clear failed: %w", err)
	}
	after, err := InspectFootprint(ctx, settings)
	if err != nil {
		return fmt.Errorf("managed PAC footprint verification failed: %w", err)
	}
	if after.Needed() {
		services := make([]string, 0, len(after.OwnedSnapshots))
		for _, state := range after.OwnedSnapshots {
			services = append(services, state.ServiceName)
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
