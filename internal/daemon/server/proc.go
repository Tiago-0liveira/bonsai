package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/notify"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

var errGenerationMismatch = errors.New("generation mismatch")

// spawn creates a new managed process and starts it.
func (s *Server) spawn(req *protocol.Request) (*procstore.Record, error) {
	if req.Worktree == "" || req.Command == "" {
		return nil, fmt.Errorf("spawn requires worktree and command")
	}
	policy := s.resolvePolicy(req)

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	mp := &managedProc{
		rec: &procstore.Record{
			ID:       id,
			Label:    req.Label,
			Command:  req.Command,
			Worktree: req.Worktree,
			Branch:   req.Branch,
			Policy:   policy,
			Status:   procstore.StatusStarting,
		},
		generation: 1,
	}
	s.procs[id] = mp
	s.mu.Unlock()

	if err := s.start(mp, 1); err != nil {
		s.mu.Lock()
		delete(s.procs, id)
		s.mu.Unlock()
		_ = s.store.RemoveRecord(id)
		return nil, err
	}

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
	return s.snapshot(mp), nil
}

// start launches mp's command as a fresh subprocess in its own process group,
// routing output to its log file. Caller must not hold mp.mu.
func (s *Server) start(mp *managedProc, expectedGen uint64) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.generation != expectedGen || mp.rec.Status != procstore.StatusStarting {
		return errGenerationMismatch
	}

	cap := s.logCap
	if cap <= 0 {
		cap = logCap
	}
	var urlBuf []byte
	logw, err := newLogWriter(s.store.LogPath(mp.rec.ID), cap, func(chunk []byte) {
		mp.mu.Lock()
		urlBuf = append(urlBuf, chunk...)
		if len(urlBuf) > 4096 {
			urlBuf = urlBuf[len(urlBuf)-4096:]
		}
		if url := coreexec.LastLocalURL(string(urlBuf)); url != "" && mp.rec.LastURL != url {
			mp.rec.LastURL = url
			_ = s.store.WriteRecord(mp.rec)
		}
		mp.mu.Unlock()
	})
	if err != nil {
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: -1,
			Text: "failed to start · " + err.Error(),
		})
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = err.Error()
		_ = s.store.WriteRecord(mp.rec)
		return err
	}

	cmd := coreexec.Command(mp.rec.Worktree, mp.rec.Command)
	cmd.Stdout = logw
	cmd.Stderr = logw
	cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1")
	coreexec.SetProcessGroup(cmd)

	if mp.generation != expectedGen || mp.rec.Status != procstore.StatusStarting {
		_ = logw.Close()
		return errGenerationMismatch
	}

	s.appendMarker(mp.rec.ID, procstore.Marker{Kind: procstore.MarkerStart, Text: startText(mp.rec)})
	if err := cmd.Start(); err != nil {
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: -1,
			Text: "failed to start · " + err.Error(),
		})
		_ = logw.Close()
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = err.Error()
		_ = s.store.WriteRecord(mp.rec)
		return err
	}

	mp.cmd = cmd
	mp.logw = logw
	mp.waitDone = make(chan struct{})
	mp.rec.PID = cmd.Process.Pid
	mp.rec.Status = procstore.StatusRunning
	mp.rec.StartedAt = time.Now()
	mp.rec.ExitError = ""
	_ = s.store.WriteRecord(mp.rec)

	started := mp.rec.StartedAt
	done := mp.waitDone
	gen := mp.generation
	go func() {
		werr := cmd.Wait()
		_ = logw.Close()
		s.onExit(mp, werr, started, done, gen)
	}()
	return nil
}

