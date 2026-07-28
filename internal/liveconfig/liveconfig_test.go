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
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeFile(t, secondDomainPath, "second.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	if initial.CATrusted() {
		t.Fatal("initial CA trust = true")
	}
	if initial.DomainListEntriesRevision() != 1 {
		t.Fatalf("initial Domain List Entries Revision = %d", initial.DomainListEntriesRevision())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, secondDomainPath, true)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live := event.Snapshot
	if live.DomainListPath() != secondDomainPath {
		t.Fatalf("domain list path = %q", live.DomainListPath())
	}
	entries := live.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "second.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if live.DomainListEntriesRevision() != 2 {
		t.Fatalf("Domain List Entries Revision after path and entries change = %d", live.DomainListEntriesRevision())
	}
	if live.CATrusted() {
		t.Fatal("restart-applied CA trust was hot-applied")
	}
	if !live.CATrustPending() {
		t.Fatal("CA trust change is not pending")
	}

	cached := source.Current()
	if cached.DomainListPath() != secondDomainPath {
		t.Fatalf("cached domain list path = %q", cached.DomainListPath())
	}

	writeFile(t, secondDomainPath, "second-updated.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live = event.Snapshot
	if live.CATrusted() {
		t.Fatal("Domain List reload hot-applied CA trust")
	}
	if !live.CATrustPending() {
		t.Fatal("CA trust change is not pending after Domain List reload")
	}
	if live.DomainListEntriesRevision() != 3 {
		t.Fatalf("Domain List Entries Revision after entry change = %d", live.DomainListEntriesRevision())
	}

	writeConfig(t, configPath, secondDomainPath, false)
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.CATrustPending() {
		t.Fatal("CA trust change remains pending after revert")
	}
	if event.Snapshot.DomainListEntriesRevision() != 3 {
		t.Fatalf("pending lifecycle change advanced Domain List Entries Revision to %d", event.Snapshot.DomainListEntriesRevision())
	}
}

func TestSnapshotDomainListIsImmutable(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "https://example.test:443\n")
	writeConfig(t, configPath, domainPath, false)

	snapshot, err := liveconfig.LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	exposed := snapshot.DomainList()
	exposed.OriginSelectors[0].Hostname = "changed.example.test"
	exposed.OriginSelectors[0].Port = "8443"

	retained := snapshot.DomainList().OriginSelectors[0]
	if retained.Hostname != "example.test" || retained.Port != "443" {
		t.Fatalf("snapshot Domain List was mutated through exposed fields: %#v", retained)
	}
}

func TestWatchPublishesOnlySemanticDomainListChanges(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "api.example.test\nhttps://secure.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, domainPath, "# same routes, reordered\nhttps://secure.example.test\nAPI.EXAMPLE.TEST\napi.example.test # duplicate\n")
	assertNoEvent(t, events, 300*time.Millisecond)

	writeFile(t, domainPath, "changed.example.test\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "changed.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Snapshot.DomainListEntriesRevision() != 2 {
		t.Fatalf("Domain List Entries Revision = %d", event.Snapshot.DomainListEntriesRevision())
	}
}

func TestWatchDoesNotAdvanceDomainListEntriesRevisionForPathOnlyChange(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "api.example.test\n")
	writeFile(t, secondDomainPath, "# same entries\nAPI.EXAMPLE.TEST\n")
	writeConfig(t, configPath, firstDomainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := source.Current()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, secondDomainPath, false)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Snapshot.DomainListPath() != secondDomainPath {
		t.Fatalf("Domain List path = %q", event.Snapshot.DomainListPath())
	}
	if event.Snapshot.DomainListEntriesRevision() != initial.DomainListEntriesRevision() {
		t.Fatalf(
			"path-only change advanced Domain List Entries Revision from %d to %d",
			initial.DomainListEntriesRevision(),
			event.Snapshot.DomainListEntriesRevision(),
		)
	}
}

func TestWatchAppliesDomainWarningsAndRecovers(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, domainPath, "https://invalid.example.test/path\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if len(event.Snapshot.DomainList().DomainSelectors) != 0 ||
		len(event.Snapshot.DomainList().Warnings) != 1 {
		t.Fatalf("invalid edit snapshot = %#v", event.Snapshot)
	}

	writeFile(t, domainPath, "recovered.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "recovered.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if warnings := event.Snapshot.DomainList().Warnings; len(warnings) != 0 {
		t.Fatalf("warnings after recovery = %#v", warnings)
	}
}

func TestWatchPublishesDomainWarningsDespiteConfigNoise(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)
	writeFile(t, domainPath, "https://invalid.example.test/path\n")
	writeFile(t, configPath, "# noise\ndomain-list: "+domainPath+"\nca-trusted: false\n")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if warnings := event.Snapshot.DomainList().Warnings; len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestWatchIgnoresSiblingEventsAndHandlesTargetReplacement(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	temporaryPath := filepath.Join(home, ".domains.txt.tmp")
	writeFile(t, temporaryPath, "replacement.example.test\n")
	assertNoEvent(t, events, 300*time.Millisecond)
	if err := os.Remove(domainPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporaryPath, domainPath); err != nil {
		t.Fatal(err)
	}

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "replacement.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWatchCoalescesChangesWhileConsumerIsBusy(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, domainPath, "first.example.test\n")
	waitForCachedDomain(t, source, "first.example.test")
	writeFile(t, domainPath, "latest.example.test\n")
	waitForCachedDomain(t, source, "latest.example.test")

	first := waitForEvent(t, events)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	firstEntries := first.Snapshot.DomainList().DomainSelectors
	if len(firstEntries) != 1 || firstEntries[0].Hostname != "first.example.test" {
		t.Fatalf("first entries = %#v", firstEntries)
	}

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Snapshot.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "latest.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Snapshot.DomainListEntriesRevision() != 3 {
		t.Fatalf("coalesced Domain List Entries Revision = %d", event.Snapshot.DomainListEntriesRevision())
	}
}

