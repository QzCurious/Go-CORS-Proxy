package gateway

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartReturnsOwnerAlreadyRunningWhenOwnershipLeaseIsHeld(t *testing.T) {
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
	if result.Kind != StartResultOwnerAlreadyRunning {
		t.Fatalf("start kind = %s, want %s", result.Kind, StartResultOwnerAlreadyRunning)
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
