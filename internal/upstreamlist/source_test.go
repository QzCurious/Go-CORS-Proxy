package upstreamlist_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

const timeout = 4 * time.Second

func TestMissingSourceStartsDegradedWithoutCreating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	source := upstreamlist.Open(path, upstreamlist.CreationDeclined)
	defer source.Close()
	diagnostic := requireDiagnostic(t, waitTransition(t, source.Transitions()))
	if diagnostic.Kind != upstreamlist.DiagnosticSourceUnavailable {
		t.Fatalf("kind = %v", diagnostic.Kind)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("source was created: %v", err)
	}
}

func TestAcceptedCreationCreatesImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	source := upstreamlist.Open(path, upstreamlist.CreationAccepted)
	defer source.Close()
	requireList(t, waitTransition(t, source.Transitions()))
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidInitialSourceIsDegraded(t *testing.T) {
	path := writeFile(t, string([]byte{0xff}))
	source := upstreamlist.Open(path, upstreamlist.CreationUndecided)
	defer source.Close()
	if got := requireDiagnostic(t, waitTransition(t, source.Transitions())).Kind; got != upstreamlist.DiagnosticInvalidSource {
		t.Fatalf("kind = %v", got)
	}
}

func TestSemanticChangesAndRecoveryPublishListAccepted(t *testing.T) {
	path := writeFile(t, "first.example.test\n")
	source := upstreamlist.Open(path, upstreamlist.CreationUndecided)
	defer source.Close()
	if got := requireList(t, waitTransition(t, source.Transitions())).HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("host = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	requireDiagnostic(t, waitTransition(t, source.Transitions()))
	writeAt(t, path, "first.example.test\n")
	if got := requireList(t, waitTransition(t, source.Transitions())).HostSelectors[0].Hostname; got != "first.example.test" {
		t.Fatalf("host = %q", got)
	}
}

func TestContinuouslyHealthySemanticEqualityIsSuppressed(t *testing.T) {
	path := writeFile(t, "api.example.test\n")
	source := upstreamlist.Open(path, upstreamlist.CreationUndecided)
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
	a := upstreamlist.AssessCreation(path)
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
func requireDiagnostic(t *testing.T, transition upstreamlist.Transition) upstreamlist.Diagnostic {
	t.Helper()
	value, ok := transition.(upstreamlist.DiagnosticReported)
	if !ok {
		t.Fatalf("transition = %#v", transition)
	}
	return value.Diagnostic
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
