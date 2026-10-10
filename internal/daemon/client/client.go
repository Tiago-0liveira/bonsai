// Package client talks to a repo's bonsai daemon over its Unix socket. Spawn and
// restart auto-start the daemon on demand; read-only calls (List, Logs, Ping)
// fall back to reading the on-disk registry directly when no daemon is running,
// so `bonsai ps`/`logs` work even for an idle repo.
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// ErrIncompatibleDaemon is returned when the daemon's protocol version does not match.
var ErrIncompatibleDaemon = errors.New("daemon protocol version mismatch")

// Client is a connection factory for one repo's daemon.
type Client struct {
	store    *procstore.Store
	userHome bool
}

// For returns a client for the repo whose main worktree is root.
func For(root string) *Client { return &Client{store: procstore.New(root)} }

// ForUserHome returns a client for the user-level daemon (`bonsai web`) whose
// home is dir. That daemon is auto-started with --home, so its root is never
// rewritten to an enclosing Git repository.
func ForUserHome(dir string) *Client {
	return &Client{store: procstore.New(dir), userHome: true}
}

// Store exposes the underlying on-disk store (paths, records).
func (c *Client) Store() *procstore.Store { return c.store }

// dial connects to the daemon socket.
func (c *Client) dial() (net.Conn, error) {
	return net.DialTimeout("unix", c.store.SockPath(), time.Second)
}

func (c *Client) dialContext(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: time.Second}
	return dialer.DialContext(ctx, "unix", c.store.SockPath())
}

// alive reports whether a daemon is currently reachable.
func (c *Client) alive() bool {
	conn, err := c.dial()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// CheckCompatibility checks if the running daemon matches the expected protocol version.
// If incompatible:
// - If the daemon owns running processes, returns an error refusing to disrupt them.
// - If the daemon is idle, shuts it down and restarts a compatible one.
func (c *Client) CheckCompatibility() error {
	resp, err := c.Ping()
	if err != nil {
		return err
	}
	if resp.Version == protocol.Version {
		return nil
	}
	if resp.ProcCount > 0 {
		return fmt.Errorf("%w: daemon version %d, client version %d (%d processes running)",
			ErrIncompatibleDaemon, resp.Version, protocol.Version, resp.ProcCount)
	}
	// Incompatible but idle: shut it down and replace it.
	if err := c.Shutdown(false); err != nil {
		return fmt.Errorf("%w: failed to shutdown incompatible daemon: %v", ErrIncompatibleDaemon, err)
	}
	stopped := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !c.alive() {
			stopped = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !stopped {
		return fmt.Errorf("%w: incompatible daemon survived shutdown deadline", ErrIncompatibleDaemon)
	}
	if err := c.autostart(); err != nil {
		return err
	}
	newResp, err := c.Ping()
	if err != nil {
		return fmt.Errorf("failed to ping replacement daemon: %w", err)
	}
	if newResp.Version != protocol.Version {
		return fmt.Errorf("%w: replacement daemon version %d, expected %d",
			ErrIncompatibleDaemon, newResp.Version, protocol.Version)
	}
	return nil
}

// ensureDaemon guarantees a reachable compatible daemon, auto-starting one if needed.
func (c *Client) ensureDaemon() error {
	if c.alive() {
		return c.CheckCompatibility()
	}
	if err := c.autostart(); err != nil {
		return err
	}
	resp, err := c.Ping()
	if err != nil {
		return fmt.Errorf("ping autostarted daemon: %w", err)
	}
	if resp.Version != protocol.Version {
		_ = c.Shutdown(false)
		return fmt.Errorf("%w: daemon version %d, client version %d",
			ErrIncompatibleDaemon, resp.Version, protocol.Version)
	}
	return nil
}

// autostart launches a detached daemon for this repo and waits for its socket.
func (c *Client) autostart() error {
	bin := os.Getenv("BONSAI_DAEMON_BIN")
	if bin == "" {
		var err error
		bin, err = os.Executable()
		if err != nil {
			return err
		}
		base := filepath.Base(bin)
		if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
			return errors.New("cannot autostart daemon from test binary")
		}
	}
	flag := "--repo"
	if c.userHome {
		flag = "--home"
	}
	cmd := exec.Command(bin, "__daemon", flag, c.store.Root())
	// Fully detach: new session, no controlling terminal, stdio to /dev/null.
	devnull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if devnull != nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
		defer devnull.Close()
	}
	setDetach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.alive() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("daemon did not start in time")
}

