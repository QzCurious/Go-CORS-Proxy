package userca

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrCAOperationInProgress = errors.New("CA operation already in progress")

type caMutationLease struct {
	file *os.File
}

func acquireCAMutationLease(ctx context.Context, caDir string) (*caMutationLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	leasePath := filepath.Clean(caDir) + ".mutation-lease"
	if err := os.MkdirAll(filepath.Dir(leasePath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(leasePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	acquired, err := tryLockCAFile(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !acquired {
		_ = file.Close()
		return nil, fmt.Errorf("%w", ErrCAOperationInProgress)
	}
	return &caMutationLease{file: file}, nil
}

func (l *caMutationLease) release() error {
	return errors.Join(unlockCAFile(l.file), l.file.Close())
}
