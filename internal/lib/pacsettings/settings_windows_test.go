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
	runner := &fakeWindowsRunner{out: []byte("{\"ServiceName\":\"Windows Current User\",\"URL\":\"http://127.0.0.1/seamless-cors.pac\",\"Enabled\":true}")}
	settings := &Settings{runner: runner}

	got, err := settings.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "http://127.0.0.1/seamless-cors.pac" || !got[0].Enabled {
		t.Fatalf("settings = %#v", got)
	}
}

func TestWindowsSetURLRequiresUnchangedObservation(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte("{\"ServiceName\":\"Windows Current User\",\"URL\":\"http://corp.example/proxy.pac\",\"Enabled\":true}")}
	settings := &Settings{runner: runner, notify: func() error { return nil }}

	result, err := settings.SetURL(context.Background(), Setting{ServiceName: windowsPACServiceName}, "http://127.0.0.1/seamless-cors.pac")
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || result.Current == nil || result.Current.URL != "http://corp.example/proxy.pac" {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want inspection only", runner.calls)
	}
}

func TestWindowsSetURLAppliesAndNotifiesAfterMatchingObservation(t *testing.T) {
	current := Setting{ServiceName: windowsPACServiceName}
	runner := &fakeWindowsRunner{out: []byte("{\"ServiceName\":\"Windows Current User\",\"URL\":\"\",\"Enabled\":false}")}
	notified := false
	settings := &Settings{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	result, err := settings.SetURL(context.Background(), current, "http://127.0.0.1/seamless-cors.pac")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !notified {
		t.Fatalf("result = %#v, notified = %t", result, notified)
	}
	if len(runner.calls) != 2 || !strings.Contains(runner.calls[1], "New-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestWindowsDisableRequiresUnchangedObservation(t *testing.T) {
	current := Setting{ServiceName: windowsPACServiceName, URL: "http://127.0.0.1/seamless-cors.pac", Enabled: true}
	runner := &fakeWindowsRunner{out: []byte("{\"ServiceName\":\"Windows Current User\",\"URL\":\"http://127.0.0.1/seamless-cors.pac\",\"Enabled\":true}")}
	notified := false
	settings := &Settings{runner: runner, notify: func() error {
		notified = true
		return nil
	}}

	result, err := settings.Disable(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !notified {
		t.Fatalf("result = %#v, notified = %t", result, notified)
	}
	if len(runner.calls) != 2 || !strings.Contains(runner.calls[1], "Remove-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}
