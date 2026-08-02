//go:build windows

package managedpac

import (
	"context"
	"strings"
	"testing"
)

type fakeWindowsRunner struct {
	calls []string
	out   []byte
	err   error
}

func (f *fakeWindowsRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.out, f.err
}

func TestWindowsSystemSettingsAppliesPACForCurrentUser(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	settings := &windowsSystemSettings{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	result, err := settings.Apply(context.Background(), "http://127.0.0.1/seamless-cors.pac", []string{windowsPACServiceName})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AppliedServices) != 1 || result.AppliedServices[0] != windowsPACServiceName {
		t.Fatalf("apply result = %#v", result)
	}
	if !notified {
		t.Fatal("Windows settings change was not announced")
	}
}

func TestWindowsSystemSettingsSnapshotsCurrentPAC(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"ServiceName":"Windows Current User","PACURL":"http://127.0.0.1/seamless-cors.pac","Enabled":true}`)}
	settings := &windowsSystemSettings{runner: runner}

	snapshots, err := settings.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].PACURL != "http://127.0.0.1/seamless-cors.pac" || !snapshots[0].Enabled {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func TestWindowsSystemSettingsPreservesChangedPAC(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte(`{"ServiceName":"Windows Current User","PACURL":"http://corp.example/proxy.pac","Enabled":true}`)}
	settings := &windowsSystemSettings{runner: runner, notify: func() error { return nil }}

	err := settings.ClearOwned(context.Background(), []string{windowsPACServiceName})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want inspection only", runner.calls)
	}
}
