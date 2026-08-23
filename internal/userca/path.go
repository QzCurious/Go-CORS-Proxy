package userca

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

func defaultDir() string {
	return storageDir(xdg.StateHome)
}

func storageDir(stateHome string) string {
	return filepath.Join(stateHome, "seamless-cors", "userca")
}
