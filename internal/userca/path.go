package userca

import (
	"os"
	"path/filepath"
)

// DefaultDir returns the default location for seamless-cors UserCA material.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors", "ca"), nil
}
