package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

var _ domain.IsolationService = (*Service)(nil)

// isolationVerifyTimeout bounds the read-back after a push.
const isolationVerifyTimeout = 45 * time.Second

func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// resourceBucket holds one record per operation: the proposal it was bound to
// before any effect, and how far creation, hooks and removal got. It is
// ownership evidence for recovery, not a second command journal.
const resourceBucket = "isolation_resources"

// Resource phases and hook states.
const (
	phaseIntent   = "intent"
	phaseCreated  = "created"
	phaseRemoving = "removing"
	phaseRemoved  = "removed"

	hooksPending = "pending"
	hooksRunning = "running"
	hooksDone    = "done"
	hooksFailed  = "failed"
	hooksUnknown = "unknown"
)

type resource struct {
	Proposal  domain.IsolationProposal `json:"proposal"`
	Phase     string                   `json:"phase"`
	Hooks     string                   `json:"hooks"`
	PushHead  string                   `json:"push_head,omitempty"`
	UpdatedAt time.Time                `json:"updated_at"`
}

var (
	operationIDRE = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
	// Branch and base text reaches configured hooks as variables, so only
	// characters no shell treats specially are accepted.
	launchRefRE = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)
	remoteRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	repoRE      = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	shaRE       = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	hashRE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (r *repository) git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := r.IsolationRun(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// fixed hides a Git failure's text, which can carry remote URLs and tokens.
// Cancellation passes through so callers can tell an interrupted effect.
func fixed(ctx context.Context, err error, e error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	// A typed domain error has a fixed message by construction.
	var typed *domain.Error
	if errors.As(err, &typed) {
		return typed
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return e
}

func (r *repository) isolationSettingsPath() (string, error) {
	return config.ProjectRootsPath()
}

// validRequest checks the shape of a request without touching Git.
func validRequest(req domain.IsolationRequest) bool {
	return operationIDRE.MatchString(req.OperationID) &&
		launchRefRE.MatchString(req.Branch) && !strings.HasPrefix(req.Branch, "-") && !strings.HasPrefix(req.Branch, "/") &&
		!strings.HasSuffix(req.Branch, "/") && !strings.Contains(req.Branch, "//") && !strings.Contains(req.Branch, "..") &&
		launchRefRE.MatchString(req.BaseRef) && !strings.HasPrefix(req.BaseRef, "-") &&
		shaRE.MatchString(req.BaseSHA) && remoteRE.MatchString(req.RemoteName) && repoRE.MatchString(req.Repository)
}

func (r *repository) validateRequest(ctx context.Context, req domain.IsolationRequest) error {
	if req.RepositoryID != r.ID || !validRequest(req) {
		return domain.ErrInvalid
	}
	if _, err := r.git(ctx, r.Root, "check-ref-format", "refs/heads/"+req.Branch); err != nil {
		return fixed(ctx, err, domain.E("invalid", "invalid branch name"))
	}
	return nil
}

func (r *repository) validateProposal(ctx context.Context, p domain.IsolationProposal) error {
	if err := r.validateRequest(ctx, p.IsolationRequest); err != nil {
		return err
	}
	if !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path || !hashRE.MatchString(p.ConfigHash) {
		return domain.ErrInvalid
	}
	return nil
}

// verifyRemote proves the selected remote, for fetch and push alike, names the
// verified repository. A fork, another host or a push URL that differs from the
// fetch URL is refused, so a push can never leave through a look-alike remote.
func (r *repository) verifyRemote(ctx context.Context, req domain.IsolationRequest) error {
	// Every configured URL counts: a push goes to all push URLs of the remote, so
	// one foreign second URL would publish the work elsewhere.
	for _, args := range [][]string{{"remote", "get-url", "--all", req.RemoteName}, {"remote", "get-url", "--all", "--push", req.RemoteName}} {
		raw, err := r.git(ctx, r.Root, args...)
		if err != nil || raw == "" {
			return fixed(ctx, err, domain.ErrOwnership)
		}
		for _, line := range strings.Split(raw, "\n") {
			identity := sanitizeRemoteIdentity(req.RemoteName, strings.TrimSpace(line))
			if identity.FullName == "" || !strings.EqualFold(identity.FullName, req.Repository) {
				return domain.ErrOwnership
			}
		}
	}
	return nil
}

// localBranchTip returns the commit of the exact local branch.
func (r *repository) localBranchTip(ctx context.Context, branch string) (string, bool, error) {
	out, err := r.git(ctx, r.Root, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/"+branch)
	if err != nil {
		return "", false, fixed(ctx, err, domain.ErrOwnership)
	}
	for _, line := range strings.Split(out, "\n") {
		if name, sha, ok := strings.Cut(strings.TrimSpace(line), " "); ok && name == "refs/heads/"+branch {
			return sha, true, nil
		}
	}
	return "", false, nil
}

// remoteBranch asks the remote itself for the exact branch. verified is false
// when it could not answer: absence is never inferred from tracking refs or from
// a failed call.
func (r *repository) remoteBranch(ctx context.Context, remote, branch string) (sha string, verified bool) {
	out, err := r.git(ctx, r.Root, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == "refs/heads/"+branch {
			return fields[0], true
		}
	}
	return "", true
}

func (r *repository) resolveBase(ctx context.Context, req domain.IsolationRequest) error {
	sha, err := r.git(ctx, r.Root, "rev-parse", "--verify", "--end-of-options", req.BaseRef+"^{commit}")
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return domain.ErrBaseChanged
	}
	if sha != req.BaseSHA {
		return domain.ErrBaseChanged
	}
	return nil
}

func placementError(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, config.ErrPlacementCollision):
		return domain.ErrPathCollision
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return domain.ErrOwnership
	}
}

