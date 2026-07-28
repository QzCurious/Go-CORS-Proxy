package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestStopWithoutOwnerReturnsNotRunningAndRemovesStaleCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coord := newCoordinator(filepath.Join(home, ".seamless-cors", "runtime"))
	if err := coord.Write(stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{states: []managedpac.ServiceSnapshot{{
		ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true,
	}}}

	result, err := stop(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StopResultNotRunning {
		t.Fatalf("stop kind = %s, want %s", result.Kind, StopResultNotRunning)
	}
	if coord.Exists() {
		t.Fatal("stale Gateway State Cache was not removed")
	}
	if settings.cleared != 1 {
		t.Fatalf("managed PAC cleanup calls = %d, want 1", settings.cleared)
	}
}

func TestStopWithoutOwnerPreservesResultWhenCleanupFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coord := newCoordinator(filepath.Join(home, ".seamless-cors", "runtime"))
	if err := coord.Write(stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{
		clearErr: errors.New("pac denied"),
		states: []managedpac.ServiceSnapshot{{
			ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8079/seamless-cors.pac", Enabled: true,
		}},
	}

	result, err := stop(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != StopResultNotRunningCleanupFailed {
		t.Fatalf("stop kind = %s, want %s", result.Kind, StopResultNotRunningCleanupFailed)
	}
	if len(result.CleanupFailures) != 1 || result.CleanupFailures[0].Subject != CleanupSubjectManagedPAC {
		t.Fatalf("cleanup failures = %#v", result.CleanupFailures)
	}
	if coord.Exists() {
		t.Fatal("stale Gateway State Cache was not removed after PAC cleanup failure")
	}
}

func TestStopWithoutPublishedOwnerRejectsOwnershipLeaseContention(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coord := newCoordinator(filepath.Join(home, ".seamless-cors", "runtime"))
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("test did not acquire ownership lease")
	}
	defer lease.Release()
	settings := &lifecycleTestSystemSettings{}

	_, err = stop(context.Background(), settings)

	if err == nil || !strings.Contains(err.Error(), "retry after it finishes") {
		t.Fatalf("stop error = %v, want retryable ownership contention", err)
	}
	if settings.cleared != 0 {
		t.Fatalf("managed PAC cleanup calls = %d, want 0", settings.cleared)
	}
}
