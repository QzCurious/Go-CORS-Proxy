package liveconfig_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/liveconfig"
)

func TestWatchEmitsEffectiveConfigAndKeepsLifecycleChangesPending(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	secondUpstreamPath := filepath.Join(home, "second-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstUpstreamPath, "first.example.test\n")
	writeFile(t, secondUpstreamPath, "second.example.test\n")
	writeConfig(t, configPath, firstUpstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	if initial.CATrusted() {
		t.Fatal("initial CA trust = true")
	}
	if initial.UpstreamListEntriesRevision() != 1 {
		t.Fatalf("initial Upstream List Entries Revision = %d", initial.UpstreamListEntriesRevision())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, secondUpstreamPath, true)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live := event.Snapshot
	if live.UpstreamListPath() != secondUpstreamPath {
		t.Fatalf("upstream list path = %q", live.UpstreamListPath())
	}
	entries := live.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "second.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if live.UpstreamListEntriesRevision() != 2 {
		t.Fatalf("Upstream List Entries Revision after path and entries change = %d", live.UpstreamListEntriesRevision())
	}
	if live.CATrusted() {
		t.Fatal("restart-applied CA trust was hot-applied")
	}
	if !live.CATrustPending() {
		t.Fatal("CA trust change is not pending")
	}

	cached := source.Current()
	if cached.UpstreamListPath() != secondUpstreamPath {
		t.Fatalf("cached upstream list path = %q", cached.UpstreamListPath())
	}

	writeFile(t, secondUpstreamPath, "second-updated.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live = event.Snapshot
	if live.CATrusted() {
		t.Fatal("Upstream List reload hot-applied CA trust")
	}
	if !live.CATrustPending() {
		t.Fatal("CA trust change is not pending after Upstream List reload")
	}
	if live.UpstreamListEntriesRevision() != 3 {
		t.Fatalf("Upstream List Entries Revision after entry change = %d", live.UpstreamListEntriesRevision())
	}

	writeConfig(t, configPath, secondUpstreamPath, false)
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.CATrustPending() {
		t.Fatal("CA trust change remains pending after revert")
	}
	if event.Snapshot.UpstreamListEntriesRevision() != 3 {
		t.Fatalf("pending lifecycle change advanced Upstream List Entries Revision to %d", event.Snapshot.UpstreamListEntriesRevision())
	}
}

func TestSnapshotUpstreamListIsImmutable(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "https://example.test:443\n")
	writeConfig(t, configPath, upstreamPath, false)

	snapshot, err := liveconfig.LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	exposed := snapshot.UpstreamList()
	exposed.OriginSelectors[0].Hostname = "changed.example.test"
	exposed.OriginSelectors[0].Port = "8443"

	retained := snapshot.UpstreamList().OriginSelectors[0]
	if retained.Hostname != "example.test" || retained.Port != "443" {
		t.Fatalf("snapshot Upstream List was mutated through exposed fields: %#v", retained)
	}
}

func TestWatchPublishesOnlySemanticUpstreamListChanges(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "api.example.test\nhttps://secure.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, upstreamPath, "# same routes, reordered\nhttps://secure.example.test\nAPI.EXAMPLE.TEST\napi.example.test # duplicate\n")
	assertNoEvent(t, events, 300*time.Millisecond)

	writeFile(t, upstreamPath, "changed.example.test\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "changed.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Snapshot.UpstreamListEntriesRevision() != 2 {
		t.Fatalf("Upstream List Entries Revision = %d", event.Snapshot.UpstreamListEntriesRevision())
	}
}

func TestWatchDoesNotAdvanceUpstreamListEntriesRevisionForPathOnlyChange(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	secondUpstreamPath := filepath.Join(home, "second-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstUpstreamPath, "api.example.test\n")
	writeFile(t, secondUpstreamPath, "# same entries\nAPI.EXAMPLE.TEST\n")
	writeConfig(t, configPath, firstUpstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, secondUpstreamPath, false)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.UpstreamListPath() != secondUpstreamPath {
		t.Fatalf("Upstream List path = %q", event.Snapshot.UpstreamListPath())
	}
	if event.Snapshot.UpstreamListEntriesRevision() != initial.UpstreamListEntriesRevision() {
		t.Fatalf(
			"path-only change advanced Upstream List Entries Revision from %d to %d",
			initial.UpstreamListEntriesRevision(),
			event.Snapshot.UpstreamListEntriesRevision(),
		)
	}
}

func TestWatchAppliesUpstreamWarningsAndRecovers(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "initial.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, upstreamPath, "https://invalid.example.test/path\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if len(event.Snapshot.UpstreamList().HostSelectors) != 0 ||
		len(event.Snapshot.UpstreamList().Warnings) != 1 {
		t.Fatalf("invalid edit snapshot = %#v", event.Snapshot)
	}

	writeFile(t, upstreamPath, "recovered.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "recovered.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if warnings := event.Snapshot.UpstreamList().Warnings; len(warnings) != 0 {
		t.Fatalf("warnings after recovery = %#v", warnings)
	}
}

func TestWatchPublishesUpstreamWarningsDespiteConfigNoise(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "initial.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)
	writeFile(t, upstreamPath, "https://invalid.example.test/path\n")
	writeFile(t, configPath, "# noise\nupstream-list: "+upstreamPath+"\nca-trusted: false\n")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if warnings := event.Snapshot.UpstreamList().Warnings; len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestWatchSurvivesTransientEmptyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "initial.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, configPath, "")
	// Keep the invalid state longer than the watcher debounce but shorter than
	// its invalid-source confirmation window.
	time.Sleep(500 * time.Millisecond)
	writeConfig(t, configPath, upstreamPath, false)
	time.Sleep(200 * time.Millisecond)
	writeFile(t, upstreamPath, "recovered.example.test\n")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "recovered.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWatchIgnoresSiblingEventsAndHandlesTargetReplacement(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "initial.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	temporaryPath := filepath.Join(home, ".upstreams.txt.tmp")
	writeFile(t, temporaryPath, "replacement.example.test\n")
	assertNoEvent(t, events, 300*time.Millisecond)
	if err := os.Remove(upstreamPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporaryPath, upstreamPath); err != nil {
		t.Fatal(err)
	}

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "replacement.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWatchCoalescesChangesWhileConsumerIsBusy(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "initial.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, upstreamPath, "first.example.test\n")
	waitForCachedHost(t, source, "first.example.test")
	writeFile(t, upstreamPath, "latest.example.test\n")
	waitForCachedHost(t, source, "latest.example.test")

	first := waitForEvent(t, events)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	firstEntries := first.Snapshot.UpstreamList().HostSelectors
	if len(firstEntries) != 1 || firstEntries[0].Hostname != "first.example.test" {
		t.Fatalf("first entries = %#v", firstEntries)
	}

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "latest.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Snapshot.UpstreamListEntriesRevision() != 3 {
		t.Fatalf("coalesced Upstream List Entries Revision = %d", event.Snapshot.UpstreamListEntriesRevision())
	}
}

func TestReloadReestablishesSnapshotAndLifecycleBaselineBeforeWatch(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	secondUpstreamPath := filepath.Join(home, "second-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstUpstreamPath, "first.example.test\n")
	writeFile(t, secondUpstreamPath, "second.example.test\n")
	writeConfig(t, configPath, firstUpstreamPath, true)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, configPath, secondUpstreamPath, false)
	reloaded, err := source.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CATrusted() {
		t.Fatal("reloaded CA trust = true")
	}
	if reloaded.CATrustPending() {
		t.Fatal("reload left CA trust pending")
	}
	if reloaded.UpstreamListEntriesRevision() != 2 {
		t.Fatalf("reloaded Upstream List Entries Revision = %d", reloaded.UpstreamListEntriesRevision())
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := watchSource(ctx, source)
	assertNoEvent(t, events, 300*time.Millisecond)
	cancel()
}

func TestReloadFailsAfterWatchStarts(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "api.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)
	writeFile(t, upstreamPath, "changed.example.test\n")
	if event := waitForEvent(t, events); event.Err != nil {
		t.Fatal(event.Err)
	}

	if _, err := source.Reload(); err == nil || !strings.Contains(err.Error(), "before Watch") {
		t.Fatalf("Reload error = %v", err)
	}
	cancel()
	for range events {
	}
}

func TestWatchAppliesValidEntriesAlongsideWarnings(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "first.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)
	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, upstreamPath, "next.example.test\nhttps://bad.example.test/path\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	cached := source.Current()
	entries := cached.UpstreamList().HostSelectors
	if len(entries) != 1 || entries[0].Hostname != "next.example.test" {
		t.Fatalf("cached entries = %#v", entries)
	}
	if warnings := cached.UpstreamList().Warnings; len(warnings) != 1 {
		t.Fatalf("cached warnings = %#v", warnings)
	}
}

func TestWatchTreatsMissingLiveUpstreamListAsFatal(t *testing.T) {
	home := t.TempDir()
	firstUpstreamPath := filepath.Join(home, "first-upstreams.txt")
	missingUpstreamPath := filepath.Join(home, "missing-upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstUpstreamPath, "first.example.test\n")
	writeConfig(t, configPath, firstUpstreamPath, false)
	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, missingUpstreamPath, false)
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Upstream List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
}

func TestWatchTreatsUnreadableLiveConfigAsFatal(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "first.example.test\n")
	writeConfig(t, configPath, upstreamPath, false)
	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Config Error") {
		t.Fatalf("event error = %v", event.Err)
	}
}

func TestLoadIgnoresUnknownConfigKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	upstreamPath := filepath.Join(home, "upstreams.txt")
	writeFile(t, upstreamPath, "api.example.test\n")
	writeFile(t, configPath, "unknown-setting: ignored\nupstream-list: "+upstreamPath+"\n")

	loaded, err := liveconfig.LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpstreamListPath() != upstreamPath {
		t.Fatalf("upstream list path = %q", loaded.UpstreamListPath())
	}
	if loaded.CATrusted() {
		t.Fatal("omitted ca-trusted should default to false")
	}
}

