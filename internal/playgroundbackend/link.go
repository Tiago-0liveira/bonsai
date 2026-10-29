package playgroundbackend

import (
	"context"
	"errors"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/sessionlink"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

const linkTimeout = 40 * time.Second

func linkOf(l sessionlink.Link) playgroundapi.Link {
	return playgroundapi.Link{SessionID: l.SessionID, IssueNumber: l.IssueNumber, IssueID: uint64(l.IssueID), LinkedAt: l.LinkedAt, Origin: l.Origin}
}

// LinkSession implements playgroundapi.Backend. It checks, in order: the session
// belongs to this repository, Garden did not start it (a launched session is
// never relinked), and the Issue is in the live Garden collection of this
// clone. Then it writes one local record. It creates no plan, attempt,
// approval or launch record and never touches the Issue.
func (b *Backend) LinkSession(ctx context.Context, req playgroundapi.LinkRequest) (playgroundapi.LinkView, error) {
	ctx, cancel := context.WithTimeout(ctx, linkTimeout)
	defer cancel()
	gen := b.genLinks.Add(1)
	if req.IssueNumber <= 0 {
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeInvalidInput, "An Issue number is required.")
	}
	sc, err := b.scope(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.LinkView{}, err
	}
	if sc.session.Task != nil {
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeHasTask, "This session was started for a Garden task and cannot be relinked.")
	}
	if b.deps.Garden == nil {
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeGardenUnavailable, "Garden is not available, so the Issue cannot be verified.")
	}
	if b.deps.Links == nil {
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeStore, "Task links cannot be stored.")
	}
	issue, err := b.findIssue(ctx, req.IssueNumber)
	if err != nil {
		return playgroundapi.LinkView{}, err
	}
	stored, created, err := b.deps.Links.Put(ctx, sessionlink.Link{
		SessionID: req.SessionID, LocalRepoID: b.local, IssueNumber: issue.Number, IssueID: issue.ID, LinkedAt: b.deps.Now(),
	}, req.Replace)
	switch {
	case errors.Is(err, sessionlink.ErrConflict):
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeLinkConflict, "This session is already linked to another Issue. Replace the link to change it.")
	case err != nil:
		if e := ctxErr(ctx); e != nil {
			return playgroundapi.LinkView{}, e
		}
		return playgroundapi.LinkView{}, fail(playgroundapi.CodeStore, "The link could not be saved.")
	}
	return playgroundapi.LinkView{Stamp: b.stamp(gen), Link: linkOf(stored), Created: created}, nil
}

// findIssue requires a live, complete-enough read of this clone's collection
// that contains the Issue. Retained (stale) data never authorizes a link.
func (b *Backend) findIssue(ctx context.Context, number int64) (garden.Issue, error) {
	work, err := b.deps.Garden.Work(ctx)
	if err != nil {
		if e := ctxErr(ctx); e != nil {
			return garden.Issue{}, e
		}
		return garden.Issue{}, fail(playgroundapi.CodeGardenUnavailable, "Garden could not be read, so the Issue cannot be verified.")
	}
	snap := work.Snapshot
	if snap.LocalRepoID != b.local || snap.Source != gardensync.SourceLive {
		return garden.Issue{}, fail(playgroundapi.CodeGardenUnavailable, "Garden has no fresh read of its Issues. Refresh Garden and try again.")
	}
	for _, it := range snap.Items {
		if !it.Deleted && it.Issue.Number == number && it.Issue.ID != 0 {
			return it.Issue, nil
		}
	}
	return garden.Issue{}, fail(playgroundapi.CodeIssueNotFound, "That Issue is not in this repository's Garden.")
}

// Links implements playgroundapi.Backend.
func (b *Backend) Links(ctx context.Context) (playgroundapi.LinksView, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	gen := b.genLinks.Add(1)
	if b.deps.Links == nil {
		return playgroundapi.LinksView{Stamp: b.stamp(gen)}, nil
	}
	list, err := b.deps.Links.List(ctx, b.local)
	if err != nil {
		if e := ctxErr(ctx); e != nil {
			return playgroundapi.LinksView{}, e
		}
		return playgroundapi.LinksView{}, fail(playgroundapi.CodeStore, "Task links could not be read.")
	}
	v := playgroundapi.LinksView{Stamp: b.stamp(gen)}
	for _, l := range list {
		v.Links = append(v.Links, linkOf(l))
	}
	return v, nil
}