// PreviewIsolation checks everything creation will check again, and creates
// nothing.
func (s *Service) PreviewIsolation(ctx context.Context, req domain.IsolationRequest) (domain.IsolationProposal, error) {
	r, err := s.repo(req.RepositoryID)
	if err != nil {
		return domain.IsolationProposal{}, err
	}
	if err := r.validateRequest(ctx, req); err != nil {
		return domain.IsolationProposal{}, err
	}
	if err := r.verifyRemote(ctx, req); err != nil {
		return domain.IsolationProposal{}, err
	}
	settings, err := r.isolationSettingsPath()
	if err != nil {
		return domain.IsolationProposal{}, domain.ErrOwnership
	}
	placement, perr := config.PreviewWorktreePlacement(ctx, settings, r.Root, req.Branch)
	if perr != nil && !errors.Is(perr, config.ErrPlacementCollision) {
		return domain.IsolationProposal{}, placementError(ctx, perr)
	}
	if err := r.resolveBase(ctx, req); err != nil {
		return domain.IsolationProposal{}, err
	}
	if err := r.requireNoBranch(ctx, req); err != nil {
		return domain.IsolationProposal{}, err
	}
	if perr != nil {
		return domain.IsolationProposal{}, placementError(ctx, perr)
	}
	return domain.IsolationProposal{IsolationRequest: req, Path: placement.Path, RootRevision: placement.Revision, ConfigHash: placement.ConfigHash}, nil
}

// requireNoBranch refuses a branch that already exists locally or on the remote.
func (r *repository) requireNoBranch(ctx context.Context, req domain.IsolationRequest) error {
	if _, exists, err := r.localBranchTip(ctx, req.Branch); err != nil {
		return err
	} else if exists {
		return domain.ErrBranchCollision
	}
	sha, verified := r.remoteBranch(ctx, req.RemoteName, req.Branch)
	switch {
	case !verified:
		return fixed(ctx, nil, domain.ErrOwnership)
	case sha != "":
		return domain.ErrBranchCollision
	}
	return nil
}

func sameProposal(a, b domain.IsolationProposal) bool { return a == b }

func (r *repository) loadResource(op string) (resource, bool, error) {
	if r.IsolationJournal == nil {
		return resource{}, false, domain.ErrUncertain
	}
	var rec resource
	var found bool
	err := r.IsolationJournal.View(func(d gitstore.Data) error {
		rec, found = gitstore.Get[resource](d, resourceBucket, op)
		return nil
	})
	if err != nil {
		return resource{}, false, domain.ErrUncertain
	}
	return rec, found, nil
}

func (r *repository) saveResource(op string, fn func(*resource)) (resource, error) {
	if r.IsolationJournal == nil {
		return resource{}, domain.ErrUncertain
	}
	var out resource
	err := r.IsolationJournal.Update(func(d gitstore.Data) error {
		rec, _ := gitstore.Get[resource](d, resourceBucket, op)
		fn(&rec)
		rec.UpdatedAt = time.Now().UTC()
		out = rec
		return gitstore.Put(d, resourceBucket, op, rec)
	})
	if err != nil {
		return resource{}, domain.ErrUncertain
	}
	return out, nil
}