func TestReloadReestablishesSnapshotAndLifecycleBaselineBeforeWatch(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeFile(t, secondDomainPath, "second.example.test\n")
	writeConfig(t, configPath, firstDomainPath, true)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, configPath, secondDomainPath, false)
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
	if reloaded.DomainListEntriesRevision() != 2 {
		t.Fatalf("reloaded Domain List Entries Revision = %d", reloaded.DomainListEntriesRevision())
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := watchSource(ctx, source)
	assertNoEvent(t, events, 300*time.Millisecond)
	cancel()
}

func TestReloadFailsAfterWatchStarts(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "api.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)
	writeFile(t, domainPath, "changed.example.test\n")
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
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "first.example.test\n")
	writeConfig(t, configPath, domainPath, false)
	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeFile(t, domainPath, "next.example.test\nhttps://bad.example.test/path\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	cached := source.Current()
	entries := cached.DomainList().DomainSelectors
	if len(entries) != 1 || entries[0].Hostname != "next.example.test" {
		t.Fatalf("cached entries = %#v", entries)
	}
	if warnings := cached.DomainList().Warnings; len(warnings) != 1 {
		t.Fatalf("cached warnings = %#v", warnings)
	}
}

func TestWatchTreatsMissingLiveDomainListAsFatal(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	missingDomainPath := filepath.Join(home, "missing-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)
	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchSource(ctx, source)

	writeConfig(t, configPath, missingDomainPath, false)
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
}

func TestWatchTreatsUnreadableLiveConfigAsFatal(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "first.example.test\n")
	writeConfig(t, configPath, domainPath, false)
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
	domainPath := filepath.Join(home, "domains.txt")
	writeFile(t, domainPath, "api.example.test\n")
	writeFile(t, configPath, "unknown-setting: ignored\ndomain-list: "+domainPath+"\n")

	loaded, err := liveconfig.LoadExisting(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DomainListPath() != domainPath {
		t.Fatalf("domain path = %q", loaded.DomainListPath())
	}
	if loaded.CATrusted() {
		t.Fatal("omitted ca-trusted should default to false")
	}
}

func TestLoadRejectsSymlinkedConfigFile(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	realConfigPath := filepath.Join(home, "real-config.yaml")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "api.example.test\n")
	writeConfig(t, realConfigPath, domainPath, false)
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
	if loaded.DomainListPath() != filepath.Join(home, ".seamless-cors", "domains.txt") {
		t.Fatalf("domain path = %q", loaded.DomainListPath())
	}
	configText, err := os.ReadFile(loaded.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(configText, []byte("# One domain or origin per line.")) {
		t.Fatalf("generated config is not commented:\n%s", configText)
	}
	if !bytes.Contains(configText, []byte("ca-trusted: false")) {
		t.Fatalf("generated config does not disable trusted HTTPS:\n%s", configText)
	}
}

func TestOpenCreatesMissingConfiguredDomainList(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "nested", "config", "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, configPath, "domain-list: "+domainPath+"\n")

	source, err := liveconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if entries := source.Current().DomainList().DomainSelectors; len(entries) != 0 {
		t.Fatalf("bootstrapped Domain List entries = %#v", entries)
	}
	domainText, err := os.ReadFile(domainPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(domainText) != "# One domain or origin per line.\n# api.dev.example.com\n" {
		t.Fatalf("bootstrapped Domain List = %q", domainText)
	}
}

func TestLoadExistingDoesNotCreateMissingConfiguredDomainList(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "nested", "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, configPath, "domain-list: "+domainPath+"\n")

	if _, err := liveconfig.LoadExisting(configPath); !os.IsNotExist(err) {
		t.Fatalf("load error = %v, want not-exist", err)
	}
	if _, err := os.Stat(filepath.Dir(domainPath)); !os.IsNotExist(err) {
		t.Fatalf("passive load created Domain List parent: %v", err)
	}
}

func writeConfig(t *testing.T, path, domainPath string, caTrusted bool) {
	t.Helper()
	writeFile(t, path, "domain-list: "+domainPath+"\nca-trusted: "+map[bool]string{true: "true", false: "false"}[caTrusted]+"\n")
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

func waitForCachedDomain(t *testing.T, source *liveconfig.Source, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries := source.Current().DomainList().DomainSelectors
		if len(entries) == 1 && entries[0].Hostname == host {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cached Domain List did not become %q", host)
}
