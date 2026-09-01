//go:build windows

package networkservice

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

func TestWindowsListReturnsCurrentUserService(t *testing.T) {
	runner := &fakeWindowsRunner{}
	services := testWindowsServices(runner, nil)

	got, err := services.list(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name() != windowsServiceName {
		t.Fatalf("services = %#v", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("list observed PAC: %#v", runner.calls)
	}
}

func TestWindowsServiceObservesCurrentUserPACSetting(t *testing.T) {
	runner := &fakeWindowsRunner{out: []byte("{\"URL\":\"http://corp.example/proxy.pac\",\"Enabled\":true}")}
	services := testWindowsServices(runner, func() error { return nil })
	service := testWindowsService(services)

	setting, err := service.PAC(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if setting.URL != "http://corp.example/proxy.pac" || !setting.Enabled {
		t.Fatalf("setting = %#v", setting)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want one PAC observation", runner.calls)
	}
}

func TestWindowsSetPACMutatesAndNotifies(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	services := testWindowsServices(runner, func() error {
		notified = true
		return nil
	})
	service := testWindowsService(services)

	if err := service.SetPAC(context.Background(), "http://127.0.0.1/seamless-cors.pac"); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("PAC mutation was not notified")
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "New-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestWindowsDisablePACMutatesAndNotifies(t *testing.T) {
	runner := &fakeWindowsRunner{}
	notified := false
	services := testWindowsServices(runner, func() error {
		notified = true
		return nil
	})
	service := testWindowsService(services)

	if err := service.DisablePAC(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("PAC mutation was not notified")
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "Remove-ItemProperty") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func testWindowsServices(runner commandRunner, notify func() error) *services {
	return &services{runner: runner, notify: notify}
}

func testWindowsService(owner *services) *service {
	return &service{owner: owner, name: windowsServiceName}
}
