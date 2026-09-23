package server

import (
	"net"
	"os"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// streamLogs sends a process's log to the client. It first emits the existing
// content (optionally the last TailLines lines, optionally grep-filtered), then
// if Follow is set keeps emitting new lines until the process is terminal and the
// file is drained, or the client disconnects.
func (s *Server) streamLogs(conn net.Conn, enc *protocol.Encoder, req *protocol.Request) {
	path := s.store.LogPath(req.ID)

	data, _ := os.ReadFile(path)
	initial := string(data)
	if req.TailLines > 0 {
		initial = lastLines(initial, req.TailLines)
	}
	initial = filterGrep(initial, req.Grep, req.GrepInsensitive)
	if initial != "" {
		if err := enc.WriteResponse(&protocol.Response{OK: true, LogChunk: initial}); err != nil {
			return
		}
	}

	if !req.Follow {
		_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
		return
	}

	// Detect the client hanging up (it stops reading / closes the conn).
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

	offset := int64(len(data))
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-gone:
			return
		case <-s.done:
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		case <-ticker.C:
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, 0); err == nil {
			chunk := make([]byte, 32<<10)
			for {
				n, _ := f.Read(chunk)
				if n <= 0 {
					break
				}
				offset += int64(n)
				out := filterGrep(string(chunk[:n]), req.Grep, req.GrepInsensitive)
				if out != "" {
					if werr := enc.WriteResponse(&protocol.Response{OK: true, LogChunk: out}); werr != nil {
						_ = f.Close()
						return
					}
				}
			}
		}
		_ = f.Close()

		if s.isTerminal(req.ID) {
			// Process ended; one more drain already happened above.
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		}
	}
}

// isTerminal reports whether process id has exited and is not restarting.
func (s *Server) isTerminal(id int) bool {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return true
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.terminal
}

// lastLines returns the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

// filterGrep keeps only lines containing pattern (empty pattern = keep all).
func filterGrep(s, pattern string, insensitive bool) string {
	if pattern == "" {
		return s
	}
	pat := pattern
	if insensitive {
		pat = strings.ToLower(pat)
	}
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		hay := line
		if insensitive {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, pat) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
