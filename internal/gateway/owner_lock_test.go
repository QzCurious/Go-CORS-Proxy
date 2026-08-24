package gateway

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const ownerLockHelperRuntimeDir = "SEAMLESS_CORS_OWNER_LOCK_HELPER_RUNTIME_DIR"

func TestGatewayOwnerLockIsExclusiveAndReleased(t *testing.T) {
	runtimeDir := t.TempDir()
	first := newCoordinator(runtimeDir)
	second := newCoordinator(runtimeDir)

	lock, acquired, err := first.TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("first coordinator did not acquire ownership lock")
	}

	contender, acquired, err := second.TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		_ = contender.Release()
		t.Fatal("second coordinator acquired held ownership lock")
	}

	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("repeated release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, ownerLockFileName)); err != nil {
		t.Fatalf("lock file did not remain after release: %v", err)
	}

	contender, acquired, err = second.TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("remaining lock file prevented reacquisition")
	}
	if err := contender.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayOwnerLockIsReleasedWhenHoldingProcessExits(t *testing.T) {
	if runtimeDir := os.Getenv(ownerLockHelperRuntimeDir); runtimeDir != "" {
		lock, acquired, err := newCoordinator(runtimeDir).TryAcquireOwnerLock()
		if err != nil || !acquired {
			fmt.Fprintf(os.Stderr, "acquire owner lock = %t, %v\n", acquired, err)
			os.Exit(2)
		}
		_ = lock
		fmt.Println("locked")
		select {}
	}

	runtimeDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGatewayOwnerLockIsReleasedWhenHoldingProcessExits$")
	cmd.Env = append(os.Environ(), ownerLockHelperRuntimeDir+"="+runtimeDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper readiness = %q, %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed helper exited successfully")
	}

	lock, acquired, err := newCoordinator(runtimeDir).TryAcquireOwnerLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("owner lock remained held after holding process exited")
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}