// roundtrip sends one request and returns the single terminal response frame.
func (c *Client) roundtrip(req *protocol.Request) (*protocol.Response, error) {
	return c.roundtripContext(context.Background(), req)
}

func (c *Client) roundtripContext(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	conn, err := c.dialContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-cancelled:
		}
	}()
	defer close(cancelled)
	if err := protocol.NewEncoder(conn).WriteRequest(req); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	dec := protocol.NewDecoder(conn)
	for {
		resp, err := dec.ReadResponse()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		if resp.EOF {
			return resp, nil
		}
	}
}

// Spawn starts a shell command in a background process, auto-starting the daemon if needed.
func (c *Client) Spawn(worktree, branch, label, command string, policy *procstore.Policy) (*procstore.Record, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindSpawn, Worktree: worktree, Branch: branch,
		Label: label, Command: command, Policy: policy,
	})
	if err != nil {
		return nil, err
	}
	return resp.Record, nil
}

// SpawnExec starts a shell-free program+argv command. worktree remains the
// owning Git worktree while workingDir may point at a nested project.
func (c *Client) SpawnExec(worktree, branch, workingDir, label, program string, args []string, policy *procstore.Policy) (*procstore.Record, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	display := strings.Join(append([]string{program}, args...), " ")
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindSpawn, Worktree: worktree, Branch: branch,
		Label: label, Command: display, Program: program,
		Args: append([]string(nil), args...), WorkingDir: workingDir, Policy: policy,
	})
	if err != nil {
		return nil, err
	}
	return resp.Record, nil
}

// List returns every process record for the repo. With no live daemon it reads
// the registry directly, marking stale active records as "lost".
func (c *Client) List() ([]*procstore.Record, error) {
	return c.ListContext(context.Background())
}

func (c *Client) ListContext(ctx context.Context) ([]*procstore.Record, error) {
	if c.alive() {
		if err := c.CheckCompatibility(); err != nil {
			return nil, err
		}
		resp, err := c.roundtripContext(ctx, &protocol.Request{Kind: protocol.KindList})
		if err != nil {
			return nil, err
		}
		return resp.Records, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recs, err := c.store.ListRecords()
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if procstore.IsActive(r.Status) || r.Status == procstore.StatusOrphan {
			if procstore.ProcessMatches(r.PID, r.StartedAt, r.Worktree) {
				r.Status = procstore.StatusOrphan
			} else {
				r.Status = procstore.StatusLost
			}
		}
	}
	return recs, nil
}

// hasLivePersistedTarget reports whether a persisted active record matching the
// requested target still names a live OS process. This is used after a daemon
// crash so client operations can restart supervision instead of assuming that
// "no daemon" means "no processes".
func (c *Client) hasLivePersistedTarget(id int, all bool, worktree string) (bool, error) {
	recs, err := c.store.ListRecords()
	if err != nil {
		return false, err
	}
	for _, r := range recs {
		if !procstore.IsActive(r.Status) {
			continue
		}
		match := false
		switch {
		case all:
			match = true
		case worktree != "":
			match = r.Worktree == worktree
		default:
			match = r.ID == id
		}
		if match && procstore.ProcessMatches(r.PID, r.StartedAt, r.Worktree) {
			return true, nil
		}
	}
	return false, nil
}

// Kill stops a single process (id>0), or all when all=true, or a worktree's
// processes when worktree!="".
func (c *Client) Kill(id int, all bool, worktree string) ([]int, error) {
	if !c.alive() {
		live, err := c.hasLivePersistedTarget(id, all, worktree)
		if err != nil {
			return nil, err
		}
		if !live {
			return nil, nil
		}
		// A child survived its daemon. Restart supervision so orphan
		// reconciliation and process-tree termination stay server-owned.
		if err := c.ensureDaemon(); err != nil {
			return nil, err
		}
	}
	if err := c.CheckCompatibility(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindKill, ID: id, All: all, Worktree2: worktree,
	})
	if err != nil {
		return nil, err
	}
	return resp.Killed, nil
}

// Restart restarts a process by id, auto-starting the daemon if needed (so an
// adopted, terminal process can be revived).
func (c *Client) Restart(id int) (*procstore.Record, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindRestart, ID: id})
	if err != nil {
		return nil, err
	}
	return resp.Record, nil
}

