package server

import (
	"fmt"
	"net"
	"os"
	"sort"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// handleConn serves a single client connection: one request, one or more
// response frames. Log follows keep the connection open until the client leaves.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := protocol.NewDecoder(conn)
	enc := protocol.NewEncoder(conn)

	req, err := dec.ReadRequest()
	if err != nil {
		return
	}

	switch req.Kind {
	case protocol.KindSpawn:
		rec, err := s.spawn(req)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindList:
		writeResult(enc, &protocol.Response{Records: s.list()}, nil)

	case protocol.KindKill:
		killed := s.kill(req)
		writeResult(enc, &protocol.Response{Killed: killed}, nil)

	case protocol.KindRestart:
		rec, err := s.restart(req.ID)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindSetPolicy:
		rec, err := s.setPolicy(req)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindRemove:
		err := s.remove(req.ID)
		writeResult(enc, &protocol.Response{}, err)

	case protocol.KindLogs:
		s.streamLogs(conn, enc, req)

	case protocol.KindPing:
		s.mu.Lock()
		n := s.runningCountLocked()
		s.mu.Unlock()
		writeResult(enc, &protocol.Response{Version: protocol.Version, PID: os.Getpid(), ProcCount: n}, nil)

	case protocol.KindShutdown:
		s.mu.Lock()
		running := s.runningCountLocked()
		s.mu.Unlock()
		if running > 0 && !req.Force {
			writeResult(enc, nil, fmt.Errorf("%d process(es) still running (use --force)", running))
			return
		}
		writeResult(enc, &protocol.Response{}, nil)
		s.shutdown(req.Force)

	default:
		writeResult(enc, nil, fmt.Errorf("unknown request %q", req.Kind))
	}
}

// list returns a record snapshot for every managed process, ordered by id.
func (s *Server) list() []*procstore.Record {
	s.mu.Lock()
	mps := make([]*managedProc, 0, len(s.procs))
	for _, mp := range s.procs {
		mps = append(mps, mp)
	}
	s.mu.Unlock()

	out := make([]*procstore.Record, 0, len(mps))
	for _, mp := range mps {
		out = append(out, s.snapshot(mp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// kill stops processes selected by the request (single id, all, or a worktree).
func (s *Server) kill(req *protocol.Request) []int {
	s.mu.Lock()
	var targets []*managedProc
	for _, mp := range s.procs {
		mp.mu.Lock()
		match := false
		switch {
		case req.All:
			match = !mp.terminal
		case req.Worktree2 != "":
			match = !mp.terminal && mp.rec.Worktree == req.Worktree2
		default:
			match = mp.rec.ID == req.ID && !mp.terminal
		}
		mp.mu.Unlock()
		if match {
			targets = append(targets, mp)
		}
	}
	s.mu.Unlock()

	var killed []int
	for _, mp := range targets {
		s.killManaged(mp)
		killed = append(killed, mp.rec.ID)
	}
	sort.Ints(killed)
	return killed
}

// setPolicy updates a process's restart policy, effective on its next exit.
func (s *Server) setPolicy(req *protocol.Request) (*procstore.Record, error) {
	if req.Policy == nil || !procstore.ValidMode(req.Policy.Mode) {
		return nil, fmt.Errorf("invalid policy")
	}
	s.mu.Lock()
	mp, ok := s.procs[req.ID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no process #%d", req.ID)
	}
	mp.mu.Lock()
	mp.rec.Policy = *req.Policy
	if mp.rec.Policy.MaxRestarts <= 0 {
		mp.rec.Policy.MaxRestarts = procstore.DefaultPolicy().MaxRestarts
	}
	_ = s.store.WriteRecord(mp.rec)
	mp.mu.Unlock()
	return s.snapshot(mp), nil
}

// remove drops a terminal process from the daemon and deletes its record/log.
// A running process must be killed first.
func (s *Server) remove(id int) error {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	mp.mu.Lock()
	terminal := mp.terminal
	mp.mu.Unlock()
	if !terminal {
		return fmt.Errorf("process #%d is still running", id)
	}
	s.mu.Lock()
	delete(s.procs, id)
	s.mu.Unlock()
	_ = s.store.RemoveRecord(id)
	return nil
}

// writeResult sends a single response frame, folding an error into it.
func writeResult(enc *protocol.Encoder, resp *protocol.Response, err error) {
	if err != nil {
		_ = enc.WriteResponse(&protocol.Response{Error: err.Error(), EOF: true})
		return
	}
	resp.OK = true
	resp.EOF = true
	_ = enc.WriteResponse(resp)
}
