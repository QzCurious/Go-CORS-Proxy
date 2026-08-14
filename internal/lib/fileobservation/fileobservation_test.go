package fileobservation_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileobservation"
)

func TestMissingFileIsAResultAndLaterCreationRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	observation, err := fileobservation.Open(path, fileobservation.Options{Debounce: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	first := waitResult(t, observation.Results())
	var observed *fileobservation.ReadError
	if !errors.As(first.Err, &observed) {
		t.Fatalf("first = %#v", first)
	}
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := waitResult(t, observation.Results())
	if !second.Snapshot || string(second.Contents) != "value" || second.Err != nil {
		t.Fatalf("second = %#v", second)
	}
}

func TestUnchangedHealthyContentsAreSuppressed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := fileobservation.Open(path, fileobservation.Options{Debounce: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	waitResult(t, observation.Results())
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-observation.Results():
		t.Fatalf("unexpected result %#v", result)
	case <-time.After(150 * time.Millisecond):
	}
}

func waitResult(t *testing.T, results <-chan fileobservation.Result) fileobservation.Result {
	t.Helper()
	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("results closed")
		}
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
		return fileobservation.Result{}
	}
}
