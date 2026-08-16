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
	observation := fileobservation.Open(path)
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
	observation := fileobservation.Open(path)
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

func TestMissingParentStopsObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "upstreams.txt")
	observation := fileobservation.Open(path)
	defer observation.Close()

	outcome := waitOutcome(t, observation.Outcomes())
	if _, ok := outcome.(fileobservation.ObservationStoppedError); !ok {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestParentRemovalStopsObservation(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "upstreams.txt")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation := fileobservation.Open(path)
	defer observation.Close()
	waitOutcome(t, observation.Outcomes())

	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	for {
		outcome := waitOutcome(t, observation.Outcomes())
		if _, ok := outcome.(fileobservation.ObservationStoppedError); ok {
			return
		}
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