func TestLoadRejectsSymlinkedConfigFile(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "upstreams.txt")
	realConfigPath := filepath.Join(home, "real-config.yaml")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, upstreamPath, "api.example.test\n")
	writeConfig(t, realConfigPath, upstreamPath, false)
	if err := os.Symlink(realConfigPath, configPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := liveconfig.LoadExisting(configPath)
	if err == nil || !strings.Contains(err.Error(), "ordinary file") {
		t.Fatalf("load error = %v", err)
	}
}

func TestOpenCreatesCommentedDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	source, err := liveconfig.Open("")
	if err != nil {
		t.Fatal(err)
	}
	loaded := source.Current()
	if loaded.CATrusted() {
		t.Fatal("ca-trusted default should disable trusted HTTPS")
	}
	if loaded.UpstreamListPath() != filepath.Join(home, ".seamless-cors", "upstreams.txt") {
		t.Fatalf("upstream list path = %q", loaded.UpstreamListPath())
	}
	configText, err := os.ReadFile(loaded.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(configText, []byte("# One upstream host or origin per line.")) {
		t.Fatalf("generated config is not commented:\n%s", configText)
	}
	if !bytes.Contains(configText, []byte("ca-trusted: false")) {
		t.Fatalf("generated config does not disable trusted HTTPS:\n%s", configText)
	}
}

