package cleanup

import (
	"errors"
	"strings"
	"testing"

	"seamless-cors/internal/platform"
)

type fakeAdapter struct {
	pacStates []platform.PACServiceState
	ca        bool
	clearErr  error
	cleared   int
}

func (f *fakeAdapter) CurrentPACState() ([]platform.PACServiceState, error) {
	return f.pacStates, nil
}

func (f *fakeAdapter) ClearOwnedPAC() error {
	f.cleared++
	return f.clearErr
}

func TestInspectReportsOwnershipMarkersWithoutMutating(t *testing.T) {
	adapter := &fakeAdapter{
		pacStates: []platform.PACServiceState{{
			Name:    "Wi-Fi",
			URL:     "http://127.0.0.1:8079/seamless-cors.pac",
			Enabled: true,
		}},
		ca: true,
	}

	inspection := Inspect(adapter)

	if !inspection.Needed() {
		t.Fatal("owned marker should require cleanup")
	}
	if !inspection.OwnedPAC {
		t.Fatalf("inspection = %#v", inspection)
	}
}

func TestCleanRemovesOwnedPAC(t *testing.T) {
	adapter := &fakeAdapter{}

	if err := Clean(adapter); err != nil {
		t.Fatal(err)
	}

	if adapter.cleared != 1 {
		t.Fatalf("cleanup calls: PAC=%d", adapter.cleared)
	}
}

func TestCleanGroupsFailuresWithRetryGuidance(t *testing.T) {
	adapter := &fakeAdapter{
		clearErr: errors.New("pac denied"),
	}

	err := Clean(adapter)
	if err == nil {
		t.Fatal("expected cleanup error")
	}
	got := err.Error()
	for _, want := range []string{
		"managed PAC cleanup failed: pac denied",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cleanup error missing %q:\n%s", want, got)
		}
	}
}
