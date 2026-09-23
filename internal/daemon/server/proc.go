package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/notify"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// spawn creates a new managed process and starts it.
func (s *Server) spawn(req *protocol.Request) (*procstore.Record, error) {
	if req.Worktree == "" || req.Command == "" {
		return nil, fmt.Errorf("spawn requires worktree and command")
	}
	policy := s.resolvePolicy(req)

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	mp := &managedProc{rec: &procstore.Record{
		ID:       id,
		Label:    req.Label,
		Command:  req.Command,
		Worktree: req.Worktree,
		Branch:   req.Branch,
		Policy:   policy,
	}}
	s.procs[id] = mp
	s.mu.Unlock()

	if err := s.start(mp); err != nil {
		s.mu.Lock()
		delete(s.procs, id)
		s.mu.Unlock()
		return nil, err
	}

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
	return s.snapshot(mp), nil
}

// start launches (or relaunches) mp's command as a fresh subprocess in its own
// process group, appending output to the process log. Caller must not hold mp.mu.
func (s *Server) start(mp *managedProc) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	logw, err := newLogWriter(s.store.LogPath(mp.rec.ID), logCap)
	if err != nil {
		return err
	}
	cmd := shellCmd(mp.rec.Worktree, mp.rec.Command)
	cmd.Stdout = logw
	cmd.Stderr = logw
	// Delimit the run in the log before any of its output can land, so a reader
	// can tell one run from the next (see procstore.Marker).
	s.appendMarker(mp.rec.ID, procstore.Marker{Kind: procstore.MarkerStart, Text: startText(mp.rec)})
	if err := cmd.Start(); err != nil {
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: -1,
			Text: "failed to start · " + err.Error(),
		})
		_ = logw.Close()
		return err
	}

	mp.cmd = cmd
	mp.logw = logw
	mp.killed = false
	mp.terminal = false
	mp.running = true
	mp.waitDone = make(chan struct{})
	mp.rec.PID = cmd.Process.Pid
	mp.rec.Status = procstore.StatusRunning
	mp.rec.StartedAt = time.Now()
	mp.rec.ExitError = ""
	_ = s.store.WriteRecord(mp.rec)

	started := mp.rec.StartedAt
	done := mp.waitDone
	go func() {
		werr := cmd.Wait()
		_ = logw.Close()
		s.onExit(mp, werr, started, done)
	}()
	return nil
}

// onExit records the exit and applies the restart policy.
func (s *Server) onExit(mp *managedProc, werr error, started time.Time, done chan struct{}) {
	mp.mu.Lock()
	mp.running = false

	failed := werr != nil
	if time.Since(started) >= stableThreshold {
		mp.consecFails = 0 // the run was stable; forget past flapping
	}

	restart := false
	if !mp.killed {
		switch mp.rec.Policy.Mode {
		case procstore.PolicyAlways:
			restart = mp.consecFails < mp.rec.Policy.MaxRestarts
		case procstore.PolicyOnFailure:
			restart = failed && mp.consecFails < mp.rec.Policy.MaxRestarts
		}
	}

	if restart {
		mp.consecFails++
		mp.rec.Restarts++
		delay := backoff(mp.consecFails)
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerRestart,
			Code: exitCodeOf(werr),
			Text: fmt.Sprintf("%s · restarting in %s (attempt %d/%d)",
				exitText(werr), delay, mp.consecFails, mp.rec.Policy.MaxRestarts),
		})
		label := mp.rec.Label
		if label == "" {
			label = mp.rec.Command
		}
		id := mp.rec.ID
		mp.mu.Unlock()
		close(done)
		time.AfterFunc(delay, func() {
			// Skip if the process was removed while waiting to restart.
			s.mu.Lock()
			_, ok := s.procs[id]
			s.mu.Unlock()
			if !ok {
				return
			}
			// If it was killed during the backoff window, finalize it as stopped
			// rather than leaving it perpetually "running".
			mp.mu.Lock()
			killed := mp.killed
			mp.mu.Unlock()
			if killed {
				s.finalizeStopped(mp)
				return
			}
			if err := s.start(mp); err != nil {
				notify.Send("bonsai", fmt.Sprintf("restart failed: %s", label))
			}
		})
		return
	}

	// Terminal.
	mp.terminal = true
	ran := time.Since(started).Round(time.Millisecond)
	switch {
	case mp.killed:
		mp.rec.Status = procstore.StatusStopped
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerStopped,
			Text: fmt.Sprintf("stopped by user · ran %s", ran),
		})
	case failed:
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = werr.Error()
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: exitCodeOf(werr),
			Text: fmt.Sprintf("%s · ran %s", exitText(werr), ran),
		})
	default:
		mp.rec.Status = procstore.StatusDone
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Text: fmt.Sprintf("exited 0 · success · ran %s", ran),
		})
	}
	notifyProc := !mp.killed && failed
	label := mp.rec.Label
	if label == "" {
		label = mp.rec.Command
	}
	_ = s.store.WriteRecord(mp.rec)
	mp.mu.Unlock()
	close(done)

	if notifyProc && s.notifyEnabled() {
		notify.Send("bonsai", fmt.Sprintf("process failed: %s", label))
	}

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
}

