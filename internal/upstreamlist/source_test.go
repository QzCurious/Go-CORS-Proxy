package upstreamlist_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const timeout = 4 * time.Second

func TestMissingSourceStartsDegradedWithoutCreating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	source := upstreamlist.Open(path)
	defer source.Close()
	degradation := requireDegradation(t, waitTransition(t, source.Transitions()))
	if !errors.Is(degradation, degradation.Err) {
		t.Fatalf("degradation does not unwrap its cause: %v", degradation)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("source was created: %v", err)
	}
}

func TestBootstrapCreatesImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	if err := upstreamlist.Bootstrap(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapReturnsCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "upstreams.txt")
	if err := upstreamlist.Bootstrap(path); err == nil {
		t.Fatal("Bootstrap succeeded through a non-directory parent")
	}
}

func TestInvalidInitialSourceIsDegraded(t *testing.T) {
	path := writeFile(t, string([]byte{0xff}))
	source := upstreamlist.Open(path)
	defer source.Close()
	requireDegradation(t, waitTransition(t, source.Transitions()))
}

func TestSemanticChangesAndRecoveryPublishListAccepted(t *testing.T) {
	path := writeFile(t, "first.example.test\n")
	source := upstreamlist.Open(path)
	defer source.Close()
	if got := requireList(t, waitTransition(t, source.Transitions())).HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("host = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	requireDegradation(t, waitTransition(t, source.Transitions()))
	writeAt(t, path, "first.example.test\n")
	if got := requireList(t, waitTransition(t, source.Transitions())).HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("host = %q", got)
	}
}

func TestContinuouslyHealthySemanticEqualityIsSuppressed(t *testing.T) {
	path := writeFile(t, "api.example.test\n")
	source := upstreamlist.Open(path)
	defer source.Close()
	requireList(t, waitTransition(t, source.Transitions()))
	writeAt(t, path, "# representation only\nAPI.EXAMPLE.TEST\n")
	select {
	case got := <-source.Transitions():
		t.Fatalf("unexpected transition %#v", got)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestAssessmentDisclosesCreationConsequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "upstreams.txt")
	a := upstreamlist.AssessBootstrap(path)
	if !a.Required || a.Path != path || a.DefaultContents == "" || len(a.MissingParentDirectories) != 2 || a.Fingerprint == "" {
		t.Fatalf("assessment = %#v", a)
	}
}

func waitTransition(t *testing.T, transitions <-chan upstreamlist.Transition) upstreamlist.Transition {
	t.Helper()
	select {
	case value, ok := <-transitions:
		if !ok {
			t.Fatal("transitions closed")
		}
		return value
	case <-time.After(timeout):
		t.Fatal("timeout")
		return nil
	}
}
func requireList(t *testing.T, transition upstreamlist.Transition) upstreamlist.UpstreamList {
	t.Helper()
	value, ok := transition.(upstreamlist.ListAccepted)
	if !ok {
		t.Fatalf("transition = %#v", transition)
	}
	return value.List
}
func requireDegradation(t *testing.T, transition upstreamlist.Transition) upstreamlist.SourceDegraded {
	t.Helper()
	value, ok := transition.(upstreamlist.SourceDegraded)
	if !ok {
		t.Fatalf("transition = %#v", transition)
	}
	if value.Err == nil {
		t.Fatal("degradation has no error")
	}
	return value
}
func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	writeAt(t, path, contents)
	return path
}
func writeAt(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
