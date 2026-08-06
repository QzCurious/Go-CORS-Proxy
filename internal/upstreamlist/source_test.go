package upstreamlist_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const sourceTestTimeout = 4 * time.Second

func TestNewBootstrapsMissingPathWithoutDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	source, err := upstreamlist.New(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "One upstream host or origin per line") {
		t.Fatalf("bootstrapped content = %q", data)
	}
	if _, err := source.Current(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("bad/path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := upstreamlist.New(path); err != nil {
		t.Fatalf("New decoded an existing file: %v", err)
	}
}

func TestCurrentLoadsOnceAndReturnsCachedValue(t *testing.T) {
	path := writeSourceFile(t, "first.example.test\n")
	source := newSource(t, path)
	first, err := source.Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := source.Current()
	if err != nil {
		t.Fatal(err)
	}
	if got := second.HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("cached hostname = %q", got)
	}
	if got := first.HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("first hostname = %q", got)
	}
}

func TestCurrentDeduplicatesNormalizedSelectors(t *testing.T) {
	path := writeSourceFile(t, "EXAMPLE.TEST\nexample.test\nhttps://EXAMPLE.TEST:0443\nhttps://example.test:443\n")
	source := initializedSource(t, path)
	list, err := source.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.HostSelectors) != 1 || list.HostSelectors[0].Hostname != "example.test" {
		t.Fatalf("host selectors = %#v", list.HostSelectors)
	}
	if len(list.OriginSelectors) != 1 || list.OriginSelectors[0].Port != "443" {
		t.Fatalf("origin selectors = %#v", list.OriginSelectors)
	}
}

func TestCurrentFailsWithoutValidInitialSource(t *testing.T) {
	path := writeSourceFile(t, string([]byte{0xff}))
	source := newSource(t, path)
	if _, err := source.Current(); err == nil {
		t.Fatal("Current accepted invalid UTF-8")
	}
	if _, err := source.Updates(context.Background()); err == nil {
		t.Fatal("Updates accepted an uninitialized source")
	}
}

func TestUpdatesDoesNotRepublishInitialCurrent(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := newSource(t, path)
	if _, err := source.Current(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := source.Updates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case state := <-updates:
		t.Fatalf("initial state was republished: %#v", state)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestUpdatesClosesOnCancellation(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := newSource(t, path)
	if _, err := source.Current(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	updates, err := source.Updates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("cancellation published a state")
		}
	case <-time.After(sourceTestTimeout):
		t.Fatal("updates did not close after cancellation")
	}
}

func TestRepresentationOnlyChangeIsSuppressed(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := initializedSource(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	writeFile(t, path, "# comment\nAPI.EXAMPLE.TEST\napi.example.test\n")
	assertNoState(t, updates, 500*time.Millisecond)
}

func TestSemanticEntryChangePublishesAndUpdatesCacheBeforeState(t *testing.T) {
	path := writeSourceFile(t, "first.example.test\n")
	source := initializedSource(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	writeFile(t, path, "second.example.test\n")
	state := waitState(t, updates)
	if state.Diagnostic != nil || state.List.HostSelectors[0].Hostname != "second.example.test" {
		t.Fatalf("state = %#v", state)
	}
	cached, err := source.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cached.HostSelectors[0].Hostname != "second.example.test" {
		t.Fatal("cache was updated after publication")
	}
}

func TestWarningsOnlyChangePublishesWithoutChangingEntries(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := initializedSource(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	writeFile(t, path, "api.example.test\nbad/path\n")
	state := waitState(t, updates)
	if state.Diagnostic != nil || len(state.List.Warnings) != 1 || len(state.List.HostSelectors)+len(state.List.OriginSelectors) != 1 {
		t.Fatalf("state = %#v", state)
	}
}

func TestInitialReconciliationClosesCurrentWatchRace(t *testing.T) {
	path := writeSourceFile(t, "first.example.test\n")
	source := initializedSource(t, path)
	writeFile(t, path, "second.example.test\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	state := waitState(t, updates)
	if got := state.List.HostSelectors[0].Hostname; got != "second.example.test" {
		t.Fatalf("initial reconciliation hostname = %q", got)
	}
}

func TestRuntimeUnavailableSourceKeepsLastKnownGoodAndRecovers(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := initializedSource(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	degraded := waitState(t, updates)
	if degraded.Diagnostic == nil || degraded.Diagnostic.Kind != upstreamlist.DiagnosticSourceUnavailable {
		t.Fatalf("degraded state = %#v", degraded)
	}
	if degraded.List.HostSelectors[0].Hostname != "api.example.test" {
		t.Fatal("degraded state lost last-known-good list")
	}

	writeFile(t, path, "api.example.test\n")
	healthy := waitState(t, updates)
	if healthy.Diagnostic != nil || len(healthy.List.HostSelectors)+len(healthy.List.OriginSelectors) != 1 {
		t.Fatalf("recovered state = %#v", healthy)
	}
}

func TestEquivalentRepeatedDiagnosticIsSuppressed(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := initializedSource(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := updatesFor(t, source, ctx)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_ = waitState(t, updates)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The valid empty file is a recovery, so the next state must be healthy.
	healthy := waitState(t, updates)
	if healthy.Diagnostic != nil {
		t.Fatalf("healthy state = %#v", healthy)
	}
}

func TestCurrentRejectsSymlinkedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	target := filepath.Join(t.TempDir(), "target.txt")
	writeFile(t, target, "api.example.test\n")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	source, err := upstreamlist.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Current(); err == nil || !strings.Contains(err.Error(), "ordinary file") {
		t.Fatalf("Current error = %v", err)
	}
}

func newSource(t *testing.T, path string) *upstreamlist.Source {
	t.Helper()
	source, err := upstreamlist.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func initializedSource(t *testing.T, path string) *upstreamlist.Source {
	t.Helper()
	source := newSource(t, path)
	if _, err := source.Current(); err != nil {
		t.Fatal(err)
	}
	return source
}

func updatesFor(t *testing.T, source *upstreamlist.Source, ctx context.Context) <-chan upstreamlist.State {
	t.Helper()
	updates, err := source.Updates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return updates
}

func waitState(t *testing.T, updates <-chan upstreamlist.State) upstreamlist.State {
	t.Helper()
	select {
	case state, ok := <-updates:
		if !ok {
			t.Fatal("updates closed unexpectedly")
		}
		return state
	case <-time.After(sourceTestTimeout):
		t.Fatal("timed out waiting for source state")
		return upstreamlist.State{}
	}
}

func assertNoState(t *testing.T, updates <-chan upstreamlist.State, duration time.Duration) {
	t.Helper()
	select {
	case state := <-updates:
		t.Fatalf("unexpected state: %#v", state)
	case <-time.After(duration):
	}
}

func writeSourceFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	writeFile(t, path, contents)
	return path
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