// onExit records process termination, executes state transitions, and applies restart policies.
func (s *Server) onExit(mp *managedProc, werr error, started time.Time, done chan struct{}, gen uint64) {
	mp.mu.Lock()
	if mp.generation != gen {
		mp.mu.Unlock()
		close(done)
		return // Stale notification from an earlier run
	}

	failed := werr != nil
	ran := time.Since(started).Round(time.Millisecond)

	// If a user kill was initiated while running, transition to StatusStopped.
	if mp.rec.Status == procstore.StatusStopping {
		mp.rec.Status = procstore.StatusStopped
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerStopped,
			Text: fmt.Sprintf("stopped by user · ran %s", ran),
		})
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()
		close(done)
		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return
	}

	if time.Since(started) >= stableThreshold {
		mp.consecFails = 0
	}

	restart := false
	switch mp.rec.Policy.Mode {
	case procstore.PolicyAlways:
		restart = mp.consecFails < mp.rec.Policy.MaxRestarts
	case procstore.PolicyOnFailure:
		restart = failed && mp.consecFails < mp.rec.Policy.MaxRestarts
	}

	if restart {
		mp.consecFails++
		mp.rec.Restarts++
		mp.generation++
		scheduledGen := mp.generation
		mp.rec.Status = procstore.StatusBackoff
		_ = s.store.WriteRecord(mp.rec)

		delay := backoff(mp.consecFails)
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerRestart,
			Code: exitCodeOf(werr),
			Text: fmt.Sprintf("%s · restarting in %s (attempt %d/%d)",
				exitText(werr), delay, mp.consecFails, mp.rec.Policy.MaxRestarts),
		})
		id := mp.rec.ID
		mp.restartTimer = time.AfterFunc(delay, func() {
			s.executeScheduledRestart(id, scheduledGen)
		})
		mp.mu.Unlock()
		close(done)
		return
	}

	// Terminal exit: StatusFailed or StatusDone
	if failed {
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = werr.Error()
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: exitCodeOf(werr),
			Text: fmt.Sprintf("%s · ran %s", exitText(werr), ran),
		})
	} else {
		mp.rec.Status = procstore.StatusDone
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerExit,
			Text: fmt.Sprintf("exited 0 · success · ran %s", ran),
		})
	}
	_ = s.store.WriteRecord(mp.rec)
	label := mp.rec.Label
	if label == "" {
		label = mp.rec.Command
	}
	mp.mu.Unlock()
	close(done)

	if failed && s.notifyEnabled() {
		notify.Send("bonsai", fmt.Sprintf("process failed: %s", label))
	}

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
}

// executeScheduledRestart executes a scheduled restart if the generation has not been invalidated.
func (s *Server) executeScheduledRestart(id int, scheduledGen uint64) {
	s.mu.Lock()
	mp, ok := s.procs[id]
	barrier := s.startBarrier
	s.mu.Unlock()
	if !ok {
		return
	}

	mp.mu.Lock()
	if mp.generation != scheduledGen || mp.rec.Status != procstore.StatusBackoff {
		mp.mu.Unlock()
		return
	}
	if mp.rec.Policy.Mode == procstore.PolicyNo || (mp.rec.Policy.MaxRestarts > 0 && mp.consecFails > mp.rec.Policy.MaxRestarts) {
		mp.rec.Status = procstore.StatusFailed
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()
		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return
	}
	mp.restartTimer = nil
	mp.rec.Status = procstore.StatusStarting
	_ = s.store.WriteRecord(mp.rec)
	label := mp.rec.Label
	if label == "" {
		label = mp.rec.Command
	}
	mp.mu.Unlock()

	if barrier != nil {
		barrier(id)
	}

	if err := s.start(mp, scheduledGen); err != nil {
		if errors.Is(err, errGenerationMismatch) {
			return
		}
		mp.mu.Lock()
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = err.Error()
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()

		s.appendMarker(id, procstore.Marker{
			Kind: procstore.MarkerExit,
			Code: -1,
			Text: "restart failed · " + err.Error(),
		})

		if s.notifyEnabled() {
			notify.Send("bonsai", fmt.Sprintf("restart failed: %s", label))
		}

		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
	}
}

