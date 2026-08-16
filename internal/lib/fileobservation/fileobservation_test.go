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
	first := waitOutcome(t, observation.Outcomes())
	observed, ok := first.(fileobservation.ReadError)
	if !ok {
		t.Fatalf("first = %#v", first)
	}
	if !errors.Is(observed, os.ErrNotExist) {
		t.Fatalf("first cause = %v", observed)
	}
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := waitOutcome(t, observation.Outcomes())
	contents, ok := second.(fileobservation.Contents)
	if !ok || string(contents) != "value" {
		t.Fatalf("second = %#v", second)
	}
}

func TestUnchangedHealthyContentsArePublished(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := fileobservation.Open(path, fileobservation.Options{Debounce: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	waitOutcome(t, observation.Outcomes())
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := waitOutcome(t, observation.Outcomes())
	contents, ok := outcome.(fileobservation.Contents)
	if !ok || string(contents) != "value" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func waitOutcome(t *testing.T, outcomes <-chan fileobservation.Outcome) fileobservation.Outcome {
	t.Helper()
	select {
	case outcome, ok := <-outcomes:
		if !ok {
			t.Fatal("outcomes closed")
		}
		return outcome
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
		return nil
	}
}
