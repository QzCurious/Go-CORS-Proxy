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

func TestDarwinListNormalizesVisibleServicesAndSkipsDisappearance(t *testing.T) {
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "-listallnetworkservices":
			return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Disabled VPN\nVanished VPN\n"), nil
		case "-getautoproxyurl":
			if args[1] == "Wi-Fi" {
				return []byte("URL: (null)\nEnabled: No\n"), nil
			}
			if args[1] == "Vanished VPN" {
				return []byte("Vanished VPN is not a recognized network service."), errors.New("exit status 1")
			}
			return []byte("URL: http://corp.example/proxy.pac\nEnabled: Yes\n"), nil
		default:
			return nil, nil
		}
	}}
	settings := &Settings{runner: runner}

	got, err := settings.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Setting{ServiceName: "Wi-Fi"}) {
		t.Fatalf("settings = %#v", got)
	}
	if got[1].ServiceName != "Disabled VPN" || got[1].URL != "http://corp.example/proxy.pac" || !got[1].Enabled {
		t.Fatalf("settings = %#v", got)
	}
}

func TestDarwinSetURLRequiresUnchangedObservation(t *testing.T) {
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-getautoproxyurl" {
			return []byte("URL: http://corp.example/proxy.pac\nEnabled: Yes\n"), nil
		}
		return nil, nil
	}}
	settings := &Settings{runner: runner}

	result, err := settings.SetURL(context.Background(), Setting{ServiceName: "Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac")
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || result.Current == nil || result.Current.URL != "http://corp.example/proxy.pac" {
		t.Fatalf("result = %#v", result)
	}
	if joined := strings.Join(runner.calls, "\n"); strings.Contains(joined, "-setautoproxyurl") {
		t.Fatalf("changed setting was overwritten:\n%s", joined)
	}
}

func TestDarwinSetURLAppliesAfterMatchingObservation(t *testing.T) {
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-getautoproxyurl" {
			return []byte("URL: (null)\nEnabled: No\n"), nil
		}
		return nil, nil
	}}
	settings := &Settings{runner: runner}

	result, err := settings.SetURL(context.Background(), Setting{ServiceName: "Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("result = %#v", result)
	}
	want := "networksetup -setautoproxyurl Wi-Fi http://127.0.0.1/seamless-cors.pac"
	if joined := strings.Join(runner.calls, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("missing call %q in:\n%s", want, joined)
	}
}

func TestDarwinDisableRequiresUnchangedObservation(t *testing.T) {
	runner := &fakeDarwinRunner{}
	settings := &Settings{runner: runner}
	observed := Setting{ServiceName: "Wi-Fi", URL: "http://old.example/proxy.pac", Enabled: true}

	result, err := settings.Disable(context.Background(), observed)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("result = %#v", result)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "networksetup -setautoproxystate Wi-Fi off") {
		t.Fatalf("PAC was not disabled:\n%s", joined)
	}
	if strings.Contains(joined, "-setautoproxyurl") {
		t.Fatalf("disable changed the retained URL:\n%s", joined)
	}
}
