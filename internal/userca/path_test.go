package userca

import (
	"path/filepath"
	"testing"
)

func TestStorageDirUsesXDGStateHome(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state")
	want := filepath.Join(stateHome, "seamless-cors", "userca")

	if got := storageDir(stateHome); got != want {
		t.Fatalf("storage dir = %q, want %q", got, want)
	}
}
