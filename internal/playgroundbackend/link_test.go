package playgroundbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/sessionlink"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

func linkRig(t *testing.T) *rig {
	r := newRig(t, "main")
	r.add("s", r.worktree("feature"))
	r.garden.view = workWith(r.b.local, gardensync.SourceLive, 7, 8)
	return r
}

func TestLinkSessionHappyPathIdempotentAndReplace(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)

	v, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	if err != nil || !v.Created || v.Link.IssueNumber != 7 || v.Link.IssueID != 700 || v.Link.Origin != "manual" || !v.Link.LinkedAt.Equal(r.tick) {
		t.Fatalf("link = %+v err=%v", v, err)
	}
	r.tick = r.tick.Add(3600e9)
	again, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	if err != nil || again.Created || !again.Link.LinkedAt.Equal(v.Link.LinkedAt) {
		t.Fatalf("same pair must be idempotent: %+v err=%v", again, err)
	}
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 8})
	wantCode(t, err, playgroundapi.CodeLinkConflict)
	rep, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 8, Replace: true})
	if err != nil || !rep.Created || rep.Link.IssueNumber != 8 {
		t.Fatalf("replace = %+v err=%v", rep, err)
	}

	list, err := r.b.Links(ctx)
	if err != nil || len(list.Links) != 1 || list.Links[0].IssueNumber != 8 {
		t.Fatalf("links = %+v err=%v", list, err)
	}
	// Changes shows the link as a manual task, never as a launch.
	ch, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || ch.Task.Source != playgroundapi.TaskManual || ch.Task.IssueNumber != 8 || ch.Task.AttemptID != "" {
		t.Fatalf("task = %+v err=%v", ch.Task, err)
	}
}

func TestLinkSessionOwnership(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)

	// Outside the repository.
	r.sessions.list = append(r.sessions.list, session("alien", t.TempDir(), "other-project"))
	_, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "alien", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeNotInRepository)

	// A Garden-launched session is never relinked.
	r.sessions.list[0].Task = &agents.TaskRef{IssueID: 700, AttemptID: "att"}
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7, Replace: true})
	wantCode(t, err, playgroundapi.CodeHasTask)
	r.sessions.list[0].Task = nil

	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 0})
	wantCode(t, err, playgroundapi.CodeInvalidInput)
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "ghost", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeSessionNotFound)

	// An Issue that is not in the verified collection.
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 99})
	wantCode(t, err, playgroundapi.CodeIssueNotFound)

	// Nothing was written by any refusal.
	if list, err := r.b.Links(ctx); err != nil || len(list.Links) != 0 {
		t.Fatalf("links after refusals = %+v err=%v", list, err)
	}
	if _, err := os.Stat(r.links.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store file created by a refusal: %v", err)
	}
}

func TestLinkSessionNeedsLiveGarden(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)

	r.garden.err = errors.New("github 500 secret")
	_, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeGardenUnavailable)
	r.garden.err = nil

	r.garden.view = workWith(r.b.local, gardensync.SourceStale, 7) // retained data is not verification
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeGardenUnavailable)

	r.garden.view = workWith("some-other-clone", gardensync.SourceLive, 7)
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeGardenUnavailable)

	r.garden.view = workWith(r.b.local, gardensync.SourceLive, 7)
	r.garden.view.Snapshot.Items[0].Deleted = true
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeIssueNotFound)

	r.b.deps.Garden = nil
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeGardenUnavailable)
}

func TestLinksAreScopedToTheClone(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)
	// Another clone linked the same session ID to another Issue in the shared file.
	if _, _, err := r.links.Put(ctx, sessionlink.Link{SessionID: "s", LocalRepoID: "another-clone", IssueNumber: 9, IssueID: 900, LinkedAt: r.tick}, false); err != nil {
		t.Fatal(err)
	}
	list, err := r.b.Links(ctx)
	if err != nil || len(list.Links) != 0 {
		t.Fatalf("foreign link shown: %+v err=%v", list, err)
	}
	ch, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || ch.Task.Source != playgroundapi.TaskNone {
		t.Fatalf("task = %+v err=%v", ch.Task, err)
	}
	// And it does not block this clone's own link.
	if _, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7}); err != nil {
		t.Fatal(err)
	}
}

func TestLinkStoreFailuresStayLocal(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)
	if err := os.WriteFile(r.links.Path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeStore)
	_, err = r.b.Links(ctx)
	wantCode(t, err, playgroundapi.CodeStore)
	if _, err = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"}); err != nil {
		t.Fatalf("PR read failed because of the link store: %v", err)
	}
	if _, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"}); err != nil {
		t.Fatalf("checks read failed because of the link store: %v", err)
	}

	// No store at all: the feature is off, reads stay empty, nothing panics.
	r.b.deps.Links = nil
	if v, err := r.b.Links(ctx); err != nil || len(v.Links) != 0 {
		t.Fatalf("links without store = %+v err=%v", v, err)
	}
	_, err = r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7})
	wantCode(t, err, playgroundapi.CodeStore)
	if _, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
}

func TestLinkNeverTouchesAttempts(t *testing.T) {
	ctx := context.Background()
	r := linkRig(t)
	if _, err := r.b.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 7}); err != nil {
		t.Fatal(err)
	}
	if r.attempts.calls != 0 || len(r.attempts.byID) != 0 {
		t.Fatal("linking read or created launch attempts")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.links.Path), "garden-launch.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a launch store appeared next to the link store")
	}
}
