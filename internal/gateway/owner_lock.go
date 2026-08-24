package gateway

import (
	"fmt"
	"os"

	"github.com/gofrs/flock"
)

type ownerLock struct {
	file *flock.Flock
}

func (c *coordinator) TryAcquireOwnerLock() (*ownerLock, bool, error) {
	if err := os.MkdirAll(c.runtimeDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create Gateway Runtime Directory: %w", err)
	}
	file := flock.New(c.lockPath, flock.SetPermissions(0o600))
	acquired, err := file.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("acquire Gateway Ownership Lock: %w", err)
	}
	if !acquired {
		return nil, false, nil
	}
	return &ownerLock{file: file}, true, nil
}

func (l *ownerLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return file.Close()
}
