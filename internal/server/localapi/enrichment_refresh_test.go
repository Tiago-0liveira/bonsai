package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

type refreshingGitHub struct {
	*syncTestGitHub
	headRepository string
	headSHA        string
	checksStarted  chan struct{}
	checksRelease  chan struct{}
	checksError    error
}

func (g *refreshingGitHub) PullRequests(ctx context.Context, repo string, filter githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	pulls, err := g.syncTestGitHub.PullRequests(ctx, repo, filter)
	pulls[0].HeadRepository = g.headRepository
	pulls[0].HeadSHA = g.headSHA
	return pulls, err
}

func (g *refreshingGitHub) Checks(ctx context.Context, repo, sha string) ([]githubdomain.Check, error) {
	if g.checksStarted != nil {
		close(g.checksStarted)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-g.checksRelease:
		}
	}
	if g.checksError != nil {
		return nil, g.checksError
	}
	return g.syncTestGitHub.Checks(ctx, repo, sha)
}

func TestProviderRefreshKeepsChecksOnlyForSameRepositoryAndSHA(t *testing.T) {
	for _, test := range []struct {
		name, repository, sha, want string
		fail                        bool
	}{
		{"same commit", "acme/repo", "remote-head", "passed", false},
		{"transient failure", "acme/repo", "remote-head", "passed", true},
		{"new commit", "acme/repo", "new-head", "unknown", false},
		{"different repository", "acme/fork", "remote-head", "unknown", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			projectID := "refresh-project"
			provider := &refreshingGitHub{syncTestGitHub: &syncTestGitHub{}, headRepository: "acme/repo", headSHA: "remote-head"}
			daemon := &syncTestDaemon{root: root}
			project := projectServices{
				info:   ProjectInfo{ID: projectID, Path: root, Available: true, FullName: "acme/repo"},
				daemon: daemon, github: provider,
			}
			registry := &syncTestRegistry{entries: map[string]projectServices{projectID: project}}
			syncer := newStateSync(registry, newEventHub())
			now := time.Unix(100, 0).UTC()
			syncer.now = func() time.Time { return now }
			syncer.ReconcileCatalog()
			result, err := daemon.result(gitbridge.Command{Type: "git.repository.refresh"})
			if err != nil {
				t.Fatal(err)
			}
			var local domain.RepositoryState
			if err := json.Unmarshal(result.Payload, &local); err != nil {
				t.Fatal(err)
			}
			syncer.commitProject(project, "seed", func(snapshot *browserSnapshot) { snapshot.Local = &local })
			syncer.refreshProvider(projectID, false)
			worktreeID := gitlocal.ID(localRepositoryID, root)
			initial, _ := syncer.CachedSnapshot(projectID)
			if initial.WorktreeState[worktreeID].CI.Status != "passed" {
				t.Fatal("initial checks did not pass")
			}

			now = now.Add(providerReadyTTL + time.Second)
			provider.headRepository, provider.headSHA = test.repository, test.sha
			if test.repository != "acme/repo" {
				syncer.commitProject(project, "remote changed", func(snapshot *browserSnapshot) {
					snapshot.Local.Remotes[0].FullName = test.repository
					snapshot.Repository.FullName = test.repository
				})
			}
			provider.checksStarted, provider.checksRelease = make(chan struct{}), make(chan struct{})
			if test.fail {
				provider.checksError = errors.New("temporary provider failure")
			}
			done := make(chan struct{})
			go func() { syncer.refreshProvider(projectID, true); close(done) }()
			defer func() {
				select {
				case <-provider.checksRelease:
				default:
					close(provider.checksRelease)
				}
				<-done
			}()
			select {
			case <-provider.checksStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("checks refresh did not start")
			}
			pending, _ := syncer.CachedSnapshot(projectID)
			ci := pending.WorktreeState[worktreeID].CI
			if ci.Status != test.want || ci.CheckedSHA != test.sha {
				t.Fatalf("pending checks = %+v, want %s for %s", ci, test.want, test.sha)
			}
			if test.want == "passed" && (len(ci.Checks) != 1 || ci.Freshness.State != "stale") {
				t.Fatalf("cached checks were lost: %+v", ci)
			}
			if test.want == "unknown" && (len(ci.Checks) != 0 || ci.Freshness.State != "loading") {
				t.Fatalf("checks reused across provider identities: %+v", ci)
			}
			close(provider.checksRelease)
			<-done
			final, _ := syncer.CachedSnapshot(projectID)
			ci = final.WorktreeState[worktreeID].CI
			if ci.Status != "passed" {
				t.Fatalf("final checks lost their result: %+v", ci)
			}
			if test.fail && (ci.Freshness.State != "stale" || ci.Freshness.Error == nil) {
				t.Fatalf("provider failure not recorded as stale: %+v", ci)
			}
		})
	}
}
