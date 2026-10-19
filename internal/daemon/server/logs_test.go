package server

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func TestLogRotationFollowerMissingBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc.log")
	const live = "live output\n"
	if err := os.WriteFile(path, []byte(live), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveLogCursor(path, logCursorState{Generation: "original", Base: 128, BackupSize: 64}); err != nil {
		t.Fatal(err)
	}
	f := &logFollower{path: path}
	got, err := f.read()
	if err != nil || string(got) != live {
		t.Fatalf("follow without backup = %q, %v; want %q", got, err, live)
	}
}

func TestLogRotationFollowerRecreatedCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc.log")
	w, err := newLogWriter(path, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Earlier rotations can leave offsets beyond the size of all retained data.
	if err := saveLogCursor(path, logCursorState{Generation: "original", Base: 128}); err != nil {
		t.Fatal(err)
	}
	const first = "first output\n"
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	f := &logFollower{path: path}
	got, err := f.read()
	if err != nil || string(got) != first {
		t.Fatalf("initial follow = %q, %v; want %q", got, err, first)
	}
	if err := os.Remove(path + ".cursor"); err != nil {
		t.Fatal(err)
	}
	const next = "new output\n"
	if _, err := w.Write([]byte(next)); err != nil {
		t.Fatal(err)
	}
	got, err = f.read()
	if err != nil || !strings.Contains(string(got), next) {
		t.Fatalf("follow after cursor recreation = %q, %v; want new output %q", got, err, next)
	}
}

func TestLogRotationFollowerCursor(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		name := "same writer"
		if reopen {
			name = "replacement writer"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "proc.log")
			w, err := newLogWriter(path, 80, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = w.Close() })
			write := func(data string) {
				t.Helper()
				if _, err := w.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			read := func(f *logFollower, want string) {
				t.Helper()
				got, err := f.read()
				if err != nil || string(got) != want {
					t.Fatalf("follow = %q, %v; want %q", got, err, want)
				}
			}
			first := "start\nline 111111111111111111\n"
			write(first)
			f := &logFollower{path: path}
			read(f, first)
			if reopen {
				_ = w.Close()
				w, err = newLogWriter(path, 80, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			second := "line 222222222222222222\n"
			write(second)
			// Leave unread bytes in the old file, then rotate to a live file
			// already larger than the last observed offset.
			third := "line 333333333333333333333333333333333333\n"
			write(third)
			read(f, second+third)
			read(f, "")
			// An attaching follower must include the retained start and line 1.
			read(&logFollower{path: path}, first+second+third)
		})
	}
}

// Holding a completed write lets the test rotate after the follower's read but
// before it checks lifecycle state, without scheduling assumptions or processes.
type logWriteBarrierConn struct {
	net.Conn
	writes  int
	reached chan struct{}
	release chan struct{}
}

func (c *logWriteBarrierConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.writes++
	if c.writes == 2 {
		close(c.reached)
		<-c.release
	}
	return n, err
}

func TestLogRotationFollowerTerminalDrain(t *testing.T) {
	s := streamTestServer(t)
	mp := &managedProc{rec: &procstore.Record{ID: 1, Status: procstore.StatusRunning}}
	s.procs[1] = mp
	w, err := newLogWriter(s.store.LogPath(1), 80, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	mp.logw = w
	write := func(data string) {
		t.Helper()
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	first := "start\nline 111111111111111111\n"
	second := "line 222222222222222222\n"
	third := "line 333333333333333333\n"
	fourth := "line 444444444444444444\n"
	write(first)
	a, b := net.Pipe()
	defer b.Close()
	if err := b.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn := &logWriteBarrierConn{Conn: a, reached: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(conn.release) }) }
	done := make(chan struct{})
	defer func() {
		release()
		_ = b.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("log follower did not stop")
		}
	}()
	go func() {
		defer close(done)
		defer a.Close()
		s.streamLogs(conn, protocol.NewEncoder(conn), &protocol.Request{ID: 1, Follow: true})
	}()
	dec := protocol.NewDecoder(b)
	var out strings.Builder
	read := func() *protocol.Response {
		t.Helper()
		resp, err := dec.ReadResponse()
		if err != nil {
			t.Fatal(err)
		}
		out.WriteString(resp.LogChunk)
		return resp
	}
	read()
	write(second)
	read()
	select {
	case <-conn.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("follower did not reach the write barrier")
	}
	// The follower's old offset is larger than the new live file. Line 3
	// remains unread in the backup while line 4 is written to the live file.
	write(third)
	write(fourth)
	mp.mu.Lock()
	mp.rec.Status = procstore.StatusDone
	mp.mu.Unlock()
	release()
	for !read().EOF {
	}
	if got, want := out.String(), first+second+third+fourth; got != want {
		t.Fatalf("followed output = %q, want %q", got, want)
	}
}
