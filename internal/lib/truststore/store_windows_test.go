//go:build windows

package truststore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeWindowsRunner struct {
	calls   []string
	listOut []byte
	out     []byte
	err     error
}

func (f *fakeWindowsRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	script := args[len(args)-1]
	if strings.Contains(script, "ConvertTo-Json") {
		return f.listOut, nil
	}
	if strings.Contains(script, "Remove-Item") && f.err == nil {
		f.listOut = []byte("[]")
	}
	return f.out, f.err
}

func TestWindowsAddUsesCertificatePath(t *testing.T) {
	runner := &fakeWindowsRunner{}
	store := testWindowsStore(runner)

	if err := store.Add(context.Background(), `C:\Users\dev\certificate.pem`); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, `Import-Certificate -FilePath 'C:\Users\dev\certificate.pem'`) {
		t.Fatalf("certificate path was not passed to Import-Certificate:\n%s", joined)
	}
	if !strings.Contains(joined, "-ErrorAction Stop") {
		t.Fatalf("Import-Certificate failures are not terminating:\n%s", joined)
	}
}

func TestWindowsAddWrapsCommandFailureAndDiagnostic(t *testing.T) {
	commandErr := errors.New("exit status 1")
	runner := &fakeWindowsRunner{
		out: []byte("Import-Certificate: access denied"),
		err: commandErr,
	}
	store := testWindowsStore(runner)

	err := store.Add(context.Background(), `C:\Users\dev\certificate.pem`)
	if !errors.Is(err, commandErr) {
		t.Fatalf("Add() error = %v, want wrapped command error", err)
	}
	if !strings.Contains(err.Error(), "Import-Certificate: access denied") {
		t.Fatalf("Add() error = %v, want command diagnostic", err)
	}
}

func TestWindowsListReturnsEveryParseableCertificate(t *testing.T) {
	caPEM := testCertificatePEM(t, "CA", true)
	leafPEM := testCertificatePEM(t, "leaf", false)
	ca := mustParsePEM(t, caPEM)
	leaf := mustParsePEM(t, leafPEM)
	raw := []map[string]string{
		{"DER": base64.StdEncoding.EncodeToString(ca.Raw)},
		{"DER": "not base64"},
		{"DER": base64.StdEncoding.EncodeToString(leaf.Raw)},
	}
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeWindowsRunner{listOut: out}
	store := testWindowsStore(runner)

	certificates, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(certificates) != 2 {
		t.Fatalf("listed certificates = %d, want 2", len(certificates))
	}
	if joined := strings.Join(runner.calls, "\n"); strings.Contains(joined, "Where-Object") {
		t.Fatalf("List applied a product-specific certificate filter:\n%s", joined)
	}
}

func TestWindowsRemoveDeletesEachFingerprintWithoutListing(t *testing.T) {
	certificate := mustParsePEM(t, testCertificatePEM(t, "CA", true))
	out, err := json.Marshal(map[string]string{"DER": base64.StdEncoding.EncodeToString(certificate.Raw)})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeWindowsRunner{listOut: out}
	store := testWindowsStore(runner)
	if err := store.Remove(context.Background(), []string{certificateFingerprint(certificate), "ABCDEF"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if strings.Count(joined, "Remove-Item") != 2 {
		t.Fatalf("remove calls:\n%s", joined)
	}
	if !strings.Contains(joined, `'ABCDEF'`) ||
		!strings.Contains(joined, `'`+certificateFingerprint(certificate)+`'`) {
		t.Fatalf("remove calls do not contain every fingerprint:\n%s", joined)
	}
	if strings.Count(joined, "Where-Object { $_.Thumbprint -eq $thumbprint }") != 2 {
		t.Fatalf("remove calls do not match certificates by thumbprint:\n%s", joined)
	}
	if strings.Contains(joined, "ConvertTo-Json") {
		t.Fatalf("removal listed certificates:\n%s", joined)
	}
}

func TestWindowsRemoveReportsPowerShellFailure(t *testing.T) {
	wantErr := errors.New("denied")
	certificate := mustParsePEM(t, testCertificatePEM(t, "CA", true))
	out, err := json.Marshal(map[string]string{"DER": base64.StdEncoding.EncodeToString(certificate.Raw)})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeWindowsRunner{listOut: out, err: wantErr}
	store := testWindowsStore(runner)

	err = store.Remove(context.Background(), []string{certificateFingerprint(certificate)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("remove error = %v", err)
	}
}

func testWindowsStore(runner commandRunner) *Store {
	return &Store{platform: &windowsStore{runner: runner}}
}
