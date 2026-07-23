package gateway

import (
	"context"
	"io"
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

	source, initial, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
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
	events := source.Watch(ctx)
	initialPACURL := runtime.PACURL()

	writeTrafficTestFile(t, configPath, "domain-list: "+secondDomainPath+"\nca-trusted: false\n")
	pathOnly := waitForTrafficConfigEvent(t, events)
	runtime.applyLiveConfig(pathOnly.Config)
	if runtime.PACURL() != initialPACURL {
		t.Fatalf("path-only change advanced PAC URL from %q to %q", initialPACURL, runtime.PACURL())
	}

	writeTrafficTestFile(t, secondDomainPath, "changed.example.test\n")
	entriesChanged := waitForTrafficConfigEvent(t, events)
	runtime.applyLiveConfig(entriesChanged.Config)
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

func waitForTrafficConfigEvent(t *testing.T, events <-chan liveconfig.Event) liveconfig.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("Live Configuration event channel closed")
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Live Configuration event")
		return liveconfig.Event{}
	}
}
