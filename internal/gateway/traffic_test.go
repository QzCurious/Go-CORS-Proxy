package gateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"seamless-cors/internal/liveconfig"
)

func TestPACVersionFollowsDomainListEntriesRevision(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeTrafficTestFile(t, firstDomainPath, "api.example.test\n")
	writeTrafficTestFile(t, secondDomainPath, "# same entries\nAPI.EXAMPLE.TEST\n")
	writeTrafficTestFile(t, configPath, "domain-list: "+firstDomainPath+"\nca-trusted: false\n")

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

	writeTrafficTestFile(t, configPath, "domain-list: "+secondDomainPath+"\nca-trusted: false\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.DomainList == secondDomainPath
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("path-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondDomainPath, "API.EXAMPLE.TEST\nhttps://bad.example.test/path\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return len(state.DomainListWarnings) == 1
	})
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("warning-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondDomainPath, "changed.example.test\n")
	waitForTrafficConfig(t, runtime, errs, func(state runtimeState) bool {
		return state.DomainCount == 1 && runtime.PACURL() != initialPACURL
	})
	if runtime.PACURL() == initialPACURL {
		t.Fatalf("Domain List Entries Revision change did not advance PAC URL %q", initialPACURL)
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