func TestOpenCreatesMissingConfiguredUpstreamList(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "nested", "config", "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, configPath, "upstream-list: "+upstreamPath+"\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if entries := source.Current().UpstreamList().HostSelectors; len(entries) != 0 {
		t.Fatalf("bootstrapped Upstream List entries = %#v", entries)
	}
	upstreamText, err := os.ReadFile(upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(upstreamText) != "# One upstream host or origin per line.\n# api.dev.example.com\n" {
		t.Fatalf("bootstrapped Upstream List = %q", upstreamText)
	}
}

func TestLoadExistingDoesNotCreateMissingConfiguredUpstreamList(t *testing.T) {
	home := t.TempDir()
	upstreamPath := filepath.Join(home, "nested", "upstreams.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, configPath, "upstream-list: "+upstreamPath+"\n")

	if _, err := liveconfig.LoadExisting(configPath); !os.IsNotExist(err) {
		t.Fatalf("load error = %v, want not-exist", err)
	}
	if _, err := os.Stat(filepath.Dir(upstreamPath)); !os.IsNotExist(err) {
		t.Fatalf("passive load created Upstream List parent: %v", err)
	}
}

func writeConfig(t *testing.T, path, upstreamPath string, caTrusted bool) {
	t.Helper()
	writeFile(t, path, "upstream-list: "+upstreamPath+"\nca-trusted: "+map[bool]string{true: "true", false: "false"}[caTrusted]+"\n")
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

type liveConfigEvent struct {
	Snapshot liveconfig.Snapshot
	Err      error
}

func watchSource(ctx context.Context, source *liveconfig.Source) <-chan liveConfigEvent {
	events := make(chan liveConfigEvent)
	go func() {
		defer close(events)
		err := source.Watch(ctx, func(snapshot liveconfig.Snapshot) {
			select {
			case events <- liveConfigEvent{Snapshot: snapshot}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case events <- liveConfigEvent{Err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return events
}

func waitForEvent(t *testing.T, events <-chan liveConfigEvent) liveConfigEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return liveConfigEvent{}
	}
}

func assertNoEvent(t *testing.T, events <-chan liveConfigEvent, duration time.Duration) {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		t.Fatalf("unexpected event: %#v", event)
	case <-time.After(duration):
	}
}

func waitForCachedHost(t *testing.T, source *liveconfig.Source, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries := source.Current().UpstreamList().HostSelectors
		if len(entries) == 1 && entries[0].Hostname == host {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cached Upstream List did not become %q", host)
}
