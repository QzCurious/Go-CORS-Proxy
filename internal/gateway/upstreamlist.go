package gateway

import (
	"os"
	"path/filepath"
)

// defaultUpstreamListPath is application policy. The resulting path is
// absolute and cleaned, but intentionally not resolved through symlinks; the
// upstream list source enforces ordinary-file safety when it reads the path.
func defaultUpstreamListPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(home, ".seamless-cors", "upstreams.txt"))
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}
