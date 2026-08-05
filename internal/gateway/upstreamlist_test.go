package gateway

import (
	"os"
	"path/filepath"
	"testing"
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
