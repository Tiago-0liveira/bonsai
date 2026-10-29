package gardenbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// launchUserID names this client in the daemon's Git command envelopes.
const launchUserID = "bonsai-tui"

// wireLaunch fills the launch seams for a production backend. Every part is
// optional: a part that cannot be built leaves its seam nil and the launch
// unavailable, never Garden. Nothing here creates an agent account or starts a
// provider; the daemon client connects only when the first command is sent.
func wireLaunch(d *Deps, repoDir string, shared *ghcli.Shared, api *agentsapi.Backend) {
	if api == nil || api.Accounts == nil || api.Sessions == nil {
		return
	}
	resolver, ok := api.Accounts.(agentsapi.LaunchResolver)
	if !ok {
		return
	}
	ops, ok := api.Sessions.(agentsapi.OperationSessions)
	if !ok {
		return
	}
	store, err := launch.Open()
	if err != nil {
		return
	}
	worktrees, err := newLaunchWorktrees(repoDir)
	if err != nil {
		return
	}
	d.LaunchStore = store
	d.LaunchAccounts = resolver
	d.LaunchWorktrees = worktrees
	holder := launchHolder{ops: ops, sessions: api.Sessions}
	d.LaunchHolder = holder
	d.LaunchSessions = holder
	d.HandoffWork = gitWork{}
	if shared != nil {
		prs := pullRequestSubset{svc: shared.Service(repoDir)}
		d.LaunchPRs = prs
		d.HandoffPRs = prs
	}
}

// newLaunchWorktrees builds the daemon-backed isolation authority for the
// repository's main worktree. Its envelope journal is transport replay
// metadata under the Bonsai config directory, not the record of what happened.
func newLaunchWorktrees(repoDir string) (*gitbridge.IsolationClient, error) {
	root, err := coregit.MainRoot(repoDir)
	if err != nil {
		return nil, err
	}
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil {
		root = resolved
	}
	path, err := config.GardenLaunchGitPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	journal, err := gitstore.Open(path)
	if err != nil {
		return nil, err
	}
	return &gitbridge.IsolationClient{
		Caller: client.For(root), RepositoryID: "local", UserID: launchUserID,
		Now: func() time.Time { return time.Now().UTC() }, NewID: randomID, Journal: journal,
	}, nil
}

// launchHolder adapts the agent backend's operation sessions to the launch
// ports. Starting and querying are by durable identity; stopping uses the
// session ID the identity names.
type launchHolder struct {
	ops      agentsapi.OperationSessions
	sessions agentsapi.Sessions
}

var (
	_ launch.Holder            = launchHolder{}
	_ launch.Sessions          = launchHolder{}
	_ launch.DirectorySessions = launchHolder{}
)

func (h launchHolder) Start(ctx context.Context, s launch.StartSession) (launch.SessionObservation, error) {
	res, err := h.ops.StartOperation(ctx, agentsapi.OperationStartRequest{
		StartRequest: agentsapi.StartRequest{
			ProjectID: s.ProjectID, WorktreeID: s.WorktreeID, WorkDir: s.Dir, AccountID: s.AccountID,
			Name: s.Name, Cols: s.Cols, Rows: s.Rows, Launch: s.Launch,
		},
		OperationID: s.Identity.OperationID, SessionID: agents.SessionID(s.Identity.SessionID), Task: s.Task,
	})
	return observation(res, s.Identity), err
}

func (h launchHolder) Query(ctx context.Context, id launch.StartIdentity) (launch.SessionObservation, error) {
	res, err := h.ops.QueryOperation(ctx, agents.SessionID(id.SessionID), id.OperationID)
	if err != nil {
		return launch.SessionObservation{}, err
	}
	return observation(res, id), nil
}

// ActiveAt reports whether any listed agent session, whoever started it, is
// active in dir or below it. A listing that cannot be read is an error, never
// "none".
func (h launchHolder) ActiveAt(ctx context.Context, dir string) (bool, error) {
	list, err := h.sessions.List(ctx)
	if err != nil {
		return false, err
	}
	root := canonicalDir(dir)
	for _, s := range list {
		if !s.Active() {
			continue
		}
		if work := canonicalDir(s.WorkDir); work == root || strings.HasPrefix(work, root+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}

func canonicalDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return dir
}

func (h launchHolder) Stop(ctx context.Context, id launch.StartIdentity) error {
	return h.sessions.Stop(ctx, id.SessionID)
}

// observation reads what a start receipt proves. A receipt for another identity
// is reported empty, so it can never be taken for this run.
func observation(res agentsapi.OperationSession, id launch.StartIdentity) launch.SessionObservation {
	s := res.Session
	if s.ID == "" || s.ID != id.SessionID || s.OperationID != id.OperationID {
		return launch.SessionObservation{}
	}
	activity := "unknown"
	if a := s.Activity; a != nil {
		switch a.State {
		case agentactivity.Working, agentactivity.Idle:
			activity = string(a.State)
		case agentactivity.Waiting:
			if a.Reason == agentactivity.ReasonPermission {
				activity = "waiting-permission"
			}
		}
	}
	return launch.SessionObservation{
		Activity: activity,
		Identity: id, State: s.State, Alive: !res.Closed && s.Active(), Acknowledged: res.Acknowledged, Closed: res.Closed,
		ExitCode: s.ExitCode, StartedAt: s.StartedAt, EndedAt: s.EndedAt,
	}
}
