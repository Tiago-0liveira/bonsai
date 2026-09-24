// Package server implements the per-repo bonsai daemon. It owns every background
// process for one repo, supervises them (restart policy + backoff), streams their
// output to on-disk logs, and answers TUI/CLI requests over a Unix socket. A
// flock guarantees a single daemon per repo; the daemon self-registers in the
// global index and self-exits when idle.
package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

const (
	logCap          = 10 << 20 // 10 MiB per log before rotation
	backoffBase     = time.Second
	backoffCap      = 30 * time.Second
	stableThreshold = 10 * time.Second // a run this long resets the failure count
	idleTimeout     = 60 * time.Second // exit after this long with 0 active procs
)

// managedProc is one supervised process (or an adopted record loaded at start).
type managedProc struct {
	mu sync.Mutex

	rec *procstore.Record

	cmd  *exec.Cmd
	logw *logWriter

	generation   uint64
	restartTimer *time.Timer

	consecFails int
	waitDone    chan struct{} // closed when the current subprocess run exits
}

// Server is a running daemon for one repo.
type Server struct {
	root   string
	store  *procstore.Store
	logCap int64

	mu     sync.Mutex
	procs  map[int]*managedProc
	nextID int

	ln        net.Listener
	lock      *procstore.FileLock
	idleTimer *time.Timer

	done         chan struct{}
	closeOnce    sync.Once
	startBarrier func(id int)
}

// SetLogCap sets a custom log capacity before rotation (used by tests).
func (s *Server) SetLogCap(cap int64) {
	s.logCap = cap
}

// SetStartBarrier sets a deterministic test hook called between StatusStarting and start().
func (s *Server) SetStartBarrier(fn func(id int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startBarrier = fn
}

// Run runs the daemon serve loop until shut down.
func (s *Server) Run() error {
	defer s.lock.Unlock()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			s.shutdown(true)
		case <-s.done:
		}
	}()

	go s.acceptLoop()

	s.mu.Lock()
	s.armIdleLocked()
	s.mu.Unlock()

	<-s.done
	s.cleanup()
	return nil
}

// Serve runs the daemon for the repo at root until it is shut down (idle timeout,
// a shutdown request, or a termination signal). If another daemon already holds
// the repo lock, Serve returns nil immediately.
func Serve(root string) error {
	s, err := NewServer(root)
	if err != nil {
		if errors.Is(err, procstore.ErrLocked) {
			return nil // another daemon owns this repo
		}
		return err
	}
	return s.Run()
}

// NewServer initializes and locks a new Server instance for root.
func NewServer(root string) (*Server, error) {
	if canon, err := git.MainRoot(root); err == nil {
		root = canon
	}
	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		return nil, err
	}

	lock, err := procstore.TryLock(store.LockPath())
	if err != nil {
		return nil, err
	}

	// Remove stale socket from crashed daemon
	_ = os.Remove(store.SockPath())
	ln, err := net.Listen("unix", store.SockPath())
	if err != nil {
		_ = lock.Unlock()
		return nil, err
	}

	s := &Server{
		root:   store.Root(),
		store:  store,
		logCap: logCap,
		procs:  map[int]*managedProc{},
		ln:     ln,
		lock:   lock,
		done:   make(chan struct{}),
	}

	_ = os.WriteFile(store.PidPath(), []byte(strconv.Itoa(os.Getpid())+"\n"+strconv.Itoa(protocol.Version)+"\n"), 0o644)
	_ = procstore.Register(s.root, store.SockPath(), os.Getpid())

	s.adoptExisting()
	return s, nil
}

// adoptExisting loads records left by a previous daemon. If active records exist,
// they are explicitly reconciled: if still alive, classified as "orphan"; if dead, classified as "lost".
func (s *Server) adoptExisting() {
	recs, _ := s.store.ListRecords()
	max := 0
	for _, r := range recs {
		if r.ID > max {
			max = r.ID
		}
		if procstore.IsActive(r.Status) || r.Status == procstore.StatusOrphan {
			alive := procstore.ProcessMatches(r.PID, r.StartedAt, r.Worktree)
			if alive {
				r.Status = procstore.StatusOrphan
				text := fmt.Sprintf("daemon restarted · orphan process still alive (pid %d)", r.PID)
				s.appendMarker(r.ID, procstore.Marker{
					Kind: procstore.MarkerStart,
					Text: text,
				})
			} else {
				r.Status = procstore.StatusLost
				text := "daemon exited · process lost"
				s.appendMarker(r.ID, procstore.Marker{
					Kind: procstore.MarkerExit,
					Code: -1,
					Text: text,
				})
			}
			_ = s.store.WriteRecord(r)
		}
		s.procs[r.ID] = &managedProc{rec: r}
	}
	s.nextID = max + 1
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed on shutdown
		}
		go s.handleConn(conn)
	}
}

