package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type fakeLaunchIsolationRig struct {
	service      *Service
	root         string
	request      domain.IsolationRequest
	mu           sync.Mutex
	calls        [][]string
	remoteSHA    string
	hookCalls    int
	hookErr      error
	afterAddErr  error
	branchOnly   bool
	deleteChange bool
}

func newFakeLaunchIsolationRig(t *testing.T) *fakeLaunchIsolationRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	_, root, _ := setup(t)
	git(t, root, "remote", "add", "origin", "https://github.com/owner/repo.git")
	path, err := config.ProjectRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.UpdateProjectRoots(path, "launch-root", 0, root, ""); err != nil {
		t.Fatal(err)
	}
	journal, err := gitstore.Open(filepath.Join(t.TempDir(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeLaunchIsolationRig{root: root}
	r.request = domain.IsolationRequest{OperationID: "impl-isolation", RepositoryID: "local", Branch: "bonsai/9-test", BaseRef: "main", BaseSHA: strings.TrimSpace(git(t, root, "rev-parse", "HEAD")), RemoteName: "origin", Repository: "owner/repo"}
	r.service, err = New([]Config{{ID: "local", Root: root, WorktreeRoot: filepath.Join(root, "retained"), IsolationJournal: journal, IsolationRun: r.run, IsolationHooks: func(_ context.Context, dir string, commands []string, vars map[string]string) error {
		r.hookCalls++
		if r.deleteChange {
			if err := os.WriteFile(filepath.Join(dir, "user-during-delete"), []byte("retain"), 0600); err != nil {
				return err
			}
		}
		return r.hookErr
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (r *fakeLaunchIsolationRig) run(ctx context.Context, dir string, args ...string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{}, args...))
	remote, after, branchOnly := r.remoteSHA, r.afterAddErr, r.branchOnly
	r.mu.Unlock()
	if slices.Contains(args, "ls-remote") {
		if remote == "" {
			return "", nil
		}
		return remote + "\trefs/heads/" + r.request.Branch + "\n", nil
	}
	if len(args) > 0 && args[0] == "push" {
		sha, err := coregit.RunContext(ctx, dir, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		r.mu.Lock()
		r.remoteSHA = strings.TrimSpace(sha)
		r.mu.Unlock()
		return "", nil
	}
	if len(args) > 1 && args[0] == "worktree" && args[1] == "add" {
		if branchOnly {
			i := slices.Index(args, "-b")
			if i < 0 {
				return "", domain.ErrInvalid
			}
			if _, err := coregit.RunContext(ctx, dir, "branch", args[i+1], r.request.BaseSHA); err != nil {
				return "", err
			}
			return "", domain.ErrUncertain
		}
		out, err := coregit.RunContext(ctx, dir, args...)
		if err != nil {
			return out, err
		}
		return out, after
	}
	if len(args) > 0 && (args[0] == "fetch" || args[0] == "pull") {
		return "", errors.New("unexpected remote command")
	}
	return coregit.RunContext(ctx, dir, args...)
}
func (r *fakeLaunchIsolationRig) preview(t *testing.T) domain.IsolationProposal {
	t.Helper()
	p, err := r.service.PreviewIsolation(context.Background(), r.request)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (r *fakeLaunchIsolationRig) create(t *testing.T) domain.IsolationCheck {
	t.Helper()
	c, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: r.preview(t)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestIsolationPreviewReadOnlyAndExactApprovedBaseDespiteDirtySource(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	if err := os.WriteFile(filepath.Join(r.root, "file.txt"), []byte("source uncommitted"), 0600); err != nil {
		t.Fatal(err)
	}
	p := r.preview(t)
	if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
		t.Fatalf("preview created directory: %v", err)
	}
	c, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p})
	if err != nil || !c.Owned || !c.Exists || c.HeadSHA != r.request.BaseSHA || c.Hooks != "done" {
		t.Fatalf("create=%+v %v", c, err)
	}
	data, err := os.ReadFile(filepath.Join(p.Path, "file.txt"))
	if err != nil || string(data) != "base\n" {
		t.Fatalf("source dirty content copied: %q %v", data, err)
	}
	if actual := strings.TrimSpace(git(t, p.Path, "rev-parse", "--git-common-dir")); actual == "" {
		t.Fatal("missing common-dir identity")
	}
}
func TestIsolationMovedBaseAndBranchPathCollisionsRefuse(t *testing.T) {
	for _, mode := range []string{"base moved", "branch exists", "path exists", "dangling path", "shell branch"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchIsolationRig(t)
			p := r.preview(t)
			want := "base_changed"
			switch mode {
			case "base moved":
				git(t, r.root, "commit", "--allow-empty", "-m", "moved")
			case "branch exists":
				git(t, r.root, "branch", r.request.Branch)
				want = "branch_collision"
			case "path exists":
				if err := os.MkdirAll(p.Path, 0700); err != nil {
					t.Fatal(err)
				}
				want = "path_collision"
			case "dangling path":
				if err := os.MkdirAll(filepath.Dir(p.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), p.Path); err != nil {
					t.Skip(err)
				}
				want = "path_collision"
			case "shell branch":
				p.Branch = "bad;echo injected"
				want = "invalid"
			}
			if c, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p}); domain.Code(err) != want {
				t.Fatalf("create=%+v err=%v want=%s", c, err, want)
			}
		})
	}
}
func TestIsolationInterruptedAddAndBranchOnlyIntentRecoverWithoutReset(t *testing.T) {
	for _, branchOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "add completed", true: "branch only"}[branchOnly], func(t *testing.T) {
			r := newFakeLaunchIsolationRig(t)
			p := r.preview(t)
			r.afterAddErr = domain.ErrUncertain
			r.branchOnly = branchOnly
			_, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p})
			if !errors.Is(err, domain.ErrUncertain) {
				t.Fatalf("partial=%v", err)
			}
			r.afterAddErr = nil
			r.branchOnly = false
			c, err := r.service.InspectIsolation(context.Background(), p)
			if err != nil && !errors.Is(err, domain.ErrUncertain) {
				t.Fatalf("inspect=%+v %v", c, err)
			}
			c, err = r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p, RetryHooks: true})
			if err != nil || !c.Owned || c.HeadSHA != p.BaseSHA {
				t.Fatalf("resume=%+v %v", c, err)
			}
			if got := strings.TrimSpace(git(t, r.root, "rev-parse", p.Branch)); got != p.BaseSHA {
				t.Fatalf("branch reset/changed: %s", got)
			}
		})
	}
}
func TestIsolationHooksRetainResourcesAndRequireAcknowledgedRetry(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	if err := os.WriteFile(filepath.Join(r.root, ".bonsai.yaml"), []byte("hooks:\n  on_worktree_create:\n    - configured-create\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := r.preview(t)
	r.hookErr = errors.New("fake hook refused")
	c, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p})
	if domain.Code(err) != "hooks_failed" || !c.Exists || c.Hooks != "failed" {
		t.Fatalf("hook failure=%+v %v", c, err)
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatal("failed hook erased worktree", err)
	}
	count := r.hookCalls
	r.hookErr = nil
	if _, err = r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p}); err == nil || r.hookCalls != count {
		t.Fatalf("unaccepted hook retry=%v calls=%d", err, r.hookCalls)
	}
	c, err = r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p, RetryHooks: true})
	if err != nil || c.Hooks != "done" || r.hookCalls != count+1 {
		t.Fatalf("hook retry=%+v %v", c, err)
	}
	if _, err = r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p, RetryHooks: true}); err != nil || r.hookCalls != count+1 {
		t.Fatalf("done hook rerun=%v", err)
	}
}
func TestIsolationCleanupNeverErasesUserContentOrBranchHistory(t *testing.T) {
	for _, mode := range []string{"clean", "dirty", "untracked", "ignored", "nested repository", "extra commit", "changed fingerprint", "delete hook changes"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchIsolationRig(t)
			if mode == "delete hook changes" {
				if err := os.WriteFile(filepath.Join(r.root, ".bonsai.yaml"), []byte("hooks:\n  on_worktree_delete:\n    - configured-delete\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := r.create(t)
			switch mode {
			case "dirty":
				os.WriteFile(filepath.Join(c.Proposal.Path, "file.txt"), []byte("edited"), 0600)
			case "untracked":
				os.WriteFile(filepath.Join(c.Proposal.Path, "keep"), []byte("user"), 0600)
			case "ignored":
				git(t, c.Proposal.Path, "config", "core.excludesFile", filepath.Join(t.TempDir(), "ignore"))
				os.WriteFile(filepath.Join(c.Proposal.Path, "ignored"), []byte("user"), 0600)
				os.WriteFile(filepath.Join(r.root, ".git", "info", "exclude"), []byte("ignored\n"), 0600)
			case "nested repository":
				os.Mkdir(filepath.Join(c.Proposal.Path, "nested"), 0700)
				git(t, filepath.Join(c.Proposal.Path, "nested"), "init")
			case "extra commit":
				git(t, c.Proposal.Path, "commit", "--allow-empty", "-m", "retain history")
			case "changed fingerprint":
				c.Fingerprint = "changed"
			case "delete hook changes":
				r.deleteChange = true
			}
			err := r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: c})
			if mode == "clean" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(c.Proposal.Path); !os.IsNotExist(err) {
					t.Fatalf("directory retained: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe removal accepted")
				}
				if _, err := os.Stat(c.Proposal.Path); err != nil {
					t.Fatal("user content erased", err)
				}
			}
			if got := strings.TrimSpace(git(t, r.root, "rev-parse", "--verify", c.Proposal.Branch)); got == "" {
				t.Fatal("cleanup erased branch")
			}
			for _, args := range r.calls {
				for _, arg := range args {
					if arg == "--force" || arg == "-D" || arg == "reset" || arg == "clean" {
						t.Fatalf("destructive compensation: %v", args)
					}
				}
			}
		})
	}
}
func TestIsolationPublishedHeadUsesTreeDifferenceAndFreshRemoteProof(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	git(t, c.Proposal.Path, "commit", "--allow-empty", "-m", "empty")
	head, err := r.service.InspectPublishedHead(context.Background(), c.Proposal)
	if err != nil || head.Meaningful {
		t.Fatalf("empty commit meaningful: %+v %v", head, err)
	}
	os.WriteFile(filepath.Join(c.Proposal.Path, "change"), []byte("meaningful"), 0600)
	git(t, c.Proposal.Path, "add", "change")
	git(t, c.Proposal.Path, "commit", "-m", "change")
	head, err = r.service.InspectPublishedHead(context.Background(), c.Proposal)
	if err != nil || !head.Meaningful || head.RemoteVerified {
		t.Fatalf("unpublished head=%+v %v", head, err)
	}
	pushed, err := r.service.PushIsolation(context.Background(), domain.IsolationPushRequest{Proposal: c.Proposal, HeadSHA: head.HeadSHA})
	if err != nil || !pushed.RemoteVerified || pushed.RemoteSHA != head.HeadSHA {
		t.Fatalf("pushed=%+v %v", pushed, err)
	}
	seen := false
	for _, args := range r.calls {
		if len(args) > 0 && args[0] == "push" {
			seen = slices.Contains(args, "origin") && slices.Contains(args, "HEAD:refs/heads/"+c.Proposal.Branch)
		}
	}
	if !seen {
		t.Fatal("push did not use exact explicit remote/refspec")
	}
}

