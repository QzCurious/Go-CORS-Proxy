//go:build windows

package userca

import (
	"context"
	"errors"
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

func TestWindowsTrustStoreRemoveEmptyIsNoOp(t *testing.T) {
	runner := &fakeWindowsRunner{}
	store := &windowsTrustStore{runner: runner}

	if err := store.Remove(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("empty remove calls = %#v", runner.calls)
	}
}

func TestWindowsTrustStoreRemoveReportsPowerShellFailure(t *testing.T) {
	wantErr := errors.New("denied")
	runner := &fakeWindowsRunner{out: []byte("access denied"), err: wantErr}
	store := &windowsTrustStore{runner: runner}

	err := store.Remove(context.Background(), []string{"ABCDEF"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("remove error = %v", err)
	}
}
