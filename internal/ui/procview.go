package ui

import (
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

// procView adapts the background-process daemon to the shapes the TUI expects. It
// caches the daemon's record list (refreshed each tick) so rendering is cheap and
// synchronous, while mutations (spawn/kill/restart) go straight to the daemon and
// refresh the cache. Process output is read from the on-disk log files.
type procView struct {
	client *client.Client
	recs   []*procstore.Record
}

func newProcView(repoDir string) *procView {
	return &procView{client: client.For(repoDir)}
}

// refresh reloads the cached record list from the daemon (or on-disk registry).
func (v *procView) refresh() {
	if recs, err := v.client.List(); err == nil {
		v.recs = recs
	}
}

// List returns the cached records for a worktree path, oldest first.
func (v *procView) List(path string) []*procstore.Record {
	var out []*procstore.Record
	for _, r := range v.recs {
		if r.Worktree == path {
			out = append(out, r)
		}
	}
	return out
}

// All returns every cached record.
func (v *procView) All() []*procstore.Record { return v.recs }

// GetByID finds a cached record under path by id.
func (v *procView) GetByID(path string, id int) (*procstore.Record, bool) {
	for _, r := range v.recs {
		if r.Worktree == path && r.ID == id {
			return r, true
		}
	}
	return nil, false
}

// Latest returns the most recently spawned record under path.
func (v *procView) Latest(path string) (*procstore.Record, bool) {
	var last *procstore.Record
	for _, r := range v.recs {
		if r.Worktree == path {
			last = r
		}
	}
	return last, last != nil
}

// Output returns the full captured log of process id. A view with no daemon
// client (render tests, which supply records directly) has no logs to read.
func (v *procView) Output(id int) string {
	if v.client == nil {
		return ""
	}
	var out string
	_ = v.client.Logs(id, false, 0, "", false, func(chunk string) error {
		out += chunk
		return nil
	})
	// Raw terminal output would break out of the pane it is drawn in, so every
	// TUI read goes through the sanitizer.
	return procstore.SanitizeOutput(out)
}

// LastURL returns the last local URL printed by process id (dev-server address).
func (v *procView) LastURL(id int) string {
	return coreexec.LastLocalURL(v.Output(id))
}

// Spawn starts a new background process and refreshes the cache.
func (v *procView) Spawn(path, branch, label, command string) (*procstore.Record, error) {
	rec, err := v.client.Spawn(path, branch, label, command, nil)
	if err != nil {
		return nil, err
	}
	v.refresh()
	return rec, nil
}

// Kill stops a process and refreshes the cache.
func (v *procView) Kill(id int) {
	_, _ = v.client.Kill(id, false, "")
	v.refresh()
}

// Restart restarts a process and refreshes the cache.
func (v *procView) Restart(id int) (*procstore.Record, error) {
	rec, err := v.client.Restart(id)
	if err != nil {
		return nil, err
	}
	v.refresh()
	return rec, nil
}

// Remove drops a terminal process and refreshes the cache.
func (v *procView) Remove(id int) {
	_ = v.client.Remove(id)
	v.refresh()
}

// SetPolicy updates a process's restart policy and refreshes the cache.
func (v *procView) SetPolicy(id int, policy procstore.Policy) (*procstore.Record, error) {
	rec, err := v.client.SetPolicy(id, policy)
	if err != nil {
		return nil, err
	}
	v.refresh()
	return rec, nil
}
