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

func TestWindowsTrustStoreTrustsCertificateFile(t *testing.T) {
	runner := &fakeWindowsRunner{}
	store := &windowsTrustStore{runner: runner}

	if err := store.trust(context.Background(), `C:\Users\dev\certificate.pem`); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, `Import-Certificate -FilePath 'C:\Users\dev\certificate.pem'`) {
		t.Fatalf("certificate path was not passed to Import-Certificate:\n%s", joined)
	}
}

func TestWindowsTrustStoreRemoveReportsPowerShellFailure(t *testing.T) {
	wantErr := errors.New("denied")
	runner := &fakeWindowsRunner{out: []byte("access denied"), err: wantErr}
	store := &windowsTrustStore{runner: runner}

	err := store.remove(context.Background(), []string{"ABCDEF"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("remove error = %v", err)
	}
}