// SetPolicy changes a process's restart policy. With a live daemon the change is
// immediate; otherwise it is written straight to the on-disk record.
func (c *Client) SetPolicy(id int, policy procstore.Policy) (*procstore.Record, error) {
	if err := procstore.ValidatePolicy(policy); err != nil {
		return nil, err
	}
	if c.alive() {
		if err := c.CheckCompatibility(); err != nil {
			return nil, err
		}
		resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindSetPolicy, ID: id, Policy: &policy})
		if err != nil {
			return nil, err
		}
		return resp.Record, nil
	}
	rec, err := c.store.ReadRecord(id)
	if err != nil {
		return nil, err
	}
	rec.Policy = policy
	if err := c.store.WriteRecord(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Remove drops a terminal process from the daemon (deleting its record and log).
func (c *Client) Remove(id int) error {
	if !c.alive() {
		live, err := c.hasLivePersistedTarget(id, false, "")
		if err != nil {
			return err
		}
		if !live {
			return c.store.RemoveRecord(id)
		}
		// Do not delete the only metadata for a live orphan. Re-establish the
		// daemon and let the server reject removal until the process is stopped.
		if err := c.ensureDaemon(); err != nil {
			return err
		}
	}
	if err := c.CheckCompatibility(); err != nil {
		return err
	}
	_, err := c.roundtrip(&protocol.Request{Kind: protocol.KindRemove, ID: id})
	return err
}

// Ping returns the daemon's version, pid, and running-process count.
func (c *Client) Ping() (*protocol.Response, error) {
	return c.roundtrip(&protocol.Request{Kind: protocol.KindPing})
}

// Shutdown asks the daemon to exit (only succeeds with 0 running unless force).
func (c *Client) Shutdown(force bool) error {
	if !c.alive() {
		if !force {
			return nil
		}
		live, err := c.hasLivePersistedTarget(0, true, "")
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
		// Forced shutdown is also a cleanup operation: if the daemon crashed but
		// children survived, revive supervision first so they are not left behind.
		if err := c.ensureDaemon(); err != nil {
			return err
		}
	}
	_, err := c.roundtrip(&protocol.Request{Kind: protocol.KindShutdown, Force: force})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !c.alive() {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("daemon survived shutdown deadline")
}

// Logs streams a process's log, invoking onChunk for each chunk until the stream
// ends (process terminal or, when follow, the caller returns an error/ctx ends).
// When no daemon is live it reads the log file once, respecting tailLines and grep options.
func (c *Client) Logs(id int, follow bool, tailLines int, grep string, insensitive bool, onChunk func(string) error) error {
	if !c.alive() {
		data, err := c.store.ReadCombinedLog(id)
		if err != nil {
			return err
		}
		filtered := procstore.FilterLog(string(data), tailLines, grep, insensitive)
		if filtered != "" {
			return onChunk(filtered)
		}
		return nil
	}
	if err := c.CheckCompatibility(); err != nil {
		return err
	}
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	req := &protocol.Request{
		Kind: protocol.KindLogs, ID: id, Follow: follow,
		TailLines: tailLines, Grep: grep, GrepInsensitive: insensitive,
	}
	if err := protocol.NewEncoder(conn).WriteRequest(req); err != nil {
		return err
	}
	dec := protocol.NewDecoder(conn)
	for {
		resp, err := dec.ReadResponse()
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		if resp.LogChunk != "" {
			if err := onChunk(resp.LogChunk); err != nil {
				return err
			}
		}
		if resp.EOF {
			return nil
		}
	}
}

// Attach connects to a process's output stream. Currently backed by log streaming,
// but separated from Logs so future interactive PTY sessions (stdin, resize, signals)
// can be added without altering the Logs API.
func (c *Client) Attach(id int, onChunk func(string) error) error {
	return c.Logs(id, true, 0, "", false, onChunk)
}

// Git executes one structured, allowlisted Git command through the daemon.
// The HTTP API uses this boundary instead of opening repositories or invoking
// git/gh itself.
func (c *Client) Git(command gitbridge.Command) (*gitbridge.Result, error) {
	return c.GitContext(context.Background(), command)
}

func (c *Client) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtripContext(ctx, &protocol.Request{Kind: protocol.KindGit, Git: &command})
	if err != nil {
		return nil, err
	}
	if resp.Git == nil {
		return nil, errors.New("daemon returned no Git result")
	}
	return resp.Git, nil
}

// ServeStart starts or reuses the daemon-owned development stack for a workspace.
func (c *Client) ServeStart(spec procstore.ServeSpec) (*procstore.ServeGroup, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindServeStart, ServeSpec: &spec})
	if err != nil {
		return nil, err
	}
	return resp.ServeGroup, nil
}

// ServeStatus returns the current serve group for workspaceID, if one exists.
func (c *Client) ServeStatus(workspaceID string) (*procstore.ServeGroup, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindServeStatus, ServeGroup: workspaceID})
	if err != nil {
		return nil, err
	}
	return resp.ServeGroup, nil
}

