package userca

import (
	"os"
	"path/filepath"
)

func defaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors", "ca"), nil
}
