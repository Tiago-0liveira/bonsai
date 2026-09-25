package gym

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

var (
	ErrGymUnavailable = errors.New("agym service is unavailable")
	ErrNoActiveRun    = errors.New("no active agent run found")
)

// Service coordinates Bonsai worktree state and the AGYM integration client.
type Service struct {
	repoDir  string
	store    *Store
	client   agym.Client
	clientID string
}

// NewService constructs a Service for repoDir with the specified client.
func NewService(repoDir string, client agym.Client) (*Service, error) {
	var store *Store
	if repoDir != "" {
		s, err := NewStore(repoDir)
		if err == nil {
			store = s
		}
	}

	clientID, _ := GetClientID()

	return &Service{
		repoDir:  repoDir,
		store:    store,
		client:   client,
		clientID: clientID,
	}, nil
}

// Available checks if the AGYM client is configured and responding.
func (s *Service) Available() bool {
	if s.client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := s.client.Info(ctx)
	return err == nil && info != nil && info.SupportsMajor(agym.SupportedProtocolMajor)
}

// Store returns the underlying local state store.
func (s *Service) Store() *Store {
	return s.store
}

// Client returns the underlying AGYM client.
func (s *Service) Client() agym.Client {
	return s.client
}

// Info returns integration metadata and capabilities.
func (s *Service) Info(ctx context.Context) (*agym.InfoData, error) {
	if s.client == nil {
		return nil, ErrGymUnavailable
	}
	return s.client.Info(ctx)
}

// Profiles lists available AGYM profiles.
func (s *Service) Profiles(ctx context.Context) ([]agym.Profile, error) {
	if s.client == nil {
		return nil, ErrGymUnavailable
	}
	return s.client.Profiles(ctx)
}

// Usage retrieves quota status for a profile or all profiles.
func (s *Service) Usage(ctx context.Context, profile string, refresh bool) ([]agym.Usage, error) {
	if s.client == nil {
		return nil, ErrGymUnavailable
	}
	return s.client.Usage(ctx, profile, refresh)
}

// CheckPruneAllowed checks if worktreePath can be safely pruned.
func (s *Service) CheckPruneAllowed(ctx context.Context, worktreePath string) error {
	if s.store == nil {
		return nil
	}
	return CheckPruneAllowed(ctx, s.store, s.client, worktreePath)
}

// GetAgentView builds the current AgentView for worktreePath.
func (s *Service) GetAgentView(ctx context.Context, worktreePath string) (*AgentView, error) {
	if s.store == nil {
		return nil, errors.New("no gym store available")
	}

	ws, err := s.store.ResolveWorkspaceIdentity(worktreePath)
	if err != nil {
		return nil, err
	}

	binding, err := s.store.GetBinding(ws.WorktreeID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		history, historyErr := s.store.LatestHistory(ws.WorktreeID)
		if historyErr != nil {
			return nil, historyErr
		}
		// Recover a run after local binding loss only when every public identity
		// agrees. A branch name or reused path is insufficient.
		if s.client != nil {
			runs, lookupErr := s.client.ListRunsByWorkspace(ctx, ws.WorktreeID)
			if lookupErr == nil {
				for _, candidate := range runs {
					if agym.IsTerminal(candidate.Status) && history != nil &&
						!candidate.CreatedAt.After(history.CreatedAt) {
						continue
					}
					if candidate.Client != "bonsai" || candidate.ClientID != s.clientID ||
						candidate.Workspace.Key != ws.WorktreeID ||
						candidate.Workspace.Cwd != ws.Path ||
						candidate.Workspace.RepositoryKey != ws.RepositoryID {
						continue
					}
					binding = &RunBinding{SchemaVersion: CurrentSchemaVersion, Workspace: *ws,
						RequestID: candidate.RequestID, RunID: candidate.RunID, LeaseID: candidate.LeaseID,
						CreatedAt: candidate.CreatedAt, Submission: SubmissionAcknowledged}
					if info, err := s.client.Info(ctx); err == nil && info != nil {
						binding.AGYMInstanceID = info.InstanceID
					}
					if err := s.store.SaveBinding(binding); err != nil {
						return nil, err
					}
					break
				}
			}
		}
		if binding == nil {
			binding = history
			if binding == nil {
				return nil, nil
			}
		}
	}
	if binding.AGYMInstanceID != "" && s.client != nil {
		info, err := s.client.Info(ctx)
		if err != nil || info == nil || info.InstanceID != binding.AGYMInstanceID {
			return &AgentView{Binding: *binding, ObservedAt: time.Now(), Stale: true,
				Error: "AGYM installation identity could not be verified"}, nil
		}
	}
	if s.client != nil && binding.RunID != "" && binding.Submission == SubmissionAcknowledged {
		run, err := s.client.GetRun(ctx, binding.RunID)
		if err == nil && run != nil && run.RunID == binding.RunID && run.RequestID == binding.RequestID &&
			agym.IsTerminal(run.Status) {
			if archiveErr := s.store.ArchiveBinding(binding); archiveErr != nil {
				return nil, archiveErr
			}
			return &AgentView{Binding: *binding, Run: run, ObservedAt: time.Now()}, nil
		}
	}

	reconciledBinding, run, err := ReconcileBinding(ctx, s.store, s.client, s.clientID, binding)
	view := &AgentView{
		Binding:    *reconciledBinding,
		Run:        run,
		ObservedAt: time.Now(),
	}
	if err != nil {
		view.Error = err.Error()
		view.Stale = true
	}
	return view, nil
}