// killManaged stops a running process and prevents restart. Safe on terminal
// procs (no-op). A process caught mid-backoff (no live child) is finalized as
// stopped right away instead of waiting for its pending restart timer.
func (s *Server) killManaged(mp *managedProc) {
	mp.mu.Lock()
	mp.killed = true
	if mp.terminal {
		mp.mu.Unlock()
		return
	}
	live := mp.running && mp.cmd != nil && mp.cmd.Process != nil
	pid := 0
	if mp.cmd != nil && mp.cmd.Process != nil {
		pid = mp.cmd.Process.Pid
	}
	mp.mu.Unlock()

	if !live {
		s.finalizeStopped(mp)
		return
	}
	// Signal the whole group so a forking shell's child dies too. onExit will
	// finalize the record once cmd.Wait returns.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// finalizeStopped marks a non-terminal process as user-stopped and persists it.
// Idempotent: a no-op once the process is already terminal.
func (s *Server) finalizeStopped(mp *managedProc) {
	mp.mu.Lock()
	if mp.terminal {
		mp.mu.Unlock()
		return
	}
	mp.terminal = true
	mp.rec.Status = procstore.StatusStopped
	s.appendMarker(mp.rec.ID, procstore.Marker{Kind: procstore.MarkerStopped, Text: "stopped by user"})
	_ = s.store.WriteRecord(mp.rec)
	mp.mu.Unlock()

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
}

// restart stops mp (if running), waits for exit, then starts it fresh, resetting
// the failure counter.
func (s *Server) restart(id int) (*procstore.Record, error) {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no process #%d", id)
	}

	mp.mu.Lock()
	running := !mp.terminal && mp.cmd != nil
	done := mp.waitDone
	mp.mu.Unlock()

	if running {
		s.killManaged(mp)
		if done != nil {
			<-done
		}
	}

	mp.mu.Lock()
	mp.consecFails = 0
	mp.mu.Unlock()

	if err := s.start(mp); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
	return s.snapshot(mp), nil
}

// snapshot returns a copy of mp's record safe to send over the wire.
func (s *Server) snapshot(mp *managedProc) *procstore.Record {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	r := *mp.rec
	return &r
}

func (s *Server) notifyEnabled() bool {
	cfg, err := config.Load(s.root)
	return err == nil && cfg.Notifications.Process
}

// backoff returns the delay before the nth consecutive restart: exponential from
// backoffBase, capped at backoffCap.
func backoff(consecFails int) time.Duration {
	d := backoffBase << (consecFails - 1)
	if d <= 0 || d > backoffCap {
		return backoffCap
	}
	return d
}

// appendMarker writes one delimiter line to process id's log. It opens the log
// with its own append-mode handle rather than borrowing the run's logWriter, so
// it works in every lifecycle path — including after the run's writer is closed
// (exit) and while no run exists at all (killed mid-backoff).
func (s *Server) appendMarker(id int, mk procstore.Marker) {
	f, err := os.OpenFile(s.store.LogPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(mk.Encode())
	_ = f.Close()
}

// startText is the body of a run's start marker: which run it is, what it runs,
// and when it began.
func startText(r *procstore.Record) string {
	what := r.Label
	if what == "" {
		what = r.Command
	}
	when := time.Now().Format("15:04:05")
	if r.Restarts > 0 {
		return fmt.Sprintf("restart #%d · %s · %s", r.Restarts, what, when)
	}
	return fmt.Sprintf("started · %s · %s", what, when)
}

// exitCodeOf extracts a child's exit status from cmd.Wait's error: 0 for a
// clean exit, the process's code for a normal failure, -1 when it died from a
// signal or never produced a status.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// exitText describes how a run ended, for a marker body.
func exitText(err error) string {
	if err == nil {
		return "exited 0 · success"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
			return fmt.Sprintf("killed by %s · failed", st.Signal())
		}
		return fmt.Sprintf("exited %d · failed", ee.ExitCode())
	}
	return "failed · " + err.Error()
}

// shellCmd builds `sh -c command` in dir, its own process group, color forced.
func shellCmd(dir, command string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
