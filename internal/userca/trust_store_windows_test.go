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

func TestWindowsTrustStoreRemoveReportsPowerShellFailure(t *testing.T) {
	wantErr := errors.New("denied")
	runner := &fakeWindowsRunner{out: []byte("access denied"), err: wantErr}
	store := &windowsTrustStore{runner: runner}

	err := store.Remove(context.Background(), []string{"ABCDEF"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("remove error = %v", err)
	}
}
