package server

import (
	"os"
	"sync"
)

// logWriter appends a process's combined stdout+stderr to a file, rotating to
// "<path>.1" once the live file passes cap bytes (one backup kept). It is
// goroutine-safe: the child's stdout and stderr both write through it.
type logWriter struct {
	mu         sync.Mutex
	path       string
	cap        int64
	f          *os.File
	size       int64
	generation uint64
	onWrite    func([]byte)
}

func newLogWriter(path string, cap int64, onWrite func([]byte)) (*logWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fi, _ := f.Stat()
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	return &logWriter{path: path, cap: cap, f: f, size: size, onWrite: onWrite}, nil
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.f == nil {
		w.mu.Unlock()
		return len(p), nil // closed; drop
	}
	if w.cap > 0 && w.size+int64(len(p)) > w.cap {
		// Rotation is best-effort from the child's point of view. If rotation
		// fails but the live file can be reopened safely, keep appending rather
		// than breaking stdout/stderr or truncating existing logs.
		if err := w.rotate(); err != nil && w.f == nil {
			w.mu.Unlock()
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	onWrite := w.onWrite
	w.mu.Unlock()

	if n > 0 && onWrite != nil {
		onWrite(p[:n])
	}
	return n, err
}

// rotate closes the live file, replaces "<path>.1" with the current log, and
// opens a fresh live file. Failures are non-destructive: when possible the
// original live file is reopened in append mode so process output can continue.
// Caller holds w.mu.
func (w *logWriter) rotate() error {
	if w.f == nil {
		return nil
	}
	if err := w.f.Close(); err != nil {
		w.f = nil
		_ = w.reopenAppend()
		return err
	}
	w.f = nil

	backup := w.path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		_ = w.reopenAppend()
		return err
	}
	if err := os.Rename(w.path, backup); err != nil {
		_ = w.reopenAppend()
		return err
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// Best-effort rollback. Even if the rename back fails, reopenAppend
		// leaves w.f nil rather than pretending writes succeeded.
		_ = os.Rename(backup, w.path)
		_ = w.reopenAppend()
		return err
	}
	w.f = f
	w.size = 0
	w.generation++
	return nil
}

// reopenAppend restores a usable live writer after a failed rotation.
// Caller holds w.mu.
func (w *logWriter) reopenAppend() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		w.f = nil
		return err
	}
	w.f = f
	if fi, err := f.Stat(); err == nil {
		w.size = fi.Size()
	}
	return nil
}

func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Generation returns the current rotation generation.
func (w *logWriter) Generation() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.generation
}
