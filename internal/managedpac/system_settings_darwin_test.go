//go:build darwin

package managedpac

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls          []string
	err            error
	out            []byte
	autoProxyOut   []byte
	runFunc        func(name string, args ...string) ([]byte, error)
	runContextFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (f *fakeRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.runContextFunc != nil {
		return f.runContextFunc(ctx, name, args...)
	}
	if f.runFunc != nil {
		return f.runFunc(name, args...)
	}
	switch args[0] {
	case "-listallnetworkservices":
		return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nThunderbolt Bridge\n"), nil
	case "-getautoproxyurl":
		if f.autoProxyOut != nil {
			return f.autoProxyOut, nil
		}
		return []byte("URL: http://old.example/proxy.pac\nEnabled: Yes\n"), nil
	default:
		return f.out, f.err
	}
}

func TestDarwinSystemSettingsReportsAppliedServicesBeforeFailure(t *testing.T) {
	runner := &fakeRunner{}
	runner.runFunc = func(_ string, args ...string) ([]byte, error) {
		switch args[1] {
		case "Ethernet":
			return nil, nil
		case "Missing VPN":
			return []byte("Missing VPN is not a recognized network service."), errors.New("exit status 1")
		default:
			return []byte("permission denied"), errors.New("exit status 1")
		}
	}
	adapter := &darwinSystemSettings{runner: runner}

	result, err := adapter.Apply(context.Background(), "http://127.0.0.1:8079/seamless-cors.pac", []string{"Ethernet", "Missing VPN", "Wi-Fi"})

	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("apply error = %v", err)
	}
	if got := strings.Join(result.AppliedServices, ","); got != "Ethernet" {
		t.Fatalf("applied services = %q, want Ethernet", got)
	}
}

func TestDarwinSystemSettingsIncludesDisabledVisibleServices(t *testing.T) {
	runner := &fakeRunner{runFunc: func(_ string, args ...string) ([]byte, error) {
		if args[0] == "-listallnetworkservices" {
			return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Disabled VPN\n"), nil
		}
		return nil, nil
	}}
	adapter := &darwinSystemSettings{runner: runner}

	services, err := adapter.listServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(services, ","); got != "Wi-Fi,Disabled VPN" {
		t.Fatalf("visible services = %q", got)
	}
}

func TestDarwinSystemSettingsInstallsPACDirectlyWithSetAutoProxyURL(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &darwinSystemSettings{runner: runner}

	if _, err := adapter.Apply(context.Background(), "http://127.0.0.1:8079/seamless-cors.pac", []string{"Wi-Fi"}); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	want := "networksetup -setautoproxyurl Wi-Fi http://127.0.0.1:8079/seamless-cors.pac"
	if !strings.Contains(joined, want) {
		t.Fatalf("missing call %q in:\n%s", want, joined)
	}
	for _, unwanted := range []string{"-listallnetworkservices", "-getautoproxyurl", "-setautoproxystate"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("PAC installation should not call %q:\n%s", unwanted, joined)
		}
	}
}

func TestDarwinSystemSettingsPreservesForeignPACState(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &darwinSystemSettings{runner: runner}

	if err := adapter.DisableOwned(context.Background(), []string{"Wi-Fi"}); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	if strings.Contains(joined, "-setautoproxystate Wi-Fi off") {
		t.Fatalf("foreign PAC should not be cleared:\n%s", joined)
	}
}

func TestDarwinSystemSettingsDisablesMarkerOwnedStateAcrossServices(t *testing.T) {
	runner := &fakeRunner{
		autoProxyOut: []byte("URL: http://127.0.0.1:52144/nested/seamless-cors.pac\nEnabled: Yes\n"),
	}
	adapter := &darwinSystemSettings{runner: runner}

	if err := adapter.DisableOwned(context.Background(), []string{"Wi-Fi", "Thunderbolt Bridge"}); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"networksetup -setautoproxystate Wi-Fi off",
		"networksetup -setautoproxystate Thunderbolt Bridge off",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "-setautoproxyurl") {
		t.Fatalf("cleanup must not attempt to clear the retained PAC URL:\n%s", joined)
	}
}