// Start launches a new agent run in worktreePath.
func (s *Service) Start(ctx context.Context, worktreePath, profile, task string) (*agym.Run, error) {
	if s.client == nil {
		return nil, ErrGymUnavailable
	}
	if s.store == nil {
		return nil, errors.New("no gym store available")
	}
	info, err := s.client.Info(ctx)
	if err != nil {
		return nil, err
	}
	instanceID := ""
	if info != nil {
		if !info.SupportsMajor(agym.SupportedProtocolMajor) {
			return nil, agym.ErrProtocolIncompatible
		}
		instanceID = info.InstanceID
	}

	// Canonicalize and validate path
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolving worktree path: %w", err)
	}
	canonicalPath := git.CanonicalPath(absPath)
	if _, err := os.Stat(canonicalPath); err != nil {
		return nil, fmt.Errorf("worktree directory not found: %s", canonicalPath)
	}

	var startedRun *agym.Run
	err = s.store.WithLock(func() error {
		ws, err := s.store.ResolveWorkspaceIdentityLocked(canonicalPath)
		if err != nil {
			return err
		}

		// Check if active run already exists
		existing, err := s.store.GetBindingLocked(ws.WorktreeID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.Submission == SubmissionPending {
				return agym.ErrWorkspaceBusy
			}
			if existing.RunID != "" {
				probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				r, probeErr := s.client.GetRun(probeCtx, existing.RunID)
				cancel()
				if probeErr == nil && r != nil && !agym.IsTerminal(r.Status) {
					return agym.ErrWorkspaceBusy
				}
				if probeErr != nil || r == nil || r.RunID != existing.RunID {
					return fmt.Errorf("cannot verify previous agent run: %w", agym.ErrWorkspaceBusy)
				}
			}
		}

		if profile == "" {
			profile = "auto"
		}

		requestID := NewUUID()
		binding := &RunBinding{
			SchemaVersion:  CurrentSchemaVersion,
			Workspace:      *ws,
			AGYMInstanceID: instanceID,
			RequestID:      requestID,
			CreatedAt:      time.Now(),
			Submission:     SubmissionPending,
		}

		if err := s.store.SaveBindingLocked(binding); err != nil {
			return fmt.Errorf("saving pending binding: %w", err)
		}

		startReq := &agym.StartRequest{
			RequestID: requestID,
			Client:    "bonsai",
			ClientID:  s.clientID,
			Workspace: agym.WorkspaceIdentityPayload{
				Key:           ws.WorktreeID,
				Cwd:           canonicalPath,
				RepositoryKey: ws.RepositoryID,
			},
			Profile:          profile,
			Task:             task,
			Execution:        "headless",
			PermissionPolicy: "profile-default",
		}

		run, err := s.client.StartRun(ctx, startReq)
		if err != nil {
			// A transport failure leaves the submission outcome unknown. Keep the
			// durable request ID for reconciliation and an explicit same-ID retry.
			if _, known := agym.AsProtocolError(err); known {
				binding.Submission = SubmissionRejected
				if saveErr := s.store.SaveBindingLocked(binding); saveErr != nil {
					return fmt.Errorf("saving rejected binding: %w", saveErr)
				}
			}
			return err
		}

		binding.RunID = run.RunID
		binding.LeaseID = run.LeaseID
		binding.Submission = SubmissionAcknowledged
		if err := s.store.SaveBindingLocked(binding); err != nil {
			return fmt.Errorf("saving acknowledged binding: %w", err)
		}

		startedRun = run
		return nil
	})

	if err != nil {
		return nil, err
	}
	return startedRun, nil
}

// Stop terminates an active run.
func (s *Service) Stop(ctx context.Context, worktreePath, runID string) error {
	if s.client == nil {
		return ErrGymUnavailable
	}

	targetRunID := runID
	if targetRunID == "" && s.store != nil && worktreePath != "" {
		ws, err := s.store.ResolveWorkspaceIdentity(worktreePath)
		if err == nil {
			binding, err := s.store.GetBinding(ws.WorktreeID)
			if err == nil && binding != nil && binding.RunID != "" {
				targetRunID = binding.RunID
			}
		}
	}

	if targetRunID == "" {
		return ErrNoActiveRun
	}

	return s.client.StopRun(ctx, targetRunID)
}

