//go:build darwin

package networkservice

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
	services := testDarwinServices(runner)

	got, err := services.list(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for index, service := range got {
		names[index] = service.Name()
	}
	if strings.Join(names, ",") != "Wi-Fi,Disabled VPN" {
		t.Fatalf("services = %#v", got)
	}
}

func TestDarwinServiceObservesFreshPACSetting(t *testing.T) {
	runner := &fakeDarwinRunner{runFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "-getautoproxyurl" {
			return []byte("URL: http://corp.example/proxy.pac\nEnabled: Yes\n"), nil
		}
		return nil, nil
	}}
	services := testDarwinServices(runner)
	service := testDarwinService(services, "Wi-Fi")

	setting, err := service.PAC(context.Background())
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
	services := testDarwinServices(runner)
	service := testDarwinService(services, "Vanished VPN")

	if _, err := service.PAC(context.Background()); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "get PAC setting") {
		t.Fatalf("observation error = %v", err)
	}
	if err := service.SetPAC(context.Background(), "http://127.0.0.1/p.pac"); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "set PAC URL") {
		t.Fatalf("set URL error = %v", err)
	}
	if err := service.DisablePAC(context.Background()); !errors.Is(err, runnerErr) || !strings.Contains(err.Error(), "disable PAC") {
		t.Fatalf("disable error = %v", err)
	}
}

func TestDarwinSetPACMutatesNamedService(t *testing.T) {
	runner := &fakeDarwinRunner{}
	services := testDarwinServices(runner)
	service := testDarwinService(services, "Wi-Fi")

	if err := service.SetPAC(context.Background(), "http://127.0.0.1/seamless-cors.pac"); err != nil {
		t.Fatal(err)
	}
	want := "networksetup -setautoproxyurl Wi-Fi http://127.0.0.1/seamless-cors.pac"
	if joined := strings.Join(runner.calls, "\n"); !strings.Contains(joined, want) {
		t.Fatalf("missing call %q in:\n%s", want, joined)
	}
}

func TestDarwinDisablePACMutatesNamedService(t *testing.T) {
	runner := &fakeDarwinRunner{}
	services := testDarwinServices(runner)
	service := testDarwinService(services, "Wi-Fi")

	if err := service.DisablePAC(context.Background()); err != nil {
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

func testDarwinServices(runner commandRunner) *services {
	return &services{runner: runner}
}

func testDarwinService(owner *services, name string) *service {
	return &service{owner: owner, name: name}
}
