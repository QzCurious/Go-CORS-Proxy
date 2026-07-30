package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
	"github.com/QzCurious/seamless-cors/internal/version"
)

func TestRunPrintsVersion(t *testing.T) {
	var stdout bytes.Buffer

	err := run([]string{"version"}, &stdout, io.Discard, commandHandlers{})

	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), version.Current()+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestStartRejectsFlags(t *testing.T) {
	var stderr bytes.Buffer

	err := run([]string{"start", "--ca-trusted"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer) error {
			t.Fatal("start handler should not run")
			return nil
		},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "does not accept flags") {
			t.Fatalf("error = %v", err)
		}
	} else {
		t.Fatal("expected start flag error")
	}
	if !strings.Contains(stderr.String(), "edit upstreams.txt") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInstallRejectsUnexpectedArgs(t *testing.T) {
	var stderr bytes.Buffer

	err := run([]string{"install", "--help"}, io.Discard, &stderr, commandHandlers{
		install: func(io.Writer, io.Writer) error {
			t.Fatal("install handler should not run")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected install arg error")
	}
	if !strings.Contains(err.Error(), "install does not accept arguments") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stderr.String(), "--help") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintsStartCommandErrors(t *testing.T) {
	wantErr := errors.New("start failed")
	var stderr bytes.Buffer

	err := run([]string{"start"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer) error {
			return wantErr
		},
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunPrintsManagedPACLeaseLostGuidance(t *testing.T) {
	var stderr bytes.Buffer

	err := run([]string{"start"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer) error {
			return managedpac.ErrManagedPACLeaseLost
		},
	})

	if !errors.Is(err, managedpac.ErrManagedPACLeaseLost) {
		t.Fatalf("run error = %v", err)
	}
	got := stderr.String()
	for _, want := range []string{
		"error: managed-pac-lease-lost",
		"managed PAC setting was changed outside the gateway",
		"`seamless-cors start`",
		"`seamless-cors stop`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "managed PAC lease lost\n") {
		t.Fatalf("stderr contains raw lease error:\n%s", got)
	}
}

func TestRunDispatchesServe(t *testing.T) {
	var called bool

	err := run([]string{"serve"}, io.Discard, io.Discard, commandHandlers{
		serve: func(io.Writer, io.Writer) error {
			called = true
			return nil
		},
	})

	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("serve handler was not called")
	}
}
