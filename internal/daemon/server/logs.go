package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// streamLogs sends a process's log to the client. It first emits existing
// content (filtered by TailLines and Grep), then if Follow is requested keeps
// streaming new output until the process is terminal and the log is drained.
// It is rotation-aware: when a log rotates to .1 and a new file is started,
// it drains the remainder of .1 and seamlessly resumes following the new file from offset 0.
// Line safety: partial lines across read chunks or rotations are buffered in a carry
// buffer so FilterGrep only ever inspects complete lines.
func (s *Server) streamLogs(conn net.Conn, enc *protocol.Encoder, req *protocol.Request) {
	path := s.store.LogPath(req.ID)

	var carry []byte

	emitChunk := func(chunk []byte, isFinal bool) error {
		carry = append(carry, chunk...)
		if isFinal {
			if len(carry) > 0 {
				out := procstore.FilterGrep(string(carry), req.Grep, req.GrepInsensitive)
				carry = nil
				if out != "" {
					return enc.WriteResponse(&protocol.Response{OK: true, LogChunk: out})
				}
			}
			return nil
		}
		lastNL := bytes.LastIndexByte(carry, '\n')
		if lastNL >= 0 {
			complete := string(carry[:lastNL+1])
			carry = append([]byte(nil), carry[lastNL+1:]...)
			out := procstore.FilterGrep(complete, req.Grep, req.GrepInsensitive)
			if out != "" {
				return enc.WriteResponse(&protocol.Response{OK: true, LogChunk: out})
			}
		}
		return nil
	}

	if !req.Follow {
		lock := logPathLock(path)
		lock.Lock()
		data, _ := s.store.ReadCombinedLog(req.ID)
		lock.Unlock()
		initial := procstore.FilterLog(string(data), req.TailLines, req.Grep, req.GrepInsensitive)
		if initial != "" {
			_ = enc.WriteResponse(&protocol.Response{OK: true, LogChunk: initial})
		}
		_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
		return
	}

	follower := logFollower{path: path}
	data, err := follower.read()
	if err != nil {
		writeResult(enc, nil, err)
		return
	}
	if req.TailLines > 0 {
		initial := string(data)
		lastNL := bytes.LastIndexByte(data, '\n')
		if lastNL >= 0 {
			initial = procstore.LastLines(string(data[:lastNL+1]), req.TailLines)
			if lastNL+1 < len(data) {
				carry = append([]byte(nil), data[lastNL+1:]...)
			}
		} else if len(data) > 0 {
			carry = append([]byte(nil), data...)
			initial = ""
		}
		initial = procstore.FilterGrep(initial, req.Grep, req.GrepInsensitive)
		if initial != "" {
			if err := enc.WriteResponse(&protocol.Response{OK: true, LogChunk: initial}); err != nil {
				return
			}
		}
	} else {
		if err := emitChunk(data, false); err != nil {
			return
		}
	}

	// Detect client disconnection
	gone := make(chan struct{})
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(gone)
				return
			}
		}
	}()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-gone:
			return
		case <-s.done:
			_ = emitChunk(nil, true)
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		case <-ticker.C:
		}

		// A terminal observation must precede the rotation-aware read. Checking
		// afterwards could require another drain after the live file changed.
		terminal := s.isTerminal(req.ID)
		data, err := follower.read()
		if err != nil {
			writeResult(enc, nil, err)
			return
		}
		if err := emitChunk(data, false); err != nil {
			return
		}
		if terminal {
			_ = emitChunk(nil, true)
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		}
	}
}

// Absolute offsets survive writer replacement and rotations even when the new
// live file has already grown beyond the previous file's offset.
type logFollower struct {
	path       string
	offset     int64
	generation string
}

func (f *logFollower) read() ([]byte, error) {
	lock := logPathLock(f.path)
	lock.Lock()
	defer lock.Unlock()
	state, err := loadLogCursor(f.path)
	if err != nil {
		return nil, fmt.Errorf("read log cursor: %w", err)
	}
	start := state.Base - state.BackupSize
	if f.generation != "" && f.generation != state.Generation {
		// A recreated cursor renumbers offsets, so replay retained bytes rather
		// than suppressing new output with an offset from the old cursor.
		f.offset = start
	}
	f.generation = state.Generation
	// Only the live file and one backup are retained. A slow follower resumes
	// from the oldest retained byte when earlier generations were evicted.
	if f.offset < start {
		f.offset = start
	}
	var data bytes.Buffer
	for _, part := range []struct {
		path string
		base int64
	}{{f.path + ".1", start}, {f.path, state.Base}} {
		file, err := os.Open(part.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open log %s: %w", part.path, err)
		}
		if f.offset < part.base {
			f.offset = part.base
		}
		_, err = file.Seek(f.offset-part.base, io.SeekStart)
		if err == nil {
			var n int64
			n, err = data.ReadFrom(file)
			f.offset += n
		}
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("read log %s: %w", part.path, err)
		}
	}
	return data.Bytes(), nil
}

func (s *Server) isTerminal(id int) bool {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return true
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return procstore.IsTerminal(mp.rec.Status)
}
