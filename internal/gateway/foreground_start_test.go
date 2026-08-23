package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func TestStartReturnsOwnerTransitionWhenOwnershipLeaseIsHeldWithoutPublishedOwner(t *testing.T) {
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

	result, err := start(context.Background(), nil, nil, StartHooks{})

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind() != StartResultOwnerTransition {
		t.Fatalf("start kind = %s, want %s", result.Kind(), StartResultOwnerTransition)
	}
}

func TestServeRejectsOwnershipLeaseContention(t *testing.T) {
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

	err = serve(context.Background(), nil, nil, nil)

	if err == nil || !strings.Contains(err.Error(), "gateway owner already running") {
		t.Fatalf("serve error = %v, want owner already running", err)
	}
}

func TestExecuteStartLoopCarriesInvokingWorkingDirectory(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var got string
	result, err := executeStartLoop(context.Background(), StartHooks{}, func(_ context.Context, request StartRequest) (StartResult, error) {
		got = request.WorkingDirectory
		return AlreadyRunning{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind() != StartResultAlreadyRunning || got != want {
		t.Fatalf("result = %#v, working directory = %q, want %q", result, got, want)
	}
}

func TestStartRoutesToExistingServeOwner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".seamless-cors")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "upstreams.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	coord := newCoordinator(filepath.Join(configDir, "runtime"))
	lease, acquired, err := coord.AcquireOwnershipLease()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("test did not acquire Gateway Ownership")
	}
	settings := &lifecycleTestSystemSettings{
		services: []managedpac.Service{{Name: "Wi-Fi", Ownership: managedpac.OwnershipEmpty}},
	}
	owner, err := newOwnerWithCoordinator(settings, emptyTestUserCA{}, coord)
	if err != nil {
		t.Fatal(err)
	}
	owner.lifecycle.globalUpstreamListPath = filepath.Join(configDir, "upstreams.txt")
	owner.lease = lease
	ready := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		runDone <- owner.Run(context.Background(), func(context.Context) error {
			close(ready)
			return nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("serve owner was not published")
	}

	result, err := Start(context.Background(), StartHooks{ConfirmManagedPAC: func(context.Context, ManagedPACConsentDetail) (bool, error) {
		return true, nil
	}})

	if err != nil {
		t.Fatal(err)
	}
	if result.Kind() != StartResultStarted {
		t.Fatalf("routed start = %#v", result)
	}
	if target, err := discover(); err != nil || target.kind != targetActive {
		t.Fatalf("serve owner after routed start = %#v, %v", target, err)
	}
	if _, err := owner.lifecycle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = owner.router.Close(context.Background())
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve owner did not stop")
	}
}
