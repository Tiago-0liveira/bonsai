package server

import (
	"os"
	"sync"
)

// logWriter appends a process's combined stdout+stderr to a file, rotating to
// "<path>.1" once the live file passes cap bytes (one backup kept). It is
// goroutine-safe: the child's stdout and stderr both write through it.
type logWriter struct {
	mu   sync.Mutex
	path string
	cap  int64
	f    *os.File
	size int64
}

func newLogWriter(path string, cap int64) (*logWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	fi, _ := f.Stat()
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	return &logWriter{path: path, cap: cap, f: f, size: size}, nil
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return len(p), nil // closed; drop
	}
	if w.cap > 0 && w.size+int64(len(p)) > w.cap {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the live file, renames it to "<path>.1", and opens a fresh one.
// Caller holds w.mu.
func (w *logWriter) rotate() {
	_ = w.f.Close()
	_ = os.Rename(w.path, w.path+".1")
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		w.f = nil
		return
	}
	w.f = f
	w.size = 0
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
