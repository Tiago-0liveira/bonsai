package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// Output identity is independent of subprocess attempts and survives daemon restarts.
// Retention is the live log plus one backup (10 MiB each by default). Offsets are
// absolute byte positions; an evicted cursor receives an explicit gap frame.
type logCursorState struct {
	Generation string `json:"generation"`
	Base       int64  `json:"base"`
	BackupSize int64  `json:"backup_size"`
}

var logLocks sync.Map

func logPathLock(path string) *sync.Mutex {
	lock, _ := logLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
func loadLogCursor(path string) (logCursorState, error) {
	var state logCursorState
	data, err := os.ReadFile(path + ".cursor")
	if err == nil {
		err = json.Unmarshal(data, &state)
		return state, err
	}
	if !os.IsNotExist(err) {
		return state, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return state, err
	}
	state.Generation = hex.EncodeToString(id[:])
	if fi, err := os.Stat(path + ".1"); err == nil {
		state.Base = fi.Size()
		state.BackupSize = fi.Size()
	}
	return state, saveLogCursor(path, state)
}
func saveLogCursor(path string, state logCursorState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".cursor.tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".cursor.tmp", path+".cursor")
}

// readProcessOutput snapshots lifecycle and a bounded segment under the same
// locks as lifecycle/rotation. No subscriber queues ever block child output.
func (s *Server) readProcessOutput(id int, generation string, offset int64) (*protocol.Response, error) {
	s.mu.Lock()
	mp := s.procs[id]
	s.mu.Unlock()
	if mp == nil {
		return nil, fmt.Errorf("no process #%d", id)
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	path := s.store.LogPath(id)
	lock := logPathLock(path)
	lock.Lock()
	defer lock.Unlock()
	state, err := loadLogCursor(path)
	if err != nil {
		return nil, err
	}
	rec := *mp.rec
	frame := &protocol.Response{OK: true, Record: &rec, Generation: state.Generation}
	start := state.Base - state.BackupSize
	size := int64(0)
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	end := state.Base + size
	if generation != "" && generation != state.Generation || offset < start || offset > end {
		frame.Gap = true
		offset = start
	}
	frame.Offset = offset
	remaining := int64(32 << 10)
	for _, part := range []struct {
		path       string
		base, size int64
	}{{path + ".1", start, state.BackupSize}, {path, state.Base, size}} {
		if offset < part.base || offset >= part.base+part.size || remaining == 0 {
			continue
		}
		f, err := os.Open(part.path)
		if err != nil {
			return nil, err
		}
		count := min(remaining, part.base+part.size-offset)
		buf := make([]byte, count)
		n, err := f.ReadAt(buf, offset-part.base)
		_ = f.Close()
		if err != nil && err != io.EOF {
			return nil, err
		}
		frame.Data = append(frame.Data, buf[:n]...)
		offset += int64(n)
		remaining -= int64(n)
	}
	frame.Offset = offset
	// A terminal lifecycle is only published once all preceding output is drained.
	if offset < end {
		frame.Record = nil
	}
	return frame, nil
}

func (s *Server) streamProcess(conn net.Conn, enc *protocol.Encoder, req *protocol.Request) {
	gone := make(chan struct{})
	go func() { defer close(gone); _, _ = io.Copy(io.Discard, conn) }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	generation, offset := req.Generation, req.Offset
	var revision uint64
	first := true
	for {
		frame, err := s.readProcessOutput(req.ID, generation, offset)
		if err != nil {
			writeResult(enc, nil, err)
			return
		}
		if first || frame.Gap || len(frame.Data) > 0 || frame.Record != nil && frame.Record.Revision != revision {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := enc.WriteResponse(frame); err != nil {
				return
			}
			if frame.Record != nil {
				revision = frame.Record.Revision
			}
			generation, offset = frame.Generation, frame.Offset
			first = false
		}
		if len(frame.Data) == 32<<10 {
			continue
		}
		select {
		case <-gone:
			return
		case <-s.done:
			return
		case <-ticker.C:
		}
	}
}
