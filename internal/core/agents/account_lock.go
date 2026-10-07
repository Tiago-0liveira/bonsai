package agents

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var lockMu sync.Map

type FileLock struct {
	f  *os.File
	mu *sync.Mutex
}

func LockFile(path string) (*FileLock, error) { return LockFileContext(context.Background(), path) }

func LockFileContext(ctx context.Context, path string) (*FileLock, error) {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	v, _ := lockMu.LoadOrStore(filepath.Clean(path), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	for !mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := ctx.Err(); err != nil {
		mu.Unlock()
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	_ = f.Chmod(0o600)
	err = nil
	for {
		locked, lockErr := tryLockOSFile(f)
		if lockErr != nil {
			err = lockErr
			break
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		mu.Unlock()
		return nil, err
	}
	return &FileLock{f: f, mu: mu}, nil
}

func (l *FileLock) Unlock() error {
	if l == nil {
		return nil
	}
	var err error
	if l.f != nil {
		if e := unlockOSFile(l.f); e != nil {
			err = e
		}
		if e := l.f.Close(); err == nil && e != nil {
			err = e
		}
		l.f = nil
	}
	if l.mu != nil {
		l.mu.Unlock()
		l.mu = nil
	}
	return err
}