// resolveLoose resolves symlinks through the path's existing ancestors, so a
// missing directory still compares with a registered one.
func resolveLoose(path string) string {
	path = filepath.Clean(path)
	ancestor := path
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return path
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(ancestor, path)
	if err != nil {
		return path
	}
	return filepath.Join(resolved, rel)
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b) || resolveLoose(a) == resolveLoose(b)
}

func (r *repository) registered(trees []domain.Worktree, path string) *domain.Worktree {
	for i := range trees {
		if samePath(trees[i].Path, path) {
			return &trees[i]
		}
	}
	return nil
}

// commonDir identifies the repository a worktree belongs to.
func (r *repository) commonDir(ctx context.Context, dir string) (string, error) {
	out, err := r.git(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil || out == "" {
		return "", fixed(ctx, err, domain.ErrOwnership)
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	resolved, err := filepath.EvalSymlinks(out)
	if err != nil {
		return "", domain.ErrOwnership
	}
	return filepath.Clean(resolved), nil
}

func (r *repository) isAncestor(ctx context.Context, base, tip string) (bool, error) {
	_, err := r.git(ctx, r.Root, "merge-base", "--is-ancestor", base, tip)
	if err == nil {
		return true, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return false, cerr
	}
	return false, nil
}

// proveWorktree is the ownership proof of a registered worktree: the journal
// bound this proposal before any effect, the registration names its exact path
// and branch, the path is a real directory that no link redirects, it shares
// the main repository's Git directory and its history contains the approved
// base. A name that merely looks right proves nothing.
func (r *repository) proveWorktree(ctx context.Context, rec resource, found bool, p domain.IsolationProposal, tree *domain.Worktree) (bool, error) {
	if !found || !sameProposal(rec.Proposal, p) || tree == nil || tree.Branch != p.Branch || tree.Main || !samePath(tree.Path, p.Path) {
		return false, nil
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, domain.ErrOwnership
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	if resolved, err := filepath.EvalSymlinks(p.Path); err != nil || filepath.Clean(resolved) != p.Path {
		return false, nil
	}
	mine, err := r.commonDir(ctx, p.Path)
	if err != nil {
		return false, err
	}
	main, err := r.commonDir(ctx, r.Root)
	if err != nil {
		return false, err
	}
	if mine != main {
		return false, nil
	}
	return r.isAncestor(ctx, p.BaseSHA, "refs/heads/"+p.Branch)
}

// gitStateNormal reports false for an unfinished rebase, merge, cherry-pick, revert or bisect.
func (r *repository) gitStateNormal(ctx context.Context, dir string) (bool, error) {
	gitDir, err := r.git(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil || gitDir == "" {
		return false, fixed(ctx, err, domain.ErrOwnership)
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		if _, err := os.Lstat(filepath.Join(gitDir, name)); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, domain.ErrOwnership
		}
	}
	return true, nil
}

// worktreeDigest summarizes a worktree's content state without keeping any
// file name or content: whether it is clean including ignored, untracked and
// nested repository content, and a digest of Git's own report.
func (r *repository) worktreeDigest(ctx context.Context, dir string) (clean bool, digest string, err error) {
	out, err := r.git(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return false, "", fixed(ctx, err, domain.ErrOwnership)
	}
	// Git can hide edits from status: assume-unchanged and skip-worktree entries
	// show with a tag other than H in ls-files -v. Their content is not
	// verified, so any such entry makes the worktree not clean.
	flags, err := r.git(ctx, dir, "ls-files", "-v", "-z")
	if err != nil {
		return false, "", fixed(ctx, err, domain.ErrOwnership)
	}
	hidden := false
	for _, entry := range strings.Split(flags, "\x00") {
		if entry != "" && entry[0] != 'H' {
			hidden = true
		}
	}
	sum := sha256.Sum256([]byte(out + "\x00" + flags))
	return out == "" && !hidden, hex.EncodeToString(sum[:]), nil
}

// InspectIsolation proves what exists for the proposal. Exists with Owned is a
// worktree this operation made; Owned alone is its branch left without a
// worktree by an interrupted creation; neither means nothing of it exists.
// A resource that exists but cannot be proven owned is reported not owned, and a
// registered worktree whose directory is gone is an ownership error, not absence.
func (s *Service) InspectIsolation(ctx context.Context, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	r, err := s.repo(p.RepositoryID)
	if err != nil {
		return domain.IsolationCheck{}, err
	}
	if err := r.validateProposal(ctx, p); err != nil {
		return domain.IsolationCheck{}, err
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return domain.IsolationCheck{}, err
	}
	defer unlock()
	return s.inspect(ctx, r, p)
}

func (s *Service) inspect(ctx context.Context, r *repository, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	return s.inspectWith(ctx, r, p, nil)
}

// inspectWith is inspect with a placement snapshot the caller already took, so a
// removal can bind its fingerprint to the configuration it will then execute.
func (s *Service) inspectWith(ctx context.Context, r *repository, p domain.IsolationProposal, snap *config.PlacementSnapshot) (domain.IsolationCheck, error) {
	rec, found, err := r.loadResource(p.OperationID)
	if err != nil {
		return domain.IsolationCheck{}, err
	}
	if found && !sameProposal(rec.Proposal, p) {
		return domain.IsolationCheck{}, domain.ErrInvalid
	}
	trees, err := s.ListWorktrees(ctx, r.ID)
	if err != nil {
		return domain.IsolationCheck{}, fixed(ctx, err, domain.ErrOwnership)
	}
	check := domain.IsolationCheck{Proposal: p, Hooks: hooksPending}
	if found {
		check.Hooks = r.hooksState(p.OperationID, rec.Hooks)
	}
	tree := r.registered(trees, p.Path)
	if tree == nil {
		// A registration on the proposal's branch elsewhere is not ours.
		tip, exists, err := r.localBranchTip(ctx, p.Branch)
		if err != nil {
			return domain.IsolationCheck{}, err
		}
		if exists && found {
			if ok, err := r.isAncestor(ctx, p.BaseSHA, tip); err != nil {
				return domain.IsolationCheck{}, err
			} else if ok {
				check.Owned, check.HeadSHA = true, tip
			}
		}
		check.Fingerprint = fingerprint(check, "", "", "")
		return check, nil
	}
	check.Exists = true
	check.Worktree = *tree
	check.Worktree.Main = false
	if tree.Missing {
		return check, domain.ErrOwnership
	}
	owned, err := r.proveWorktree(ctx, rec, found, p, tree)
	if err != nil {
		return check, err
	}
	check.Owned = owned
	if !owned {
		check.Fingerprint = fingerprint(check, "", "", "")
		return check, nil
	}
	head, err := r.git(ctx, p.Path, "rev-parse", "HEAD")
	if err != nil {
		return check, fixed(ctx, err, domain.ErrOwnership)
	}
	check.HeadSHA = head
	check.Worktree.HeadSHA = head
	clean, digest, err := r.worktreeDigest(ctx, p.Path)
	if err != nil {
		return check, err
	}
	normal, err := r.gitStateNormal(ctx, p.Path)
	if err != nil {
		return check, err
	}
	check.Clean = clean && normal
	common, err := r.commonDir(ctx, p.Path)
	if err != nil {
		return check, err
	}
	// The fingerprint also binds the project placement in force now (path, root,
	// revision, template, upstream and hooks), so a changed .bonsai.yaml or a
	// revoked or edited root invalidates a confirmed removal. A placement that
	// cannot be resolved refuses: it is never an accepted component.
	if snap == nil {
		settings, serr := r.isolationSettingsPath()
		if serr != nil {
			return check, domain.ErrOwnership
		}
		got, perr := config.SnapshotWorktreePlacement(ctx, settings, r.Root, p.Branch)
		if perr != nil {
			return check, placementError(ctx, perr)
		}
		snap = &got
	}
	if snap.Path != p.Path {
		return check, domain.ErrInputsChanged
	}
	current := fmt.Sprintf("%s|%s|%d", snap.ConfigHash, snap.Root, snap.Revision)
	check.Fingerprint = fingerprint(check, common, digest, fmt.Sprint(normal)+"|"+current)
	return check, nil
}

// hooksState maps a stored hook state to what a reader may assume now: a
// running state with no hook running here belongs to an earlier process and is
// unknown.
func (r *repository) hooksState(op, stored string) string {
	switch stored {
	case hooksRunning:
		r.hooksMu.Lock()
		active := r.hooksActive[op]
		r.hooksMu.Unlock()
		if active {
			return hooksRunning
		}
		return hooksUnknown
	case hooksDone, hooksFailed:
		return stored
	default:
		return hooksPending
	}
}

func fingerprint(c domain.IsolationCheck, common, digest, state string) string {
	p := c.Proposal
	h := sha256.New()
	for _, part := range []string{
		p.OperationID, p.RepositoryID, p.Branch, p.BaseRef, p.BaseSHA, p.RemoteName, p.Repository, p.Path,
		fmt.Sprint(p.RootRevision), p.ConfigHash, c.Worktree.Path, common, c.HeadSHA, digest, state,
		fmt.Sprint(c.Exists, c.Owned, c.Clean), c.Hooks,
	} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CreateIsolation creates the proposal's worktree on its new branch, or adopts
// what an interrupted attempt of the same operation left, then runs the create
// hooks. A failure after the worktree exists returns the populated check with
// the error: the worktree is retained, never removed to compensate.
func (s *Service) CreateIsolation(ctx context.Context, req domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	p := req.Proposal
	r, err := s.repo(p.RepositoryID)
	if err != nil {
		return domain.IsolationCheck{}, err
	}
	if err := r.validateProposal(ctx, p); err != nil {
		return domain.IsolationCheck{}, err
	}
	if r.IsolationJournal == nil {
		return domain.IsolationCheck{}, domain.ErrUncertain
	}
	if err := s.ensureWorktree(ctx, r, p); err != nil {
		return domain.IsolationCheck{}, err
	}
	hookErr := s.runCreateHooks(ctx, r, p, req.RetryHooks)
	check, err := func() (domain.IsolationCheck, error) {
		unlock, lerr := r.lock(context.WithoutCancel(ctx))
		if lerr != nil {
			return domain.IsolationCheck{}, lerr
		}
		defer unlock()
		return s.inspect(context.WithoutCancel(ctx), r, p)
	}()
	if err != nil {
		return check, err
	}
	return check, hookErr
}

// ensureWorktree makes the worktree exist under the repository gate and the
// root-configuration lock, so nothing else creates or removes one meanwhile.
func (s *Service) ensureWorktree(ctx context.Context, r *repository, p domain.IsolationProposal) error {
	unlock, err := r.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	defer r.inv.invalidate()
	rec, found, err := r.loadResource(p.OperationID)
	if err != nil {
		return err
	}
	if found && !sameProposal(rec.Proposal, p) {
		return domain.ErrInvalid
	}
	if found && (rec.Phase == phaseRemoving || rec.Phase == phaseRemoved) {
		return domain.ErrInputsChanged
	}
	if err := r.verifyRemote(ctx, p.IsolationRequest); err != nil {
		return err
	}
	trees, err := s.ListWorktrees(ctx, r.ID)
	if err != nil {
		return fixed(ctx, err, domain.ErrOwnership)
	}
	if tree := r.registered(trees, p.Path); tree != nil {
		// Something is registered at the reserved path: only this operation's own
		// worktree may be adopted.
		if !found {
			return domain.ErrPathCollision
		}
		if tree.Missing {
			return domain.ErrOwnership
		}
		owned, err := r.proveWorktree(ctx, rec, found, p, tree)
		if err != nil {
			return err
		}
		if !owned {
			return domain.ErrOwnership
		}
		return r.markCreated(p, found && rec.Phase != phaseIntent)
	}
	for _, t := range trees {
		if t.Branch == p.Branch {
			return domain.ErrBranchCollision
		}
	}
	tip, branchExists, err := r.localBranchTip(ctx, p.Branch)
	if err != nil {
		return err
	}
	attach := false
	if branchExists {
		if !found {
			return domain.ErrBranchCollision
		}
		// The branch is this operation's partial work only if it still holds the
		// approved base. It is attached as it is: never recreated or reset.
		ok, err := r.isAncestor(ctx, p.BaseSHA, tip)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrOwnership
		}
		attach = true
	}
	if err := r.resolveBase(ctx, p.IsolationRequest); err != nil {
		return err
	}
	if !attach {
		if _, exists, err := r.localBranchTip(ctx, p.Branch); err != nil {
			return err
		} else if exists {
			return domain.ErrBranchCollision
		}
		sha, verified := r.remoteBranch(ctx, p.RemoteName, p.Branch)
		switch {
		case !verified:
			return fixed(ctx, nil, domain.ErrOwnership)
		case sha != "":
			return domain.ErrBranchCollision
		}
	}
	settings, err := r.isolationSettingsPath()
	if err != nil {
		return domain.ErrOwnership
	}
	err = config.WithWorktreePlacement(ctx, settings, r.Root, p.Branch, func(pl config.WorktreePlacement) error {
		if pl.Path != p.Path || pl.ConfigHash != p.ConfigHash || pl.Revision != p.RootRevision {
			return domain.ErrInputsChanged
		}
		// Intent is durable before the effect, so a crash inside Git leaves
		// proof that this operation may own what appears.
		if !found {
			if _, err := r.saveResource(p.OperationID, func(rec *resource) {
				rec.Proposal, rec.Phase, rec.Hooks = p, phaseIntent, hooksPending
			}); err != nil {
				return err
			}
		}
		args := []string{"worktree", "add", "-b", p.Branch, "--", p.Path, p.BaseSHA}
		if attach {
			args = []string{"worktree", "add", "--", p.Path, p.Branch}
		}
		if _, err := r.git(ctx, r.Root, args...); err != nil {
			return fixed(ctx, err, domain.E("worktree_failed", "Git could not create the worktree"))
		}
		return nil
	})
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) || ctx.Err() != nil {
			return err
		}
		return placementError(ctx, err)
	}
	trees, err = s.ListWorktrees(ctx, r.ID)
	if err != nil {
		return fixed(ctx, err, domain.ErrOwnership)
	}
	tree := r.registered(trees, p.Path)
	if tree == nil || tree.Branch != p.Branch {
		return domain.ErrOwnership
	}
	return r.markCreated(p, false)
}

func (r *repository) markCreated(p domain.IsolationProposal, already bool) error {
	if already {
		return nil
	}
	_, err := r.saveResource(p.OperationID, func(rec *resource) {
		rec.Proposal, rec.Phase = p, phaseCreated
		if rec.Hooks == "" {
			rec.Hooks = hooksPending
		}
	})
	return err
}

// runCreateHooks runs the configured create hooks once. Done never reruns;
// failed or interrupted hooks run again only with the caller's acknowledgment
// that they may have partly executed.
func (s *Service) runCreateHooks(ctx context.Context, r *repository, p domain.IsolationProposal, retry bool) error {
	rec, found, err := r.loadResource(p.OperationID)
	if err != nil {
		return err
	}
	if !found || !sameProposal(rec.Proposal, p) {
		return domain.ErrInvalid
	}
	switch r.hooksState(p.OperationID, rec.Hooks) {
	case hooksDone:
		return nil
	case hooksRunning:
		return domain.ErrBusy
	case hooksFailed, hooksUnknown:
		if !retry {
			return domain.ErrHooksFailed
		}
	}
	settings, err := r.isolationSettingsPath()
	if err != nil {
		return domain.ErrOwnership
	}
	placement, err := config.InspectWorktreePlacement(ctx, settings, r.Root, p.Branch)
	if err != nil || placement.ConfigHash != p.ConfigHash || placement.Path != p.Path {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return domain.ErrInputsChanged
	}
	cfg, err := config.Load(r.Root)
	if err != nil {
		return domain.ErrInputsChanged
	}
	commands := cfg.CreateHooks()
	if len(commands) == 0 {
		_, err := r.saveResource(p.OperationID, func(rec *resource) { rec.Hooks = hooksDone })
		return err
	}
	if _, err := r.saveResource(p.OperationID, func(rec *resource) { rec.Hooks = hooksRunning }); err != nil {
		return err
	}
	r.hooksMu.Lock()
	r.hooksActive[p.OperationID] = true
	r.hooksMu.Unlock()
	hookErr := r.IsolationHooks(ctx, p.Path, commands, config.HookVars(r.Root, p.Path, p.Branch, p.BaseRef, 0))
	r.hooksMu.Lock()
	delete(r.hooksActive, p.OperationID)
	r.hooksMu.Unlock()
	state := hooksDone
	if hookErr != nil {
		state = hooksFailed
	}
	if _, err := r.saveResource(p.OperationID, func(rec *resource) { rec.Hooks = state }); err != nil {
		return err
	}
	if hookErr != nil {
		return domain.ErrHooksFailed
	}
	return nil
}

// RemoveIsolation removes a clean, untouched worktree this operation made,
// after the caller confirmed exactly what InspectIsolation reported. It never
// forces, resets or cleans, never deletes the branch, and leaves the journal
// record as audit.
func (s *Service) RemoveIsolation(ctx context.Context, req domain.IsolationRemoveRequest) error {
	check := req.Check
	p := check.Proposal
	r, err := s.repo(p.RepositoryID)
	if err != nil {
		return err
	}
	if err := r.validateProposal(ctx, p); err != nil {
		return err
	}
	if r.IsolationJournal == nil {
		return domain.ErrUncertain
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	defer r.inv.invalidate()
	settings, err := r.isolationSettingsPath()
	if err != nil {
		return domain.ErrOwnership
	}
	// Placement, hooks and removal are decided under the project-roots lock, from
	// one read of the configuration: the hooks that run are exactly the ones whose
	// hash the confirmed fingerprint bound.
	var removeErr error
	lockErr := config.WithProjectRootsContext(ctx, settings, func(config.ProjectRoots) error {
		removeErr = s.removeLocked(ctx, r, settings, req)
		return nil
	})
	if lockErr != nil {
		return fixed(ctx, lockErr, domain.ErrOwnership)
	}
	return removeErr
}

func (s *Service) removeLocked(ctx context.Context, r *repository, settings string, req domain.IsolationRemoveRequest) error {
	check := req.Check
	p := check.Proposal
	snap, err := config.SnapshotWorktreePlacement(ctx, settings, r.Root, p.Branch)
	if err != nil {
		return placementError(ctx, err)
	}
	current, err := s.inspectWith(ctx, r, p, &snap)
	if err != nil {
		return err
	}
	if !current.Exists || !current.Owned || samePath(p.Path, r.Root) {
		return domain.ErrOwnership
	}
	if current.Fingerprint != check.Fingerprint || current.HeadSHA != check.HeadSHA {
		return domain.ErrInputsChanged
	}
	if !current.Clean {
		return domain.ErrDirty
	}
	if current.HeadSHA != p.BaseSHA {
		return domain.ErrInputsChanged
	}
	if r.BeforeRemove != nil {
		if err := r.BeforeRemove(ctx, p.Path); err != nil {
			return err
		}
	}
	rec, _, err := r.loadResource(p.OperationID)
	if err != nil {
		return err
	}
	if rec.Phase != phaseRemoving {
		if _, err := r.saveResource(p.OperationID, func(rec *resource) { rec.Phase = phaseRemoving }); err != nil {
			return err
		}
	}
	if len(snap.DeleteHooks) > 0 {
		if err := r.IsolationHooks(ctx, p.Path, snap.DeleteHooks, config.HookVars(r.Root, p.Path, p.Branch, p.BaseRef, 0)); err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			return domain.ErrHooksFailed
		}
		// Hooks may have written files or moved HEAD.
		after, err := s.inspectWith(ctx, r, p, &snap)
		if err != nil {
			return err
		}
		if !after.Clean {
			return domain.ErrDirty
		}
		if after.HeadSHA != p.BaseSHA {
			return domain.ErrInputsChanged
		}
	}
	if _, err := r.git(ctx, r.Root, "worktree", "remove", "--", p.Path); err != nil {
		return fixed(ctx, err, domain.E("remove_failed", "Git refused to remove the worktree"))
	}
	_, err = r.saveResource(p.OperationID, func(rec *resource) { rec.Phase = phaseRemoved })
	return err
}

// ownedTree proves ownership for the read and push operations.
func (s *Service) ownedTree(ctx context.Context, r *repository, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	check, err := s.inspect(ctx, r, p)
	if err != nil {
		return check, err
	}
	if !check.Exists || !check.Owned {
		return check, domain.ErrOwnership
	}
	return check, nil
}

// InspectPublishedHead compares the worktree's committed tree with the approved
// base and asks the remote for the branch. Uncommitted content never makes a
// head meaningful, and a remote that cannot answer is reported unverified.
func (s *Service) InspectPublishedHead(ctx context.Context, p domain.IsolationProposal) (domain.PublishedHead, error) {
	r, err := s.repo(p.RepositoryID)
	if err != nil {
		return domain.PublishedHead{}, err
	}
	if err := r.validateProposal(ctx, p); err != nil {
		return domain.PublishedHead{}, err
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return domain.PublishedHead{}, err
	}
	defer unlock()
	return s.publishedHead(ctx, r, p)
}

func (s *Service) publishedHead(ctx context.Context, r *repository, p domain.IsolationProposal) (domain.PublishedHead, error) {
	check, err := s.ownedTree(ctx, r, p)
	if err != nil {
		return domain.PublishedHead{}, err
	}
	if err := r.verifyRemote(ctx, p.IsolationRequest); err != nil {
		return domain.PublishedHead{}, err
	}
	head := domain.PublishedHead{HeadSHA: check.HeadSHA}
	baseTree, err := r.git(ctx, p.Path, "rev-parse", p.BaseSHA+"^{tree}")
	if err != nil {
		return domain.PublishedHead{}, fixed(ctx, err, domain.ErrOwnership)
	}
	headTree, err := r.git(ctx, p.Path, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return domain.PublishedHead{}, fixed(ctx, err, domain.ErrOwnership)
	}
	head.Meaningful = baseTree != headTree
	dirty, err := r.git(ctx, p.Path, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return domain.PublishedHead{}, fixed(ctx, err, domain.ErrOwnership)
	}
	head.Dirty = dirty != ""
	sha, verified := r.remoteBranch(ctx, p.RemoteName, p.Branch)
	// Verified means the remote itself named the branch; a branch it does not
	// have, or a remote that did not answer, verifies nothing.
	head.RemoteSHA, head.RemoteVerified = sha, verified && sha != ""
	return head, nil
}

// PushIsolation pushes exactly the requested head to the proposal's own branch
// on the verified remote: no force, no other branch. The result is read back
// from the remote, so a push that errored but landed is still reported.
func (s *Service) PushIsolation(ctx context.Context, req domain.IsolationPushRequest) (domain.PublishedHead, error) {
	p := req.Proposal
	r, err := s.repo(p.RepositoryID)
	if err != nil {
		return domain.PublishedHead{}, err
	}
	if err := r.validateProposal(ctx, p); err != nil {
		return domain.PublishedHead{}, err
	}
	if !shaRE.MatchString(req.HeadSHA) {
		return domain.PublishedHead{}, domain.ErrInvalid
	}
	if r.IsolationJournal == nil {
		return domain.PublishedHead{}, domain.ErrUncertain
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return domain.PublishedHead{}, err
	}
	defer unlock()
	before, err := s.publishedHead(ctx, r, p)
	if err != nil {
		return before, err
	}
	if before.HeadSHA != req.HeadSHA {
		return before, domain.ErrInputsChanged
	}
	if !before.Meaningful {
		return before, domain.ErrInputsChanged
	}
	if before.Dirty {
		return before, domain.ErrDirty
	}
	if _, err := r.saveResource(p.OperationID, func(rec *resource) { rec.PushHead = req.HeadSHA }); err != nil {
		return before, err
	}
	_, pushErr := r.git(ctx, p.Path, "push", p.RemoteName, "HEAD:refs/heads/"+p.Branch)
	// The read-back is independent of the caller's cancellation but not unbounded:
	// it runs under the repository gate and the daemon's mutation lock, and a
	// stuck remote must end in an uncertain outcome the caller can query.
	vctx, vcancel := context.WithTimeout(context.WithoutCancel(ctx), isolationVerifyTimeout)
	defer vcancel()
	after, err := s.publishedHead(vctx, r, p)
	if err != nil && isContextErr(err) {
		err = domain.ErrUncertain
	}
	if err != nil {
		if errors.Is(err, domain.ErrUncertain) {
			return before, domain.ErrUncertain
		}
		if pushErr != nil {
			return before, fixed(ctx, pushErr, domain.E("push_failed", "Git could not push the branch"))
		}
		return before, err
	}
	if after.HeadSHA != req.HeadSHA {
		return after, domain.ErrInputsChanged
	}
	if pushErr != nil && !(after.RemoteVerified && after.RemoteSHA == req.HeadSHA) {
		return after, fixed(ctx, pushErr, domain.E("push_failed", "Git could not push the branch"))
	}
	if !after.RemoteVerified || after.RemoteSHA != req.HeadSHA {
		return after, domain.ErrUncertain
	}
	return after, nil
}
