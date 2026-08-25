//go:build windows

package truststore

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
)

type fakeWindowsRunner struct {
	calls   []string
	listOut []byte
	err     error
}

func (f *fakeWindowsRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	script := args[len(args)-1]
	if strings.Contains(script, "Get-ChildItem") {
		return f.listOut, nil
	}
	if strings.Contains(script, "Remove-Item") && f.err == nil {
		f.listOut = []byte("[]")
	}
	return nil, f.err
}

func TestWindowsAddUsesCertificatePath(t *testing.T) {
	runner := &fakeWindowsRunner{}
	store := &Store{runner: runner}

	if err := store.Add(context.Background(), `C:\Users\dev\certificate.pem`); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, `Import-Certificate -FilePath 'C:\Users\dev\certificate.pem'`) {
		t.Fatalf("certificate path was not passed to Import-Certificate:\n%s", joined)
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
	store := &Store{runner: runner}

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

func TestWindowsRemoveAttemptsAllAndVerifiesAbsence(t *testing.T) {
	certificate := mustParsePEM(t, testCertificatePEM(t, "CA", true))
	out, err := json.Marshal(map[string]string{"DER": base64.StdEncoding.EncodeToString(certificate.Raw)})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeWindowsRunner{listOut: out}
	store := &Store{runner: runner}
	if err := store.Remove(context.Background(), []string{certificateFingerprint(certificate), "ABCDEF"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if strings.Count(joined, "Remove-Item") != 2 {
		t.Fatalf("remove calls:\n%s", joined)
	}
}

func TestWindowsRemoveReportsPowerShellFailure(t *testing.T) {
	wantErr := errors.New("denied")
	runner := &fakeWindowsRunner{listOut: []byte("[]"), err: wantErr}
	store := &Store{runner: runner}

	err := store.Remove(context.Background(), []string{"ABCDEF"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("remove error = %v", err)
	}
}

func mustParsePEM(t *testing.T, certificatePEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
