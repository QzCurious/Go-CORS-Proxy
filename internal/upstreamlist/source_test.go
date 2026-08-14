package upstreamlist_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestProjectProducesNormalizedDeduplicatedProjection(t *testing.T) {
	projection, err := upstreamlist.Project([]byte("API.EXAMPLE.TEST\napi.example.test\nhttps://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.HostSelectors) != 1 ||
		projection.HostSelectors[0].Hostname != "api.example.test" ||
		len(projection.OriginSelectors) != 1 {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestProjectReturnsConcreteWholeDocumentError(t *testing.T) {
	projection, err := upstreamlist.Project([]byte{0xff})
	var encodingErr *upstreamlist.InvalidEncodingError
	if !errors.As(err, &encodingErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if !upstreamlist.Equal(projection, upstreamlist.Projection{}) {
		t.Fatalf("projection on error = %#v", projection)
	}
}

func TestProjectionZeroValueIsCanonicalEmpty(t *testing.T) {
	projection, err := upstreamlist.Project(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !upstreamlist.Equal(projection, upstreamlist.Projection{}) {
		t.Fatalf("empty projection = %#v", projection)
	}
}

func TestEqualUsesProjectionSemantics(t *testing.T) {
	left, err := upstreamlist.Project([]byte("api.example.test\nhttps://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := upstreamlist.Project([]byte("https://SECURE.EXAMPLE.TEST\nAPI.EXAMPLE.TEST\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !upstreamlist.Equal(left, right) {
		t.Fatalf("projections differ: %#v %#v", left, right)
	}

	withWarning, err := upstreamlist.Project([]byte("api.example.test\nhttps://bad.example.test/path\n"))
	if err != nil {
		t.Fatal(err)
	}
	if upstreamlist.Equal(left, withWarning) {
		t.Fatal("warning change preserved projection identity")
	}
}

func TestBootstrapCreatesImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	if err := upstreamlist.Bootstrap(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapReturnsCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "upstreams.txt")
	if err := upstreamlist.Bootstrap(path); err == nil {
		t.Fatal("Bootstrap succeeded through a non-directory parent")
	}
}

func TestAssessmentDisclosesCreationConsequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "upstreams.txt")
	a := upstreamlist.AssessBootstrap(path)
	if !a.Required || a.Path != path || a.DefaultContents == "" || len(a.MissingParentDirectories) != 2 || a.Fingerprint == "" {
		t.Fatalf("assessment = %#v", a)
	}
}