func TestIsolationPlacementChangesAfterPreviewRefuseCreation(t *testing.T) {
	for _, mode := range []string{"hooks config", "root revision"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchIsolationRig(t)
			p := r.preview(t)
			if mode == "hooks config" {
				if err := os.WriteFile(filepath.Join(r.root, ".bonsai.yaml"), []byte("hooks:\n  on_worktree_create:\n    - changed-hook\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				settings, err := config.ProjectRootsPath()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = config.UpdateProjectRoots(settings, "second-root", p.RootRevision, t.TempDir(), ""); err != nil {
					t.Fatal(err)
				}
			}
			if c, err := r.service.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p}); domain.Code(err) != "inputs_changed" {
				t.Fatalf("changed placement=%+v %v", c, err)
			}
			if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
				t.Fatal("changed placement created directory", err)
			}
		})
	}
}

func TestIsolationMissingJournalAndForeignSameNameCannotAuthorizeEffects(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	p := r.preview(t)
	without, err := New([]Config{{ID: "local", Root: r.root, WorktreeRoot: filepath.Join(r.root, "retained"), IsolationRun: r.run}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = without.CreateIsolation(context.Background(), domain.IsolationCreateRequest{Proposal: p}); !errors.Is(err, domain.ErrUncertain) {
		t.Fatalf("effect without durable intent=%v", err)
	}
	git(t, r.root, "worktree", "add", "-b", p.Branch, p.Path, p.BaseSHA)
	c, err := r.service.InspectIsolation(context.Background(), p)
	if err == nil && c.Owned {
		t.Fatal("foreign same-name worktree adopted without intent")
	}
	if err = r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: c}); err == nil {
		t.Fatal("foreign same-name worktree removed")
	}
	if _, err := os.Stat(p.Path); err != nil {
		t.Fatal("foreign worktree erased", err)
	}
}

func TestIsolationRemoteProofAndActiveProcessGateRemoval(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	r.remoteSHA = r.request.BaseSHA
	if _, err := r.service.PreviewIsolation(context.Background(), r.request); domain.Code(err) != "branch_collision" {
		t.Fatalf("remote branch treated as absent: %v", err)
	}
	r.remoteSHA = ""
	c := r.create(t)
	repository, err := r.service.repo("local")
	if err != nil {
		t.Fatal(err)
	}
	repository.BeforeRemove = func(context.Context, string) error { return domain.ErrBusy }
	if err := r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: c}); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("active process gate=%v", err)
	}
	if _, err := os.Stat(c.Proposal.Path); err != nil {
		t.Fatal("active worktree removed", err)
	}
}
