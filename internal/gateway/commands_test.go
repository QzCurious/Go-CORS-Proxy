package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStopWithoutOwnerReturnsNotRunningAndRemovesStaleCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coord := newCoordinator(filepath.Join(home, ".seamless-cors", "runtime"))
	if err := coord.Write(stateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale"}); err != nil {
		t.Fatal(err)
	}
	settings := &lifecycleTestSystemSettings{states: []testPACState{{
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

func TestOwnerlessInstallPublishesTransientOwnerAndFailsCompetingWorkFast(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entered := make(chan struct{})
	release := make(chan struct{})
	ca := &fakeUserCA{
		install: func(context.Context) (userCAInstallResult, error) {
			close(entered)
			<-release
			return userCAInstallResult{changed: true}, nil
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := installCA(context.Background(), ca)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("ownerless install did not begin")
	}

	target, err := discover()
	if err != nil {
		t.Fatal(err)
	}
	if target.kind != targetActive {
		t.Fatalf("transient owner discovery = %s", target.kind)
	}
	status, err := target.client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != GatewayStatusRouterOnly || status.InstalledCA.Health != CAHealthMutating {
		t.Fatalf("transient status = %#v", status)
	}
	start, err := target.client.Start(context.Background(), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if start.Kind != StartResultStartAlreadyMutating {
		t.Fatalf("start during transient mutation = %#v", start)
	}
	if _, err := target.client.Install(context.Background()); !errors.Is(err, errCAOperationInProgress) {
		t.Fatalf("competing install error = %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, err := discover()
	if err != nil {
		t.Fatal(err)
	}
	if after.kind != targetMissing {
		t.Fatalf("transient owner remained discoverable: %s", after.kind)
	}
	if ca.inspectCalls != 0 {
		t.Fatalf("transient owner inspected UserCA before its lifecycle operation: %d calls", ca.inspectCalls)
	}
}

func TestOwnerlessStatusReportsOwnershipTransitionInsteadOfInspectingUnlocked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	coord := newCoordinator(filepath.Join(home, ".seamless-cors", "runtime"))
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("test did not acquire Gateway Ownership")
	}
	defer lease.Release()
	ca := &fakeUserCA{}

	_, err = status(context.Background(), &lifecycleTestSystemSettings{}, ca)

	if !errors.Is(err, errOwnerTransition) {
		t.Fatalf("status error = %v", err)
	}
	if ca.inspectCalls != 0 {
		t.Fatalf("status inspected UserCA without coherent ownership: %d calls", ca.inspectCalls)
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
		states: []testPACState{{
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
