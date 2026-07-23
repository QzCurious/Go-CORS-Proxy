package liveconfig_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seamless-cors/internal/liveconfig"
)

func TestWatchEmitsEffectiveConfigAndKeepsLifecycleChangesPending(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	secondDomainPath := filepath.Join(home, "second-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeFile(t, secondDomainPath, "second.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)

	source, initial, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if initial.CATrusted() {
		t.Fatal("initial CA trust = true")
	}
	if initial.DomainListEntriesRevision() != 1 {
		t.Fatalf("initial Domain List Entries Revision = %d", initial.DomainListEntriesRevision())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeConfig(t, configPath, secondDomainPath, true)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live := event.Config
	if live.DomainListPath() != secondDomainPath {
		t.Fatalf("domain list path = %q", live.DomainListPath())
	}
	entries := live.Entries()
	if len(entries) != 1 || entries[0].Host != "second.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if live.DomainListEntriesRevision() != 2 {
		t.Fatalf("Domain List Entries Revision after path and entries change = %d", live.DomainListEntriesRevision())
	}
	if live.CATrusted() {
		t.Fatal("restart-applied CA trust was hot-applied")
	}
	if got := strings.Join(live.PendingLifecycle(), ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle = %q", got)
	}

	cached := source.Config()
	if cached.DomainListPath() != secondDomainPath {
		t.Fatalf("cached domain list path = %q", cached.DomainListPath())
	}

	writeFile(t, secondDomainPath, "second-updated.example.test\n")
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	live = event.Config
	if live.CATrusted() {
		t.Fatal("Domain List reload hot-applied CA trust")
	}
	if got := strings.Join(live.PendingLifecycle(), ","); got != "ca-trusted" {
		t.Fatalf("pending lifecycle after Domain List reload = %q", got)
	}
	if live.DomainListEntriesRevision() != 3 {
		t.Fatalf("Domain List Entries Revision after entry change = %d", live.DomainListEntriesRevision())
	}

	writeConfig(t, configPath, secondDomainPath, false)
	event = waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if got := strings.Join(event.Config.PendingLifecycle(), ","); got != "" {
		t.Fatalf("pending lifecycle after revert = %q", got)
	}
	if event.Config.DomainListEntriesRevision() != 3 {
		t.Fatalf("pending lifecycle change advanced Domain List Entries Revision to %d", event.Config.DomainListEntriesRevision())
	}
}

func TestWatchPublishesOnlySemanticDomainListChanges(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "api.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeFile(t, domainPath, "# same routes\nAPI.EXAMPLE.TEST\napi.example.test # duplicate\n")
	assertNoEvent(t, events, 300*time.Millisecond)

	writeFile(t, domainPath, "changed.example.test\n")
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Config.Entries()
	if len(entries) != 1 || entries[0].Host != "changed.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Config.DomainListEntriesRevision() != 2 {
		t.Fatalf("Domain List Entries Revision = %d", event.Config.DomainListEntriesRevision())
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

	source, initial, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeConfig(t, configPath, secondDomainPath, false)
	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	if event.Config.DomainListPath() != secondDomainPath {
		t.Fatalf("Domain List path = %q", event.Config.DomainListPath())
	}
	if event.Config.DomainListEntriesRevision() != initial.DomainListEntriesRevision() {
		t.Fatalf(
			"path-only change advanced Domain List Entries Revision from %d to %d",
			initial.DomainListEntriesRevision(),
			event.Config.DomainListEntriesRevision(),
		)
	}
}

func TestWatchConfirmsInvalidContentBeforeFailing(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeFile(t, domainPath, "https://*.invalid.example.test\n")
	time.Sleep(300 * time.Millisecond)
	writeFile(t, domainPath, "recovered.example.test\n")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatalf("transient invalid edit became fatal: %v", event.Err)
	}
	entries := event.Config.Entries()
	if len(entries) != 1 || entries[0].Host != "recovered.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWatchConfirmsInvalidSourcesIndependently(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)
	writeFile(t, domainPath, "https://*.invalid.example.test\n")

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(1800 * time.Millisecond)
	for edit := 0; ; edit++ {
		select {
		case event := <-events:
			if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
				t.Fatalf("event error = %v", event.Err)
			}
			return
		case <-ticker.C:
			writeFile(t, configPath, fmt.Sprintf("# edit %d\ndomain-list: %s\nca-trusted: false\n", edit, domainPath))
		case <-deadline:
			t.Fatal("Config File noise postponed invalid Domain List confirmation")
		}
	}
}

func TestWatchIgnoresSiblingEventsAndHandlesTargetReplacement(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

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
	entries := event.Config.Entries()
	if len(entries) != 1 || entries[0].Host != "replacement.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWatchKeepsOnlyLatestUnconsumedSnapshot(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "initial.example.test\n")
	writeConfig(t, configPath, domainPath, false)

	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeFile(t, domainPath, "first.example.test\n")
	waitForCachedDomain(t, source, "first.example.test")
	writeFile(t, domainPath, "latest.example.test\n")
	waitForCachedDomain(t, source, "latest.example.test")

	event := waitForEvent(t, events)
	if event.Err != nil {
		t.Fatal(event.Err)
	}
	entries := event.Config.Entries()
	if len(entries) != 1 || entries[0].Host != "latest.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
	if event.Config.DomainListEntriesRevision() != 3 {
		t.Fatalf("coalesced Domain List Entries Revision = %d", event.Config.DomainListEntriesRevision())
	}
}

func TestWatchEmitsErrorWithoutReplacingCachedConfig(t *testing.T) {
	home := t.TempDir()
	domainPath := filepath.Join(home, "domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, domainPath, "first.example.test\n")
	writeConfig(t, configPath, domainPath, false)
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

	writeFile(t, domainPath, "https://*.bad.example.test\n")
	assertNoEvent(t, events, 500*time.Millisecond)
	event := waitForEvent(t, events)
	if event.Err == nil || !strings.Contains(event.Err.Error(), "Fatal Domain List Error") {
		t.Fatalf("event error = %v", event.Err)
	}
	cached := source.Config()
	entries := cached.Entries()
	if len(entries) != 1 || entries[0].Host != "first.example.test" {
		t.Fatalf("cached entries = %#v", entries)
	}
}

func TestWatchTreatsMissingLiveDomainListAsFatal(t *testing.T) {
	home := t.TempDir()
	firstDomainPath := filepath.Join(home, "first-domains.txt")
	missingDomainPath := filepath.Join(home, "missing-domains.txt")
	configPath := filepath.Join(home, "config.yaml")
	writeFile(t, firstDomainPath, "first.example.test\n")
	writeConfig(t, configPath, firstDomainPath, false)
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

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
	source, _, err := liveconfig.LoadOrBootstrap(configPath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := source.Watch(ctx)

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

func TestLoadOrBootstrapCreatesCommentedDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out bytes.Buffer

	_, loaded, err := liveconfig.LoadOrBootstrap("", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.CATrusted() {
		t.Fatal("ca-trusted default should enable trusted HTTPS")
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
	if !bytes.Contains(out.Bytes(), []byte("Created:")) {
		t.Fatalf("bootstrap output = %q", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("Add at least one domain")) {
		t.Fatalf("bootstrap output treated empty Domain List as invalid: %q", out.String())
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

func waitForEvent(t *testing.T, events <-chan liveconfig.Event) liveconfig.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return liveconfig.Event{}
	}
}

func assertNoEvent(t *testing.T, events <-chan liveconfig.Event, duration time.Duration) {
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
		entries := source.Config().Entries()
		if len(entries) == 1 && entries[0].Host == host {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cached Domain List did not become %q", host)
}