// killManaged terminates a process and invalidates any pending restart.
func (s *Server) killManaged(mp *managedProc) {
	mp.mu.Lock()
	status := mp.rec.Status

	if procstore.IsTerminal(status) {
		mp.mu.Unlock()
		return
	}

	if status == procstore.StatusBackoff {
		if mp.restartTimer != nil {
			mp.restartTimer.Stop()
			mp.restartTimer = nil
		}
		mp.generation++
		mp.rec.Status = procstore.StatusStopped
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerStopped,
			Text: "stopped by user",
		})
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()

		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return
	}

	if status == procstore.StatusStarting {
		mp.generation++
		mp.rec.Status = procstore.StatusStopped
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerStopped,
			Text: "stopped by user",
		})
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()

		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return
	}

	if status == procstore.StatusOrphan {
		pid := mp.rec.PID
		alive := procstore.ProcessMatches(pid, mp.rec.StartedAt, mp.rec.Worktree)
		if !alive {
			mp.rec.Status = procstore.StatusLost
			s.appendMarker(mp.rec.ID, procstore.Marker{
				Kind: procstore.MarkerExit,
				Code: -1,
				Text: "orphan process exited",
			})
			_ = s.store.WriteRecord(mp.rec)
			mp.mu.Unlock()
			return
		}
		mp.rec.Status = procstore.StatusStopped
		s.appendMarker(mp.rec.ID, procstore.Marker{
			Kind: procstore.MarkerStopped,
			Text: "orphan stopped by user",
		})
		_ = s.store.WriteRecord(mp.rec)
		mp.mu.Unlock()

		if pid > 0 {
			coreexec.KillPID(pid)
		}
		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return
	}

	if status == procstore.StatusStopping {
		cmd := mp.cmd
		pid := mp.rec.PID
		mp.mu.Unlock()
		if cmd != nil {
			coreexec.KillProcessTree(cmd)
		} else if pid > 0 {
			coreexec.KillPID(pid)
		}
		return
	}

	mp.rec.Status = procstore.StatusStopping
	_ = s.store.WriteRecord(mp.rec)
	cmd := mp.cmd
	pid := mp.rec.PID
	mp.mu.Unlock()

	if cmd != nil {
		coreexec.KillProcessTree(cmd)
	} else if pid > 0 {
		coreexec.KillPID(pid)
	}
}

// restart terminates mp (if running) and starts it fresh with a new generation.
func (s *Server) restart(id int) (*procstore.Record, error) {
	s.mu.Lock()
	mp, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no process #%d", id)
	}

	mp.mu.Lock()
	status := mp.rec.Status
	done := mp.waitDone
	if status == procstore.StatusBackoff {
		if mp.restartTimer != nil {
			mp.restartTimer.Stop()
			mp.restartTimer = nil
		}
		mp.generation++
	}
	mp.mu.Unlock()

	if status == procstore.StatusRunning || status == procstore.StatusStarting || status == procstore.StatusStopping || status == procstore.StatusOrphan {
		s.killManaged(mp)
		if done != nil {
			<-done
		}
	}

	mp.mu.Lock()
	mp.consecFails = 0
	mp.generation++
	gen := mp.generation
	mp.rec.Status = procstore.StatusStarting
	_ = s.store.WriteRecord(mp.rec)
	mp.mu.Unlock()

	if err := s.start(mp, gen); err != nil {
		mp.mu.Lock()
		if mp.rec.Status != procstore.StatusFailed {
			mp.rec.Status = procstore.StatusFailed
			mp.rec.ExitError = err.Error()
			_ = s.store.WriteRecord(mp.rec)
		}
		mp.mu.Unlock()
		s.mu.Lock()
		s.armIdleLocked()
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()
	return s.snapshot(mp), nil
}

// snapshot returns a safe copy of mp's record.
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

// backoff computes exponential backoff for restart attempts.
func backoff(consecFails int) time.Duration {
	d := backoffBase << (consecFails - 1)
	if d <= 0 || d > backoffCap {
		return backoffCap
	}
	return d
}

// appendMarker writes a delimiter line to the process's log.
func (s *Server) appendMarker(id int, mk procstore.Marker) {
	f, err := os.OpenFile(s.store.LogPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(mk.Encode())
	_ = f.Close()
}

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

func exitText(err error) string {
	if err == nil {
		return "exited 0 · success"
	}
	return formatExitError(err)
}

func (s *Server) resolvePolicy(req *protocol.Request) procstore.Policy {
	if req.Policy != nil && procstore.ValidMode(req.Policy.Mode) {
		p := *req.Policy
		if p.MaxRestarts <= 0 {
			p.MaxRestarts = procstore.DefaultPolicy().MaxRestarts
		}
		return p
	}
	if cfg, err := config.Load(s.root); err == nil {
		return cfg.PolicyFor(req.Label, req.Command)
	}
	return procstore.DefaultPolicy()
}
