package upstreamlist_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const sourceTestTimeout = 4 * time.Second

func TestOpenBootstrapsAndProjectsMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	source := openSource(t, path)
	defer source.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "One upstream host or origin per line") {
		t.Fatalf("bootstrapped content = %q", data)
	}
	if state := source.Current(); state.Diagnostic != nil || len(state.List.HostSelectors)+len(state.List.OriginSelectors) != 0 {
		t.Fatalf("current = %#v", state)
	}
}

func TestOpenRejectsInvalidInitialSource(t *testing.T) {
	path := writeSourceFile(t, string([]byte{0xff}))
	if _, err := upstreamlist.Open(path); err == nil {
		t.Fatal("Open accepted invalid UTF-8")
	}
}

func TestCurrentContainsDeduplicatedInitialState(t *testing.T) {
	path := writeSourceFile(t, "EXAMPLE.TEST\nexample.test\nhttps://EXAMPLE.TEST:0443\nhttps://example.test:443\n")
	source := openSource(t, path)
	defer source.Close()

	list := source.Current().List
	if len(list.HostSelectors) != 1 || list.HostSelectors[0].Hostname != "example.test" {
		t.Fatalf("host selectors = %#v", list.HostSelectors)
	}
	if len(list.OriginSelectors) != 1 || list.OriginSelectors[0].Port != "443" {
		t.Fatalf("origin selectors = %#v", list.OriginSelectors)
	}
}

func TestUpdatesDoesNotRepublishInitialState(t *testing.T) {
	source := openSource(t, writeSourceFile(t, "api.example.test\n"))
	defer source.Close()
	assertNoState(t, source.Updates(), 250*time.Millisecond)
}

func TestRepresentationOnlyChangeIsSuppressed(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := openSource(t, path)
	defer source.Close()

	writeFile(t, path, "# comment\nAPI.EXAMPLE.TEST\napi.example.test\n")
	assertNoState(t, source.Updates(), 500*time.Millisecond)
}

func TestSemanticChangeAdvancesCurrentBeforePublication(t *testing.T) {
	path := writeSourceFile(t, "first.example.test\n")
	source := openSource(t, path)
	defer source.Close()

	writeFile(t, path, "second.example.test\n")
	state := waitState(t, source.Updates())
	if state.Diagnostic != nil || state.List.HostSelectors[0].Hostname != "second.example.test" {
		t.Fatalf("state = %#v", state)
	}
	if current := source.Current(); current.List.HostSelectors[0].Hostname != "second.example.test" {
		t.Fatalf("current = %#v", current)
	}
}

func TestWarningsOnlyChangePublishes(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := openSource(t, path)
	defer source.Close()

	writeFile(t, path, "api.example.test\nbad/path\n")
	state := waitState(t, source.Updates())
	if state.Diagnostic != nil || len(state.List.Warnings) != 1 || len(state.List.HostSelectors)+len(state.List.OriginSelectors) != 1 {
		t.Fatalf("state = %#v", state)
	}
}

func TestRuntimeFailureRetainsLastKnownGoodAndRecovers(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := openSource(t, path)
	defer source.Close()

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	degraded := waitState(t, source.Updates())
	if degraded.Diagnostic == nil || degraded.Diagnostic.Kind != upstreamlist.DiagnosticSourceUnavailable {
		t.Fatalf("degraded = %#v", degraded)
	}
	if degraded.List.HostSelectors[0].Hostname != "api.example.test" {
		t.Fatal("degraded state lost the last-known-good list")
	}

	writeFile(t, path, "api.example.test\n")
	healthy := waitState(t, source.Updates())
	if healthy.Diagnostic != nil || healthy.List.HostSelectors[0].Hostname != "api.example.test" {
		t.Fatalf("healthy = %#v", healthy)
	}
}

func TestRepeatedFailuresAreNotSuppressed(t *testing.T) {
	path := writeSourceFile(t, "api.example.test\n")
	source := openSource(t, path)
	defer source.Close()

	invalid := string([]byte{0xff})
	writeFile(t, path, invalid)
	first := waitState(t, source.Updates())
	if first.Diagnostic == nil || first.Diagnostic.Kind != upstreamlist.DiagnosticInvalidSource {
		t.Fatalf("first = %#v", first)
	}
	writeFile(t, path, invalid)
	second := waitState(t, source.Updates())
	if second.Diagnostic == nil || second.Diagnostic.Kind != upstreamlist.DiagnosticInvalidSource {
		t.Fatalf("second = %#v", second)
	}
}

func TestOpenRejectsSymlinkedSource(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeFile(t, target, "api.example.test\n")
	path := filepath.Join(dir, "upstreams.txt")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := upstreamlist.Open(path); err == nil || !strings.Contains(err.Error(), "ordinary file") {
		t.Fatalf("Open error = %v", err)
	}
}

func TestCloseClosesUpdates(t *testing.T) {
	source := openSource(t, writeSourceFile(t, "api.example.test\n"))
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-source.Updates():
		if ok {
			t.Fatal("updates remained open")
		}
	case <-time.After(sourceTestTimeout):
		t.Fatal("updates did not close")
	}
}

func openSource(t *testing.T, path string) *upstreamlist.Source {
	t.Helper()
	source, err := upstreamlist.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return source
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
