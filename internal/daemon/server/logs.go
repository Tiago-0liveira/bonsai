package server

import (
	"bytes"
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
		data, _ := s.store.ReadCombinedLog(req.ID)
		initial := procstore.FilterLog(string(data), req.TailLines, req.Grep, req.GrepInsensitive)
		if initial != "" {
			_ = enc.WriteResponse(&protocol.Response{OK: true, LogChunk: initial})
		}
		_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
		return
	}

	data, _ := os.ReadFile(path)

	offset := int64(len(data))
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

	lastGen := s.logWriterGen(req.ID)
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

		curGen := s.logWriterGen(req.ID)
		fi, statErr := os.Stat(path)

		// Check if log rotated (generation incremented or live file shrunk below offset)
		if curGen != lastGen || (statErr == nil && fi.Size() < offset) {
			// Drain remaining data from previous file (.1)
			if oldF, err := os.Open(path + ".1"); err == nil {
				if _, err := oldF.Seek(offset, 0); err == nil {
					buf := make([]byte, 32<<10)
					for {
						n, _ := oldF.Read(buf)
						if n <= 0 {
							break
						}
						if err := emitChunk(buf[:n], false); err != nil {
							_ = oldF.Close()
							return
						}
					}
				}
				_ = oldF.Close()
			}
			offset = 0
			lastGen = curGen
		}

		// Read new data from live file
		if f, err := os.Open(path); err == nil {
			if _, err := f.Seek(offset, 0); err == nil {
				buf := make([]byte, 32<<10)
				for {
					n, _ := f.Read(buf)
					if n <= 0 {
						break
					}
					offset += int64(n)
					if err := emitChunk(buf[:n], false); err != nil {
						_ = f.Close()
						return
					}
				}
			}
			_ = f.Close()
		}

		if s.isTerminal(req.ID) {
			// One final drain attempt
			if f, err := os.Open(path); err == nil {
				if _, err := f.Seek(offset, 0); err == nil {
					buf := make([]byte, 32<<10)
					for {
						n, _ := f.Read(buf)
						if n <= 0 {
							break
						}
						offset += int64(n)
						if err := emitChunk(buf[:n], false); err != nil {
							_ = f.Close()
							return
						}
					}
				}
				_ = f.Close()
			}
			_ = emitChunk(nil, true)
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		}
	}
}

func (s *Server) logWriterGen(id int) uint64 {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return 0
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.logw != nil {
		return mp.logw.Generation()
	}
	return 0
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
