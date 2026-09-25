package agents

import (
	"os"
	"path/filepath"
	"sync"
)

var lockMu sync.Map

type FileLock struct {
	f  *os.File
	mu *sync.Mutex
}

func LockFile(path string) (*FileLock, error) {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	v, _ := lockMu.LoadOrStore(filepath.Clean(path), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	_ = f.Chmod(0o600)
	if err := lockOSFile(f); err != nil {
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