// stopAllProcesses cleanly terminates all running processes and cancels scheduled restarts.
func (s *Server) stopAllProcesses() {
	s.mu.Lock()
	var runningProcs []*managedProc
	for _, mp := range s.procs {
		mp.mu.Lock()
		status := mp.rec.Status
		if status == procstore.StatusBackoff {
			if mp.restartTimer != nil {
				mp.restartTimer.Stop()
				mp.restartTimer = nil
			}
			mp.generation++
			mp.rec.Status = procstore.StatusStopped
			s.appendMarker(mp.rec.ID, procstore.Marker{
				Kind: procstore.MarkerStopped,
				Text: "stopped by daemon shutdown",
			})
			_ = s.store.WriteRecord(mp.rec)
			mp.mu.Unlock()
			continue
		}
		if status == procstore.StatusRunning || status == procstore.StatusStarting || status == procstore.StatusStopping || status == procstore.StatusOrphan {
			mp.rec.Status = procstore.StatusStopping
			_ = s.store.WriteRecord(mp.rec)
			cmd := mp.cmd
			pid := mp.rec.PID
			mp.mu.Unlock()
			if cmd != nil {
				coreexec.TerminateProcessTree(cmd)
			} else if pid > 0 {
				coreexec.TerminatePID(pid)
			}
			runningProcs = append(runningProcs, mp)
			continue
		}
		mp.mu.Unlock()
	}
	s.mu.Unlock()

	// Wait up to 2.5s for graceful termination before escalating.
	graceDeadline := time.Now().Add(2500 * time.Millisecond)
	shutdownDeadline := time.Now().Add(5 * time.Second)

	for _, mp := range runningProcs {
		mp.mu.Lock()
		done := mp.waitDone
		pid := mp.rec.PID
		cmd := mp.cmd
		mp.mu.Unlock()

		if done != nil {
			graceRem := time.Until(graceDeadline)
			if graceRem > 0 {
				select {
				case <-done:
				case <-time.After(graceRem):
					// Grace period expired; escalate to SIGKILL / taskkill /F
					if cmd != nil {
						coreexec.KillProcessTree(cmd)
					} else if pid > 0 {
						coreexec.KillPID(pid)
					}
					// Wait for exit after escalation
					rem := time.Until(shutdownDeadline)
					if rem > 0 {
						select {
						case <-done:
						case <-time.After(rem):
						}
					}
				}
			} else {
				// Already past grace period, escalate immediately
				if cmd != nil {
					coreexec.KillProcessTree(cmd)
				} else if pid > 0 {
					coreexec.KillPID(pid)
				}
				rem := time.Until(shutdownDeadline)
				if rem > 0 {
					select {
					case <-done:
					case <-time.After(rem):
					}
				}
			}
		} else if pid > 0 {
			// Orphan process without waitDone channel
			pollDeadline := time.Now().Add(1500 * time.Millisecond)
			for time.Now().Before(pollDeadline) && procstore.PidAlive(pid) {
				time.Sleep(20 * time.Millisecond)
			}
			if procstore.PidAlive(pid) {
				coreexec.KillPID(pid)
				escalateDeadline := time.Now().Add(1500 * time.Millisecond)
				for time.Now().Before(escalateDeadline) && procstore.PidAlive(pid) {
					time.Sleep(20 * time.Millisecond)
				}
			}
		}

		mp.mu.Lock()
		if !procstore.IsTerminal(mp.rec.Status) {
			mp.rec.Status = procstore.StatusStopped
			s.appendMarker(mp.rec.ID, procstore.Marker{
				Kind: procstore.MarkerStopped,
				Text: "stopped by daemon shutdown",
			})
			_ = s.store.WriteRecord(mp.rec)
		}
		mp.mu.Unlock()
	}
}

// Stop terminates the server loop, optionally stopping managed processes.
func (s *Server) Stop(killChildren bool) {
	s.shutdown(killChildren)
}

// shutdown stops the server.
func (s *Server) shutdown(killChildren bool) {
	if killChildren {
		s.stopAllProcesses()
	}
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *Server) cleanup() {
	_ = s.ln.Close()
	_ = os.Remove(s.store.SockPath())
	_ = os.Remove(s.store.PidPath())
	_ = procstore.Deregister(s.root)
}

// activeCountLocked returns how many managed processes are active (running, starting, backoff, or stopping).
// Caller holds s.mu.
func (s *Server) activeCountLocked() int {
	n := 0
	for _, mp := range s.procs {
		mp.mu.Lock()
		if mp.rec.Status == procstore.StatusOrphan && !procstore.ProcessMatches(mp.rec.PID, mp.rec.StartedAt, mp.rec.Worktree) {
			mp.rec.Status = procstore.StatusLost
			_ = s.store.WriteRecord(mp.rec)
		}
		if procstore.IsActive(mp.rec.Status) {
			n++
		}
		mp.mu.Unlock()
	}
	return n
}

// runningCountLocked returns how many processes are strictly running a child process.
// Caller holds s.mu.
func (s *Server) runningCountLocked() int {
	n := 0
	for _, mp := range s.procs {
		mp.mu.Lock()
		if mp.rec.Status == procstore.StatusRunning {
			n++
		}
		mp.mu.Unlock()
	}
	return n
}

// armIdleLocked manages the idle exit timer. Caller holds s.mu.
func (s *Server) armIdleLocked() {
	if s.activeCountLocked() > 0 {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
			s.idleTimer = nil
		}
		return
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(idleTimeout, func() {
		s.mu.Lock()
		idle := s.activeCountLocked() == 0
		s.mu.Unlock()
		if idle {
			s.shutdown(false)
		}
	})
}
