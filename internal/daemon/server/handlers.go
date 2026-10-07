package server

import (
	"fmt"
	"net"
	"os"
	"sort"
	"time"

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
	case protocol.KindGit:
		if req.Git == nil {
			writeResult(enc, nil, fmt.Errorf("missing Git command"))
			return
		}
		result := s.gitCommand(*req.Git)
		writeResult(enc, &protocol.Response{Git: &result}, nil)
	case protocol.KindSpawn:
		rec, err := s.spawn(req)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindList:
		s.lifecycleMu.Lock()
		records := s.list()
		visibility, err := s.store.ReadVisibility()
		s.lifecycleMu.Unlock()
		writeResult(enc, &protocol.Response{Records: records, Visibility: &visibility}, err)

	case protocol.KindKill:
		killed, err := s.kill(req)
		writeResult(enc, &protocol.Response{Killed: killed}, err)

	case protocol.KindRestart:
		rec, err := s.restart(req.ID)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindSetPolicy:
		rec, err := s.setPolicy(req)
		writeResult(enc, &protocol.Response{Record: rec}, err)

	case protocol.KindRemove:
		visibility, err := s.removeExecution(req.ID, req.StopFirst)
		writeResult(enc, &protocol.Response{Visibility: &visibility}, err)

	case protocol.KindServeStart:
		group, err := s.serveStart(req.ServeSpec)
		writeResult(enc, &protocol.Response{ServeGroup: group}, err)

	case protocol.KindServeStatus:
		group, err := s.serveStatus(req.ServeGroup)
		writeResult(enc, &protocol.Response{ServeGroup: group}, err)

	case protocol.KindServeStop:
		err := s.serveStop(req.ServeGroup)
		writeResult(enc, &protocol.Response{}, err)

	case protocol.KindServeRestart:
		group, err := s.serveRestart(req.ServeGroup, req.ProcessName)
		writeResult(enc, &protocol.Response{ServeGroup: group}, err)

	case protocol.KindServeLogs:
		s.streamServeLogs(conn, enc, req)

	case protocol.KindProcessStream:
		s.streamProcess(conn, enc, req)

	case protocol.KindLogs, "attach":
		s.streamLogs(conn, enc, req)

	case protocol.KindPing:
		s.mu.Lock()
		n := s.activeCountLocked()
		s.mu.Unlock()
		writeResult(enc, &protocol.Response{Version: protocol.Version, PID: os.Getpid(), ProcCount: n}, nil)

	case protocol.KindShutdown:
		s.mu.Lock()
		active := s.activeCountLocked()
		s.mu.Unlock()
		if active > 0 && !req.Force {
			writeResult(enc, nil, fmt.Errorf("%d process(es) still running (use --force)", active))
			return
		}
		if req.Force {
			if err := s.stopAllProcesses(); err != nil {
				writeResult(enc, nil, err)
				return
			}
		}
		writeResult(enc, &protocol.Response{}, nil)
		s.closeOnce.Do(func() { close(s.done) })

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
		mp.mu.Lock()
		if mp.rec.Status == procstore.StatusOrphan && !procstore.ProcessMatches(mp.rec.PID, mp.rec.StartedAt, mp.rec.Worktree) {
			mp.rec.Status = procstore.StatusLost
			_ = s.store.WriteRecord(mp.rec)
		}
		mp.mu.Unlock()
		out = append(out, s.snapshot(mp))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// kill stops processes selected by the request (single id, all, or a worktree).
func (s *Server) kill(req *protocol.Request) ([]int, error) {
	s.mu.Lock()
	var targets []*managedProc
	for _, mp := range s.procs {
		mp.mu.Lock()
		match := false
		switch {
		case req.All:
			match = !procstore.IsTerminal(mp.rec.Status)
		case req.Worktree2 != "":
			match = !procstore.IsTerminal(mp.rec.Status) && mp.rec.Worktree == req.Worktree2
		default:
			match = mp.rec.ID == req.ID && !procstore.IsTerminal(mp.rec.Status)
		}
		mp.mu.Unlock()
		if match {
			targets = append(targets, mp)
		}
	}
	s.mu.Unlock()

	var killed []int
	var failed []int
	for _, mp := range targets {
		if s.killManaged(mp) {
			killed = append(killed, mp.rec.ID)
		} else {
			failed = append(failed, mp.rec.ID)
		}
	}
	sort.Ints(killed)
	sort.Ints(failed)
	if len(failed) > 0 {
		return killed, fmt.Errorf("failed to confirm process(es) stopped: %v", failed)
	}
	return killed, nil
}

// setPolicy updates a process's restart policy, effective on its next exit.
func (s *Server) setPolicy(req *protocol.Request) (*procstore.Record, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if req.Policy == nil {
		return nil, fmt.Errorf("invalid policy")
	}
	if err := procstore.ValidatePolicy(*req.Policy); err != nil {
		return nil, err
	}
	s.mu.Lock()
	mp, ok := s.procs[req.ID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no process #%d", req.ID)
	}
	mp.mu.Lock()
	mp.rec.Policy = *req.Policy
	_ = s.store.WriteRecord(mp.rec)
	mp.mu.Unlock()
	return s.snapshot(mp), nil
}

// remove drops a process from the daemon and deletes its record/log.
// Running processes must be killed first; processes waiting in backoff are cancelled and removed.
func (s *Server) remove(id int) error {
	_, err := s.removeExecution(id, false)
	return err
}
func (s *Server) removeExecution(id int, stopFirst bool) (procstore.ProcessVisibility, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	visibility, err := s.store.ReadVisibility()
	if err != nil {
		return visibility, err
	}
	s.mu.Lock()
	mp := s.procs[id]
	s.mu.Unlock()
	if mp == nil {
		return visibility, s.store.RemoveRecord(id)
	}
	mp.mu.Lock()
	status := mp.rec.Status
	done := mp.waitDone
	mp.mu.Unlock()
	if !procstore.IsTerminal(status) && status != procstore.StatusBackoff {
		if !stopFirst {
			return visibility, fmt.Errorf("process #%d is still active", id)
		}
		if !s.killManaged(mp) {
			return visibility, fmt.Errorf("failed to confirm process #%d stopped", id)
		}
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				return visibility, fmt.Errorf("process #%d stop timed out", id)
			}
		}
	}
	mp.mu.Lock()
	if !procstore.IsTerminal(mp.rec.Status) && mp.rec.Status != procstore.StatusBackoff {
		mp.mu.Unlock()
		return visibility, fmt.Errorf("process #%d is still active", id)
	}
	if mp.restartTimer != nil {
		mp.restartTimer.Stop()
		mp.restartTimer = nil
	}
	mp.generation++
	// Cancel retries durably before recording an intent to delete artifacts.
	if mp.rec.Status == procstore.StatusBackoff {
		mp.rec.Status = procstore.StatusStopped
		mp.rec.RetryAt = nil
	}
	if err := s.store.WriteRecord(mp.rec); err != nil {
		mp.mu.Unlock()
		return visibility, err
	}
	err = s.store.BeginRemoval(mp.rec)
	if err != nil {
		mp.mu.Unlock()
		return visibility, err
	}
	if err := s.store.CompleteRemoval(mp.rec); err != nil {
		mp.mu.Unlock()
		return visibility, err
	}
	visibility, err = s.store.ReadVisibility()
	mp.mu.Unlock()
	if err != nil {
		return visibility, err
	}
	s.mu.Lock()
	delete(s.procs, id)
	s.mu.Unlock()
	return visibility, nil
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
