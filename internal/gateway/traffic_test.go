package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
)

func TestPACVersionFollowsUpstreamListEntriesRevision(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	secondUpstreamPath := filepath.Join(home, "second-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeTrafficTestFile(t, firstUpstreamPath, "api.example.test\n")
	writeTrafficTestFile(t, secondUpstreamPath, "# same entries\nAPI.EXAMPLE.TEST\n")
	writeTrafficTestFile(t, configPath, "upstream-list: "+firstUpstreamPath+"\nca-trusted: false\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	runtime, err := newRuntime(source, initial)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range runtime.listeners {
			_ = listener.Close()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan serverError, 1)
	go runtime.watchLiveConfig(ctx, errs)
	initialPACURL := runtime.PACURL()

	writeTrafficTestFile(t, configPath, "upstream-list: "+secondUpstreamPath+"\nca-trusted: false\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.UpstreamList == secondUpstreamPath
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("path-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondUpstreamPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.UpstreamListWarnings) == 1
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("warning-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondUpstreamPath, "changed.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.UpstreamCount == 1 && runtime.PACURL() != initialPACURL
	})
	if runtime.PACURL() == initialPACURL {
		t.Fatalf("Upstream List Entries Revision change did not advance PAC URL %q", initialPACURL)
	}
}

func writeTrafficTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForTrafficConfig(t *testing.T, runtime *trafficRuntime, errs <-chan serverError, ready func(runtimeState) bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready(runtime.snapshot()) {
			return
		}
		select {
		case err := <-errs:
			t.Fatal(err.err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for Live Configuration update")
		}
	}
}
