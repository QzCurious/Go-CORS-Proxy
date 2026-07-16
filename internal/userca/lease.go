package userca

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const caLeaseRetry = 25 * time.Millisecond

type caMutationLease struct {
	file *os.File
}

func acquireCAMutationLease(ctx context.Context, caDir string) (*caMutationLease, error) {
	leasePath := filepath.Clean(caDir) + ".mutation-lease"
	if err := os.MkdirAll(filepath.Dir(leasePath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(leasePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		acquired, err := tryLockCAFile(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if acquired {
			return &caMutationLease{file: file}, nil
		}
		timer := time.NewTimer(caLeaseRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *caMutationLease) release() error {
	return errors.Join(unlockCAFile(l.file), l.file.Close())
}
