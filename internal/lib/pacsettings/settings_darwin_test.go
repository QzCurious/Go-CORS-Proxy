//go:build darwin

package pacsettings

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDarwinRunner struct {
	calls   []string
	runFunc func(context.Context, string, ...string) ([]byte, error)
}

func (f *fakeDarwinRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.runFunc != nil {
		return f.runFunc(ctx, name, args...)
	}
	switch args[0] {
	case "-listallnetworkservices":
		return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Disabled VPN\n"), nil
	case "-getautoproxyurl":
		return []byte("URL: http://old.example/proxy.pac\nEnabled: Yes\n"), nil
	default:
		return nil, nil
	}
}

func TestDarwinListReturnsAllVisibleServiceNames(t *testing.T) {
	runner := &fakeDarwinRunner{}
	settings := testDarwinSettings(runner)

	got, err := settings.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "Wi-Fi,Disabled VPN" {
		t.Fatalf("services = %#v", got)
	}
}

func TestDarwinLookupReturnsFreshPACSetting(t *testing.T) {
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-getautoproxyurl" {
			return []byte("URL: http://corp.example/proxy.pac\nEnabled: Yes\n"), nil
		}
		return nil, nil
	}}
	settings := testDarwinSettings(runner)

	setting, err := settings.Lookup(context.Background(), "Wi-Fi")
	if err != nil {
		t.Fatal(err)
	}
	if setting.URL != "http://corp.example/proxy.pac" || !setting.Enabled {
		t.Fatalf("setting = %#v", setting)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "-getautoproxyurl Wi-Fi") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDarwinOperationsWrapRunnerFailures(t *testing.T) {
	runnerErr := errors.New("exit status 4")
	runner := &fakeDarwinRunner{runFunc: func(context.Context, string, ...string) ([]byte, error) {
		return nil, runnerErr
	}}
	settings := testDarwinSettings(runner)

	if _, err := settings.Lookup(context.Background(), "Vanished VPN"); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "get PAC setting") {
		t.Fatalf("lookup error = %v", err)
	}
	if err := settings.SetURL(context.Background(), "Vanished VPN", "http://127.0.0.1/p.pac"); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "set PAC URL") {
		t.Fatalf("set URL error = %v", err)
	}
	if err := settings.Disable(context.Background(), "Vanished VPN"); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "disable PAC") {
		t.Fatalf("disable error = %v", err)
	}
}

func TestDarwinSetURLMutatesNamedService(t *testing.T) {
	runner := &fakeDarwinRunner{}
	settings := testDarwinSettings(runner)

	if err := settings.SetURL(context.Background(), "Wi-Fi", "http://127.0.0.1/seamless-cors.pac"); err != nil {
		t.Fatal(err)
	}
	want := "networksetup -setautoproxyurl Wi-Fi http://127.0.0.1/seamless-cors.pac"
	if joined := strings.Join(runner.calls, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("missing call %q in:\n%s", want, joined)
	}
}

func TestDarwinDisableMutatesNamedService(t *testing.T) {
	runner := &fakeDarwinRunner{}
	settings := testDarwinSettings(runner)

	if err := settings.Disable(context.Background(), "Wi-Fi"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "networksetup -setautoproxystate Wi-Fi off") {
		t.Fatalf("PAC was not disabled:\n%s", joined)
	}
	if strings.Contains(joined, "-setautoproxyurl") {
		t.Fatalf("disable changed the retained URL:\n%s", joined)
	}
}

func testDarwinSettings(runner commandRunner) *Settings {
	return &Settings{platform: &darwinSettings{runner: runner}}
}