// Attach follows output events of a run and writes text to out.
func (s *Service) Attach(ctx context.Context, worktreePath, runID string, after uint64, out io.Writer) error {
	if s.client == nil {
		return ErrGymUnavailable
	}

	targetRunID := runID
	if targetRunID == "" && s.store != nil && worktreePath != "" {
		ws, err := s.store.ResolveWorkspaceIdentity(worktreePath)
		if err == nil {
			binding, err := s.store.GetBinding(ws.WorktreeID)
			if err == nil && binding != nil && binding.RunID != "" {
				targetRunID = binding.RunID
			}
		}
	}

	if targetRunID == "" {
		return ErrNoActiveRun
	}

	// Drain every historical page before opening the live stream.
	for {
		page, err := s.client.GetEvents(ctx, targetRunID, after, 200)
		if err != nil {
			return err
		}
		if page == nil {
			return agym.ErrInvalidResponse
		}
		for _, ev := range page.Events {
			if ev.Seq <= after {
				continue
			}
			if ev.Seq != after+1 || ev.RunID != targetRunID {
				return fmt.Errorf("%w: event sequence gap", agym.ErrInvalidResponse)
			}
			if ev.Type == "output" {
				payload, err := agym.ParseOutputPayload(ev)
				if err == nil {
					_, _ = io.WriteString(out, payload.Text)
				}
			}
			after = ev.Seq
		}
		if !page.HasMore && page.Snapshot != nil && agym.IsTerminal(page.Snapshot.Status) {
			return nil
		}
		if !page.HasMore {
			break
		}
		if len(page.Events) == 0 {
			return fmt.Errorf("%w: event page made no progress", agym.ErrInvalidResponse)
		}
	}

	// Stream live events
	eventsCh, errCh := s.client.StreamEvents(ctx, targetRunID, after)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
			} else if err != nil {
				return err
			}
		case ev, ok := <-eventsCh:
			if !ok {
				run, err := s.client.GetRun(ctx, targetRunID)
				if err != nil {
					return err
				}
				if run == nil || !agym.IsTerminal(run.Status) {
					return fmt.Errorf("%w: event stream ended before terminal status", agym.ErrTransport)
				}
				return nil
			}
			if ev.Seq <= after {
				continue
			}
			if ev.Seq != after+1 || ev.RunID != targetRunID {
				return fmt.Errorf("%w: event sequence gap", agym.ErrInvalidResponse)
			}
			after = ev.Seq
			if ev.Type == "output" {
				payload, err := agym.ParseOutputPayload(ev)
				if err == nil {
					_, _ = io.WriteString(out, payload.Text)
				}
			}
		}
	}
}

// AutoRunResult holds the details of an automatically created worktree and started agent run.
type AutoRunResult struct {
	Run          *agym.Run
	Branch       string
	WorktreePath string
}

// GenerateBranchName delegates branch name generation to agym using a fast prompt,
// falling back to a deterministic slug if agym fails or is offline.
func (s *Service) GenerateBranchName(ctx context.Context, profile, task string) (string, error) {
	if s.client != nil {
		branch, err := s.client.GenerateBranchName(ctx, profile, task)
		if err == nil && branch != "" {
			return branch, nil
		}
	}
	return agym.SlugifyTask(task), nil
}

// EnsureUniqueBranch returns branch or branch-N if the branch already exists locally.
func EnsureUniqueBranch(repoDir, branch string) (string, error) {
	branches, err := git.ListBranches(repoDir)
	if err != nil {
		return branch, err
	}
	existing := make(map[string]bool)
	for _, b := range branches {
		existing[b] = true
	}
	candidate := branch
	for counter := 2; existing[candidate]; counter++ {
		candidate = fmt.Sprintf("%s-%d", branch, counter)
	}
	return candidate, nil
}

// StartAutoWorktree generates a branch name, creates a new worktree, runs create hooks,
// and starts an agent run inside the new worktree.
func (s *Service) StartAutoWorktree(ctx context.Context, profile, task, explicitBranch string, cfg *config.Config) (*AutoRunResult, error) {
	if s.repoDir == "" {
		return nil, errors.New("cannot create auto-worktree without repository root")
	}
	if cfg == nil {
		var err error
		cfg, err = config.LoadFor(s.repoDir)
		if err != nil || cfg == nil {
			cfg, _ = config.Load("")
		}
	}

	branch := explicitBranch
	if branch == "" {
		var err error
		branch, err = s.GenerateBranchName(ctx, profile, task)
		if err != nil || branch == "" {
			branch = agym.SlugifyTask(task)
		}
	}

	branch, err := EnsureUniqueBranch(s.repoDir, branch)
	if err != nil {
		return nil, fmt.Errorf("resolving unique branch: %w", err)
	}

	wtPath := cfg.WorktreePath(s.repoDir, branch)
	for counter := 2; ; counter++ {
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			break
		}
		branch = fmt.Sprintf("%s-%d", branch, counter)
		wtPath = cfg.WorktreePath(s.repoDir, branch)
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating parent directory: %w", err)
	}

	if err := git.AddWorktreeNewBranch(s.repoDir, wtPath, branch); err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	vars := config.HookVars(s.repoDir, wtPath, branch, cfg.Upstream, 0)
	_ = coreexec.RunHooks(wtPath, cfg.CreateHooks(), vars)

	run, err := s.Start(ctx, wtPath, profile, task)
	res := &AutoRunResult{
		Run:          run,
		Branch:       branch,
		WorktreePath: wtPath,
	}
	return res, err
}
