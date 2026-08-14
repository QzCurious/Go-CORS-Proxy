package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestDefaultUpstreamListPathIsCleanAndAbsolute(t *testing.T) {
	realHome := t.TempDir()
	home := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(realHome, home); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("HOME", home)

	path, err := defaultUpstreamListPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(home, ".seamless-cors", "upstreams.txt"))
	if path != want {
		t.Fatalf("default Upstream List path = %q, want %q", path, want)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("default Upstream List path is not absolute: %q", path)
	}
}

func TestAssessUpstreamListCreationDisclosesCreationConsequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "upstreams.txt")
	consent := assessUpstreamListCreation(path)
	if consent == nil || consent.Path != path || consent.DefaultContents != upstreamlist.DefaultContents ||
		len(consent.MissingParentDirectories) != 2 || consent.Fingerprint == "" {
		t.Fatalf("consent = %#v", consent)
	}
}

func TestAssessUpstreamListCreationReturnsNilForExistingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("user contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if consent := assessUpstreamListCreation(path); consent != nil {
		t.Fatalf("consent = %#v", consent)
	}
}

func TestCreateUpstreamListCreatesDefaultContentsImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "upstreams.txt")
	if err := createUpstreamList(path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != upstreamlist.DefaultContents {
		t.Fatalf("contents = %q", contents)
	}
}

func TestCreateUpstreamListDoesNotReplaceExistingContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstreams.txt")
	if err := os.WriteFile(path, []byte("user contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createUpstreamList(path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user contents\n" {
		t.Fatalf("contents = %q", contents)
	}
}

func TestCreateUpstreamListReturnsCreationFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "upstreams.txt")
	if err := createUpstreamList(path); err == nil {
		t.Fatal("creation succeeded through a non-directory parent")
	}
}
