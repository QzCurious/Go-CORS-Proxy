//go:build windows

package pacsettings

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

func TestWindowsListReturnsCurrentUserSetting(t *testing.T) {
	runner := &fakeWindowsRunner{}
	settings := testWindowsSettings(runner, nil)

	got, err := settings.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != windowsPACServiceName {
		t.Fatalf("services = %#v", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("list invoked PAC lookup: %#v", runner.calls)
	}
}

func TestWindowsLookupReturnsCurrentUserSetting(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte("{\"URL\":\"http://corp.example/proxy.pac\",\"Enabled\":true}")}
	settings := testWindowsSettings(runner, func() error { return nil })

	setting, err := settings.Lookup(context.Background(), windowsPACServiceName)
	if err != nil {
		t.Fatal(err)
	}
	if setting.URL != "http://corp.example/proxy.pac" || !setting.Enabled {
		t.Fatalf("setting = %#v", setting)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want one lookup", runner.calls)
	}
}

func TestWindowsSetURLMutatesAndNotifies(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	settings := testWindowsSettings(runner, func() error {
		notified = true
		return nil
	})

	if err := settings.SetURL(context.Background(), windowsPACServiceName, "http://127.0.0.1/seamless-cors.pac"); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("PAC mutation was not notified")
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "New-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestWindowsDisableMutatesAndNotifies(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	settings := testWindowsSettings(runner, func() error {
		notified = true
		return nil
	})

	if err := settings.Disable(context.Background(), windowsPACServiceName); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("PAC mutation was not notified")
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "Remove-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func testWindowsSettings(runner commandRunner, notify func() error) *Settings {
	return &Settings{platform: &windowsSettings{runner: runner, notify: notify}}
}
