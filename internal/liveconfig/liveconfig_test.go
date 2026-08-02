package liveconfig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
)

const eventTimeout = 4 * time.Second

type liveConfigEvent struct {
	Snapshot liveconfig.Snapshot
	Err      error
}

func TestCreateBootstrapsOnlyUpstreamListWithoutReadingIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	config, err := liveconfig.Create()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".seamless-cors", "config.yaml")
	upstreamPath := filepath.Join(home, ".seamless-cors", "upstreams.txt")

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("Create produced config.yaml: %v", err)
	}
	data, err := os.ReadFile(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "One upstream host or origin per line") {
		t.Fatalf("upstreams.txt = %q", data)
	}

	writeFile(t, upstreamPath, "bad/path\n")
	if _, err := liveconfig.Create(); err != nil {
		t.Fatalf("Create validated existing source: %v", err)
	}
	if _, err := config.Snapshot(); err != nil {
		t.Fatalf("Snapshot rejected warning-only Upstream List: %v", err)
	}
}

func TestSnapshotLoadsOnceAndReturnsCachedSemanticValue(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "first.example.test\n")

	first, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, upstreamPath, "second.example.test\n")
	second, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	entries := second.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "first.example.test" {
		t.Fatalf("cached entries = %#v", entries)
	}
	if first.UpstreamListEntriesRevision() != second.UpstreamListEntriesRevision() {
		t.Fatal("cached snapshot revision changed")
	}
}

func TestSnapshotUpstreamListIsImmutable(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "api.example.test\nbad/path\n")

	snapshot, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	list := snapshot.UpstreamList()
	hosts := list.Entries().HostSelectors()
	hosts[0].Hostname = "mutated.example.test"
	warnings := list.Warnings()
	warnings[0].Diagnostic = "mutated"

	reloaded := snapshot.UpstreamList()
	if reloaded.Entries().HostSelectors()[0].Hostname != "api.example.test" {
		t.Fatalf("snapshot host mutated to %q", reloaded.Entries().HostSelectors()[0].Hostname)
	}
	if reloaded.Warnings()[0].Diagnostic == "mutated" {
		t.Fatal("snapshot warning mutated")
	}
}

func TestObservePublishesInitialSnapshotWhenCacheIsEmpty(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "api.example.test\n")

	ctx, cancel := context.WithCancel(context.Background())
	events := observeConfig(ctx, config)
	event := waitForEvent(t, events)
	cancel()

	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "api.example.test" {
		t.Fatalf("initial entries = %#v", entries)
	}
}

func TestObserveReconcilesCachedSnapshotBeforeWaitingForEvents(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "first.example.test\n")
	initial, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, upstreamPath, "second.example.test\n")

	ctx, cancel := context.WithCancel(context.Background())
	events := observeConfig(ctx, config)
	event := waitForEvent(t, events)
	cancel()

	entries := event.Snapshot.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "second.example.test" {
		t.Fatalf("reconciled entries = %#v", entries)
	}
	if got, want := event.Snapshot.UpstreamListEntriesRevision(), initial.UpstreamListEntriesRevision()+1; got != want {
		t.Fatalf("revision = %d, want %d", got, want)
	}
}

func TestObservePublishesSemanticChangesAndUpdatesCache(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "first.example.test\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := observeConfig(ctx, config)
	initial := waitForEvent(t, events).Snapshot

	writeFile(t, upstreamPath, "second.example.test\n")
	changed := waitForEvent(t, events).Snapshot
	entries := changed.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "second.example.test" {
		t.Fatalf("changed entries = %#v", entries)
	}
	if got, want := changed.UpstreamListEntriesRevision(), initial.UpstreamListEntriesRevision()+1; got != want {
		t.Fatalf("revision = %d, want %d", got, want)
	}

	cached, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if cached.UpstreamListEntriesRevision() != changed.UpstreamListEntriesRevision() {
		t.Fatal("Snapshot did not return observed cache")
	}
}

func TestObserveIgnoresRepresentationOnlyChanges(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "api.example.test\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := observeConfig(ctx, config)
	_ = waitForEvent(t, events)

	writeFile(t, upstreamPath, "# comment\napi.example.test\napi.example.test\n")
	assertNoEvent(t, events, 500*time.Millisecond)
}

