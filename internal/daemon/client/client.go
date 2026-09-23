// Package client talks to a repo's bonsai daemon over its Unix socket. Spawn and
// restart auto-start the daemon on demand; read-only calls (List, Logs, Ping)
// fall back to reading the on-disk registry directly when no daemon is running,
// so `bonsai ps`/`logs` work even for an idle repo.
package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// Client is a connection factory for one repo's daemon.
type Client struct {
	store *procstore.Store
}

// For returns a client for the repo whose main worktree is root.
func For(root string) *Client { return &Client{store: procstore.New(root)} }

// Store exposes the underlying on-disk store (paths, records).
func (c *Client) Store() *procstore.Store { return c.store }

// dial connects to the daemon socket.
func (c *Client) dial() (net.Conn, error) {
	return net.DialTimeout("unix", c.store.SockPath(), time.Second)
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

// ensureDaemon guarantees a reachable daemon, auto-starting one if needed.
func (c *Client) ensureDaemon() error {
	if c.alive() {
		return nil
	}
	return c.autostart()
}

// autostart launches a detached daemon for this repo and waits for its socket.
func (c *Client) autostart() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "__daemon", "--repo", c.store.Root())
	// Fully detach: new session, no controlling terminal, stdio to /dev/null.
	devnull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if devnull != nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
		defer devnull.Close()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := protocol.NewEncoder(conn).WriteRequest(req); err != nil {
		return nil, err
	}
	dec := protocol.NewDecoder(conn)
	for {
		resp, err := dec.ReadResponse()
		if err != nil {
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

// Spawn starts a background process, auto-starting the daemon if needed.
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

// List returns every process record for the repo. With no live daemon it reads
// the registry directly, downgrading stale "running" records to "stopped".
func (c *Client) List() ([]*procstore.Record, error) {
	if c.alive() {
		resp, err := c.roundtrip(&protocol.Request{Kind: protocol.KindList})
		if err != nil {
			return nil, err
		}
		return resp.Records, nil
	}
	recs, err := c.store.ListRecords()
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if r.Status == procstore.StatusRunning && !pidAlive(r.PID) {
			r.Status = procstore.StatusStopped
		}
	}
	return recs, nil
}

// Kill stops a single process (id>0), or all when all=true, or a worktree's
// processes when worktree!="".
func (c *Client) Kill(id int, all bool, worktree string) ([]int, error) {
	if !c.alive() {
		return nil, nil // nothing running
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
	if c.alive() {
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
		return c.store.RemoveRecord(id)
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
		return nil
	}
	_, err := c.roundtrip(&protocol.Request{Kind: protocol.KindShutdown, Force: force})
	return err
}

// Logs streams a process's log, invoking onChunk for each chunk until the stream
// ends (process terminal or, when follow, the caller returns an error/ctx ends).
// When no daemon is live it reads the log file once (follow is ignored).
func (c *Client) Logs(id int, follow bool, tailLines int, grep string, insensitive bool, onChunk func(string) error) error {
	if !c.alive() {
		data, err := os.ReadFile(c.store.LogPath(id))
		if err != nil {
			return err
		}
		return onChunk(string(data))
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

// pidAlive reports whether pid is a live process.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
