// Package server implements the per-repo bonsai daemon. It owns every background
// process for one repo, supervises them (restart policy + backoff), streams their
// output to on-disk logs, and answers TUI/CLI requests over a Unix socket. A
// flock guarantees a single daemon per repo; the daemon self-registers in the
// global index and self-exits when idle.
package server

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

const (
	logCap          = 10 << 20 // 10 MiB per log before rotation
	backoffBase     = time.Second
	backoffCap      = 30 * time.Second
	stableThreshold = 10 * time.Second // a run this long resets the failure count
	idleTimeout     = 60 * time.Second // exit after this long with 0 running procs
)

// managedProc is one supervised process (or a terminal record adopted at start).
type managedProc struct {
	mu       sync.Mutex
	rec      *procstore.Record
	cmd      *exec.Cmd
	logw     *logWriter
	killed   bool // user-initiated stop: suppress restart
	terminal bool // has exited and is not going to restart
	running  bool // a live child is currently executing (false during backoff)

	consecFails int
	waitDone    chan struct{} // closed when the current run exits
}

// Server is a running daemon for one repo.
type Server struct {
	root  string
	store *procstore.Store

	mu     sync.Mutex
	procs  map[int]*managedProc
	nextID int

	ln        net.Listener
	lock      *procstore.FileLock
	idleTimer *time.Timer

	done      chan struct{}
	closeOnce sync.Once
}

// Serve runs the daemon for the repo at root until it is shut down (idle timeout,
// a shutdown request, or a termination signal). If another daemon already holds
// the repo lock, Serve returns nil immediately — the caller should just connect
// to the existing socket.
func Serve(root string) error {
	// Canonicalize to the same identity clients compute (git.MainRoot), so the
	// hashed socket path matches regardless of symlinks or the caller's cwd.
	if canon, err := git.MainRoot(root); err == nil {
		root = canon
	}
	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		return err
	}

	lock, err := procstore.TryLock(store.LockPath())
	if errors.Is(err, procstore.ErrLocked) {
		return nil // another daemon owns this repo
	}
	if err != nil {
		return err
	}
	defer lock.Unlock()

	// A stale socket from a crashed daemon would block Listen.
	_ = os.Remove(store.SockPath())
	ln, err := net.Listen("unix", store.SockPath())
	if err != nil {
		return err
	}

	s := &Server{
		root:  store.Root(),
		store: store,
		procs: map[int]*managedProc{},
		ln:    ln,
		lock:  lock,
		done:  make(chan struct{}),
	}

	_ = os.WriteFile(store.PidPath(), []byte(strconv.Itoa(os.Getpid())+"\n"+strconv.Itoa(protocol.Version)+"\n"), 0o644)
	_ = procstore.Register(s.root, store.SockPath(), os.Getpid())

	s.adoptExisting()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			s.shutdown(true) // signaled: kill children (they would break anyway)
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

// adoptExisting loads records left by a previous daemon. Their processes died
// with that daemon, so they become terminal history the user can inspect or
// restart. Sets the id counter past the highest seen.
func (s *Server) adoptExisting() {
	recs, _ := s.store.ListRecords()
	max := 0
	for _, r := range recs {
		if r.ID > max {
			max = r.ID
		}
		if r.Status == procstore.StatusRunning {
			r.Status = procstore.StatusStopped
			s.appendMarker(r.ID, procstore.Marker{
				Kind: procstore.MarkerStopped,
				Text: "daemon exited · process lost",
			})
			_ = s.store.WriteRecord(r)
		}
		s.procs[r.ID] = &managedProc{rec: r, terminal: true, killed: true}
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

// shutdown signals the serve loop to stop. If killChildren, every managed
// process is killed first.
func (s *Server) shutdown(killChildren bool) {
	if killChildren {
		s.mu.Lock()
		procs := make([]*managedProc, 0, len(s.procs))
		for _, mp := range s.procs {
			procs = append(procs, mp)
		}
		s.mu.Unlock()
		for _, mp := range procs {
			s.killManaged(mp)
		}
	}
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *Server) cleanup() {
	_ = s.ln.Close()
	_ = os.Remove(s.store.SockPath())
	_ = os.Remove(s.store.PidPath())
	_ = procstore.Deregister(s.root)
}

// runningCount returns how many managed processes are currently running.
// Caller holds s.mu.
func (s *Server) runningCountLocked() int {
	n := 0
	for _, mp := range s.procs {
		mp.mu.Lock()
		if !mp.terminal {
			n++
		}
		mp.mu.Unlock()
	}
	return n
}

// armIdleLocked (re)starts the idle timer if no process is running, or cancels it
// otherwise. Caller holds s.mu.
func (s *Server) armIdleLocked() {
	if s.runningCountLocked() > 0 {
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
		idle := s.runningCountLocked() == 0
		s.mu.Unlock()
		if idle {
			s.shutdown(false)
		}
	})
}

// resolvePolicy uses the request's explicit policy, else the repo config, else
// the default.
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