func TestObservePublishesWarningsWithoutAdvancingEntryRevision(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	writeFile(t, upstreamPath, "api.example.test\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := observeConfig(ctx, config)
	initial := waitForEvent(t, events).Snapshot

	writeFile(t, upstreamPath, "api.example.test\nbad/path\n")
	warned := waitForEvent(t, events).Snapshot
	if len(warned.UpstreamList().Warnings()) != 1 {
		t.Fatalf("warnings = %#v", warned.UpstreamList().Warnings())
	}
	if warned.UpstreamListEntriesRevision() != initial.UpstreamListEntriesRevision() {
		t.Fatal("warning advanced Upstream List Entries Revision")
	}
}

func TestObserveSurvivesTransientMissingUpstreamList(t *testing.T) {
	config, _, upstreamPath := createConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := observeConfig(ctx, config)
	_ = waitForEvent(t, events)

	if err := os.Remove(upstreamPath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	writeFile(t, upstreamPath, "recovered.example.test\n")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "recovered.example.test" {
		t.Fatalf("recovered entries = %#v", entries)
	}
}

func TestObserveTreatsMissingUpstreamListAsFatal(t *testing.T) {
	config, _, upstreamPath := createConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := observeConfig(ctx, config)
	_ = waitForEvent(t, events)

	if err := os.Remove(upstreamPath); err != nil {
		t.Fatal(err)
	}
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Upstream List Error") {
		t.Fatalf("fatal event = %#v", event)
	}
}

func TestObserveMayOnlyBeCalledOnce(t *testing.T) {
	config, _, _ := createConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	events := observeConfig(ctx, config)
	_ = waitForEvent(t, events)
	cancel()
	_ = waitForEvent(t, events)

	err := config.Observe(context.Background(), func(liveconfig.Snapshot) {})
	if err == nil || !strings.Contains(err.Error(), "only be called once") {
		t.Fatalf("second Observe error = %v", err)
	}
}

func TestLegacyConfigFileIsIgnored(t *testing.T) {
	config, configPath, upstreamPath := createConfig(t)
	writeFile(t, configPath, "upstream-list: /tmp/ignored.txt\nca-trusted: true\n")
	writeFile(t, upstreamPath, "fixed.example.test\n")

	snapshot, err := config.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UpstreamListPath() != upstreamPath {
		t.Fatalf("Upstream List path = %q, want %q", snapshot.UpstreamListPath(), upstreamPath)
	}
	entries := snapshot.UpstreamList().Entries().HostSelectors()
	if len(entries) != 1 || entries[0].Hostname != "fixed.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestSnapshotRejectsSymlinkedUpstreamList(t *testing.T) {
	config, _, upstreamPath := createConfig(t)
	target := filepath.Join(t.TempDir(), "upstreams.txt")
	writeFile(t, target, "api.example.test\n")
	if err := os.Remove(upstreamPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, upstreamPath); err != nil {
		t.Fatal(err)
	}

	if _, err := config.Snapshot(); err == nil || !strings.Contains(err.Error(), "ordinary file") {
		t.Fatalf("Snapshot error = %v", err)
	}
}

func createConfig(t *testing.T) (*liveconfig.Config, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, err := liveconfig.Create()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".seamless-cors")
	return config, filepath.Join(dir, "config.yaml"), filepath.Join(dir, "upstreams.txt")
}

func observeConfig(ctx context.Context, config *liveconfig.Config) <-chan liveConfigEvent {
	events := make(chan liveConfigEvent, 1)
	go func() {
		err := config.Observe(ctx, func(snapshot liveconfig.Snapshot) {
			events <- liveConfigEvent{Snapshot: snapshot}
		})
		events <- liveConfigEvent{Err: err}
		close(events)
	}()
	return events
}

func waitForEvent(t *testing.T, events <-chan liveConfigEvent) liveConfigEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("Live Configuration events closed unexpectedly")
		}
		return event
	case <-time.After(eventTimeout):
		t.Fatal("timed out waiting for Live Configuration event")
		return liveConfigEvent{}
	}
}

func assertNoEvent(t *testing.T, events <-chan liveConfigEvent, duration time.Duration) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected Live Configuration event: %#v", event)
	case <-time.After(duration):
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
