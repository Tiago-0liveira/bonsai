package agentactivity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// MaxLineBytes is the longest spool line (without the newline) Tail accepts.
// Real lines are about 200 bytes; the cap bounds holder memory against a
// misbehaving writer.
const MaxLineBytes = 4 << 10

// pollInterval backs up fsnotify, which can miss events (inotify limits,
// network filesystems).
const pollInterval = 500 * time.Millisecond

// AppendSignals appends one JSON line per signal to an existing spool file
// with a single write(2) on an O_APPEND descriptor, so concurrent hook
// processes never interleave lines. It never creates the file: a missing spool
// means the session is gone (or never wanted activity) and the error wraps
// os.ErrNotExist. A final-component symlink or a non-regular file is refused,
// so the path can be swapped after the caller validated it without redirecting
// the append. Details are sanitized on the way in.
func AppendSignals(path string, signals []Signal) error {
	if len(signals) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, s := range signals {
		s.Detail = SanitizeDetail(s.Detail)
		line, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("encode signal: %w", err)
		}
		if len(line) > MaxLineBytes {
			return fmt.Errorf("%w: %d bytes", ErrLineTooLong, len(line))
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	f, err := openSpoolAppend(path)
	if err != nil {
		return fmt.Errorf("open spool: %w", err)
	}
	// The caller's check ran on the path, possibly long before this open: judge
	// what was actually opened.
	if info, serr := f.Stat(); serr != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	n, werr := f.Write(buf.Bytes())
	cerr := f.Close()
	switch {
	case werr != nil:
		return fmt.Errorf("write spool: %w", werr)
	case n != buf.Len():
		return fmt.Errorf("%w: %d of %d bytes", ErrShortWrite, n, buf.Len())
	case cerr != nil:
		return fmt.Errorf("close spool: %w", cerr)
	}
	return nil
}

// Tail follows a spool file from its start and calls fn, in order, for every
// complete valid line. A trailing line without newline waits for the rest;
// malformed lines and lines over MaxLineBytes are skipped. The file must exist.
//
// The returned stop func ends the tail after one last read (so the final hooks
// of a session are not lost) and waits for fn to finish, so it must not be
// called from fn. It is idempotent. Cancelling ctx stops the tail without the
// last read.
func Tail(ctx context.Context, path string, fn func(Signal)) (stop func(), err error) {
	return tail(ctx, path, fn, tailOptions{poll: pollInterval, watch: true})
}

type tailOptions struct {
	poll  time.Duration
	watch bool // tests turn this off to exercise the poll fallback
}

func tail(ctx context.Context, path string, fn func(Signal), opts tailOptions) (func(), error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("tail spool: %w", err)
	}
	var events <-chan fsnotify.Event
	var watcher *fsnotify.Watcher
	if opts.watch {
		// Watching is an optimization: any failure leaves the poll to do the work.
		if w, err := fsnotify.NewWatcher(); err == nil {
			if err = w.Add(filepath.Clean(path)); err == nil {
				watcher, events = w, w.Events
			} else {
				_ = w.Close()
			}
		}
	}

	r := &spoolReader{path: path, fn: fn}
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if watcher != nil {
			defer watcher.Close()
		}
		ticker := time.NewTicker(opts.poll)
		defer ticker.Stop()
		r.drain()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				r.drain()
				return
			case <-events:
				r.drain()
			case <-ticker.C:
				r.drain()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(stopCh) })
		<-done
	}, nil
}

// spoolReader remembers how far the file was consumed.
type spoolReader struct {
	path    string
	fn      func(Signal)
	offset  int64
	pending []byte // start of an unfinished line
	discard bool   // inside an oversize line, skip to its newline
	scratch [32 << 10]byte
}

func (r *spoolReader) drain() {
	f, err := os.Open(r.path)
	if err != nil {
		return // removed with the session; the holder stops us next
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		if st.Size() < r.offset {
			// Truncated or replaced: start over rather than wait forever.
			r.offset, r.pending, r.discard = 0, nil, false
		}
	}
	if _, err := f.Seek(r.offset, io.SeekStart); err != nil {
		return
	}
	for {
		n, err := f.Read(r.scratch[:])
		if n > 0 {
			r.offset += int64(n)
			r.consume(r.scratch[:n])
		}
		if err != nil {
			return // io.EOF ends a pass; anything else retries on the next wake
		}
	}
}

func (r *spoolReader) consume(chunk []byte) {
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			if r.discard {
				return
			}
			if len(r.pending)+len(chunk) > MaxLineBytes {
				r.pending, r.discard = nil, true
				return
			}
			r.pending = append(r.pending, chunk...)
			return
		}
		line := chunk[:i]
		chunk = chunk[i+1:]
		if r.discard {
			r.discard = false
			continue
		}
		if len(r.pending) > 0 {
			if len(r.pending)+len(line) > MaxLineBytes {
				r.pending = nil
				continue
			}
			line = append(r.pending, line...)
			r.pending = nil
		}
		r.emit(line)
	}
}

func (r *spoolReader) emit(line []byte) {
	if len(line) == 0 || len(line) > MaxLineBytes {
		return
	}
	var s Signal
	if err := json.Unmarshal(line, &s); err != nil || s.Kind == "" {
		return
	}
	s.Detail = SanitizeDetail(s.Detail)
	r.fn(s)
}