// ServeStop gracefully stops every process in a serve group.
func (c *Client) ServeStop(workspaceID string) error {
	if err := c.ensureDaemon(); err != nil {
		return err
	}
	_, err := c.roundtrip(&protocol.Request{Kind: protocol.KindServeStop, ServeGroup: workspaceID})
	return err
}

// ServeRestart restarts one named process, or the entire group when processName is empty.
func (c *Client) ServeRestart(workspaceID, processName string) (*procstore.ServeGroup, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindServeRestart, ServeGroup: workspaceID, ProcessName: processName,
	})
	if err != nil {
		return nil, err
	}
	return resp.ServeGroup, nil
}

// ServeLogs reads or follows the daemon-owned combined serve-group log.
func (c *Client) ServeLogs(ctx context.Context, workspaceID, processName string, follow bool, tailLines int, grep string, insensitive bool, onChunk func(string) error) error {
	if err := c.ensureDaemon(); err != nil {
		return err
	}
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	req := &protocol.Request{
		Kind: protocol.KindServeLogs, ServeGroup: workspaceID, ProcessName: processName,
		Follow: follow, TailLines: tailLines, Grep: grep, GrepInsensitive: insensitive,
	}
	if err := protocol.NewEncoder(conn).WriteRequest(req); err != nil {
		return err
	}
	if ctx != nil && ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			_ = conn.Close()
		}()
	}
	dec := protocol.NewDecoder(conn)
	for {
		resp, err := dec.ReadResponse()
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		if resp.LogChunk != "" {
			if err := onChunk(resp.LogChunk); err != nil {
				return err
			}
		}
		if resp.EOF {
			return nil
		}
	}
}

// StreamProcess replays retained bytes and follows all attempts until cancelled.
// Cancellation closes the socket even when no output is being produced.
func (c *Client) StreamProcess(ctx context.Context, id int, generation string, offset int64, onFrame func(*protocol.Response) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.ensureDaemon(); err != nil {
		return err
	}
	conn, err := c.dialContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	if err = protocol.NewEncoder(conn).WriteRequest(&protocol.Request{Kind: protocol.KindProcessStream, ID: id, Generation: generation, Offset: offset}); err != nil {
		return err
	}
	dec := protocol.NewDecoder(conn)
	for {
		frame, err := dec.ReadResponse()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if frame.Error != "" {
			return errors.New(frame.Error)
		}
		if err := onFrame(frame); err != nil {
			return err
		}
		if frame.EOF {
			return nil
		}
	}
}

// ProcessAuthorityContext reads records and visibility in one daemon transaction.
func (c *Client) ProcessAuthorityContext(ctx context.Context) ([]*procstore.Record, procstore.ProcessVisibility, error) {
	if !c.alive() {
		records, err := c.ListContext(ctx)
		if err != nil {
			return nil, procstore.ProcessVisibility{}, err
		}
		pending, err := c.store.PendingRemovalRecords()
		if err != nil {
			return nil, procstore.ProcessVisibility{}, err
		}
		for _, intent := range pending {
			found := false
			for i, record := range records {
				if record.ID == intent.ID {
					records[i] = intent
					found = true
					break
				}
			}
			if !found {
				records = append(records, intent)
			}
		}
		visibility, err := c.store.ReadVisibility()
		return records, visibility, err
	}
	if err := c.CheckCompatibility(); err != nil {
		return nil, procstore.ProcessVisibility{}, err
	}
	resp, err := c.roundtripContext(ctx, &protocol.Request{Kind: protocol.KindList})
	if err != nil {
		return nil, procstore.ProcessVisibility{}, err
	}
	if resp.Visibility == nil {
		return nil, procstore.ProcessVisibility{}, fmt.Errorf("daemon lacks process visibility authority")
	}
	return resp.Records, *resp.Visibility, nil
}
func (c *Client) RemoveExecution(id int, stopFirst bool) (procstore.ProcessVisibility, error) {
	if err := c.ensureDaemon(); err != nil {
		return procstore.ProcessVisibility{}, err
	}
	if err := c.CheckCompatibility(); err != nil {
		return procstore.ProcessVisibility{}, err
	}
	resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindRemove, ID: id, StopFirst: stopFirst})
	if err != nil {
		return procstore.ProcessVisibility{}, err
	}
	if resp.Visibility == nil {
		return procstore.ProcessVisibility{}, fmt.Errorf("daemon lacks deletion authority")
	}
	return *resp.Visibility, nil
}
