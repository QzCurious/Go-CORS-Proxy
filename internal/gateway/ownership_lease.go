package gateway

import (
	"errors"
	"os"
)

type ownershipLease struct {
	file *os.File
}

func (c *coordinator) AcquireOwnershipLease() (*ownershipLease, bool, error) {
	if err := os.MkdirAll(c.runtimeDir, 0o700); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(c.leasePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	acquired, err := tryLockOwnershipFile(file)
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	if !acquired {
		_ = file.Close()
		return nil, false, nil
	}
	return &ownershipLease{file: file}, true, nil
}

func (l *ownershipLease) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockOwnershipFile(file), file.Close())
}
