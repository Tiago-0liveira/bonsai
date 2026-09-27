package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

const serveRingEntries = 2000

type serveLogRing struct {
	lines []string
}

type serveStreamWriter struct {
	server  *Server
	group   string
	process string
	stream  string
	next    io.Writer
}

func (w *serveStreamWriter) Write(p []byte) (int, error) {
	w.server.appendServeLog(w.group, w.process, w.stream, p)
	return w.next.Write(p)
}

func (s *Server) serveLogPath(group string) string {
	return filepath.Join(s.serveDir(), safeServeID(group)+".log")
}

func (s *Server) appendServeLog(group, process, stream string, chunk []byte) {
	if group == "" || process == "" || len(chunk) == 0 {
		return
	}
	s.serveLogMu.Lock()
	defer s.serveLogMu.Unlock()

	key := group + "\x00" + process + "\x00" + stream
	text := s.serveLogPartials[key] + string(chunk)
	parts := strings.Split(text, "\n")
	s.serveLogPartials[key] = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		line = strings.TrimSuffix(line, "\r")
		entry := fmt.Sprintf("[%s] %s %s %s\n", time.Now().Format("15:04:05"), process, stream, line)
		s.appendServeEntryLocked(group, entry)
	}
}

func (s *Server) appendServeEntryLocked(group, entry string) {
	ring := s.serveLogRings[group]
	if ring == nil {
		ring = &serveLogRing{}
		s.serveLogRings[group] = ring
	}
	ring.lines = append(ring.lines, entry)
	if len(ring.lines) > serveRingEntries {
		ring.lines = append([]string(nil), ring.lines[len(ring.lines)-serveRingEntries:]...)
	}

	if err := os.MkdirAll(s.serveDir(), 0o700); err == nil {
		path := s.serveLogPath(group)
		if info, statErr := os.Stat(path); statErr == nil && info.Size()+int64(len(entry)) > logCap {
			_ = os.Remove(path + ".1")
			_ = os.Rename(path, path+".1")
		}
		if f, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); openErr == nil {
			_, _ = io.WriteString(f, entry)
			_ = f.Close()
		}
	}

	for _, ch := range s.serveLogSubs[group] {
		select {
		case ch <- entry:
		default:
		}
	}
}

func (s *Server) serveLogInitialLocked(group string, tailLines int) string {
	ring := s.serveLogRings[group]
	if ring == nil {
		ring = &serveLogRing{}
		path := s.serveLogPath(group)
		data := []byte{}
		if old, err := os.ReadFile(path + ".1"); err == nil {
			data = append(data, old...)
		}
		if current, err := os.ReadFile(path); err == nil {
			data = append(data, current...)
		}
		text := procstore.LastLines(string(data), serveRingEntries)
		for _, line := range strings.SplitAfter(text, "\n") {
			if line != "" {
				ring.lines = append(ring.lines, line)
			}
		}
		s.serveLogRings[group] = ring
	}
	start := 0
	if tailLines > 0 && len(ring.lines) > tailLines {
		start = len(ring.lines) - tailLines
	}
	return strings.Join(ring.lines[start:], "")
}

func filterServeLog(data, processName, grep string, insensitive bool) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(data, "\n") {
		if line == "" {
			continue
		}
		if processName != "" && !strings.Contains(line, "] "+processName+" ") {
			continue
		}
		if grep != "" {
			haystack, needle := line, grep
			if insensitive {
				haystack, needle = strings.ToLower(haystack), strings.ToLower(needle)
			}
			if !strings.Contains(haystack, needle) {
				continue
			}
		}
		out.WriteString(line)
	}
	return out.String()
}

func (s *Server) subscribeServeLogs(group string, tailLines int) (string, int, <-chan string) {
	s.serveLogMu.Lock()
	defer s.serveLogMu.Unlock()
	initial := s.serveLogInitialLocked(group, tailLines)
	s.serveLogNextSub++
	id := s.serveLogNextSub
	if s.serveLogSubs[group] == nil {
		s.serveLogSubs[group] = map[int]chan string{}
	}
	ch := make(chan string, 512)
	s.serveLogSubs[group][id] = ch
	return initial, id, ch
}

func (s *Server) unsubscribeServeLogs(group string, id int) {
	s.serveLogMu.Lock()
	defer s.serveLogMu.Unlock()
	if subs := s.serveLogSubs[group]; subs != nil {
		delete(subs, id)
		if len(subs) == 0 {
			delete(s.serveLogSubs, group)
		}
	}
}

func (s *Server) closeServeLogSubscribers(group string) {
	s.serveLogMu.Lock()
	defer s.serveLogMu.Unlock()
	for id, ch := range s.serveLogSubs[group] {
		close(ch)
		delete(s.serveLogSubs[group], id)
	}
	delete(s.serveLogSubs, group)
}

func (s *Server) closeAllServeLogSubscribers() {
	s.serveLogMu.Lock()
	defer s.serveLogMu.Unlock()
	for group, subs := range s.serveLogSubs {
		for id, ch := range subs {
			close(ch)
			delete(subs, id)
		}
		delete(s.serveLogSubs, group)
	}
}

func (s *Server) readServeLog(group string) string {
	path := s.serveLogPath(group)
	var data []byte
	if old, err := os.ReadFile(path + ".1"); err == nil {
		data = append(data, old...)
	}
	if current, err := os.ReadFile(path); err == nil {
		data = append(data, current...)
	}
	return string(data)
}

func (s *Server) streamServeLogs(conn net.Conn, enc *protocol.Encoder, req *protocol.Request) {
	if req.ServeGroup == "" {
		_ = enc.WriteResponse(&protocol.Response{Error: "serve group is required", EOF: true})
		return
	}
	if !req.Follow {
		data := s.readServeLog(req.ServeGroup)
		if req.TailLines > 0 {
			data = procstore.LastLines(data, req.TailLines)
		}
		data = filterServeLog(data, req.ProcessName, req.Grep, req.GrepInsensitive)
		if data != "" {
			_ = enc.WriteResponse(&protocol.Response{OK: true, LogChunk: data})
		}
		_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
		return
	}

	initial, subID, ch := s.subscribeServeLogs(req.ServeGroup, req.TailLines)
	defer s.unsubscribeServeLogs(req.ServeGroup, subID)
	initial = filterServeLog(initial, req.ProcessName, req.Grep, req.GrepInsensitive)
	if initial != "" {
		if err := enc.WriteResponse(&protocol.Response{OK: true, LogChunk: initial}); err != nil {
			return
		}
	}

	gone := make(chan struct{})
	go func() {
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		close(gone)
	}()

	for {
		select {
		case <-gone:
			return
		case <-s.done:
			_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
			return
		case entry, ok := <-ch:
			if !ok {
				_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
				return
			}
			filtered := filterServeLog(entry, req.ProcessName, req.Grep, req.GrepInsensitive)
			if filtered == "" {
				continue
			}
			if err := enc.WriteResponse(&protocol.Response{OK: true, LogChunk: filtered}); err != nil {
				return
			}
		}
	}
}

func serveLogContainsCompleteLine(data string) bool {
	return bytes.Contains([]byte(data), []byte("\n"))
}
