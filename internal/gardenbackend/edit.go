package gardenbackend

import (
	"context"
	"errors"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// editTimeout bounds one preview or save. A save that times out is reported as
// unknown and replay-safe: saving again checks the log before it appends.
const editTimeout = 60 * time.Second

func (b *Backend) editService() (*edit.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.edits != nil {
		return b.edits, nil
	}
	if b.deps.Issues == nil || b.deps.Writer == nil {
		return nil, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Editing specifications is unavailable in this build."}
	}
	svc, err := edit.New(edit.Deps{Reader: b.deps.Issues, Writer: b.deps.Writer})
	if err != nil {
		return nil, err
	}
	b.edits = svc
	return svc, nil
}

func baseView(b edit.Base) gardenapi.DocumentBase {
	return gardenapi.DocumentBase{HeadHash: b.HeadHash, HeadCommentID: b.HeadCommentID, BodyHash: b.BodyHash, Content: b.Content}
}

func baseOf(b gardenapi.DocumentBase) edit.Base {
	return edit.Base{HeadHash: b.HeadHash, HeadCommentID: b.HeadCommentID, BodyHash: b.BodyHash, Content: b.Content}
}

func remoteViews(rs []edit.Remote) []gardenapi.RemoteRevision {
	out := make([]gardenapi.RemoteRevision, len(rs))
	for i, r := range rs {
		out[i] = gardenapi.RemoteRevision{Hash: r.Hash, CommentID: r.CommentID, Author: r.Author, At: r.At, Content: r.Content, Source: r.Source}
	}
	return out
}

func conflictView(issue uint64, c *edit.Conflict) *gardenapi.ConflictView {
	if c == nil {
		return nil
	}
	return &gardenapi.ConflictView{IssueID: issue, Base: baseView(c.Base), Yours: c.Yours, Remote: remoteViews(c.Remote), BodyHash: c.BodyHash,
		Saved: c.Saved, SavedCommentID: c.SavedCommentID}
}

func conflictOf(c gardenapi.ConflictView) edit.Conflict {
	out := edit.Conflict{Base: baseOf(c.Base), Yours: c.Yours, BodyHash: c.BodyHash, Saved: c.Saved, SavedCommentID: c.SavedCommentID}
	for _, r := range c.Remote {
		out.Remote = append(out.Remote, edit.Remote{Hash: r.Hash, CommentID: r.CommentID, Author: r.Author, At: r.At, Content: r.Content, Source: r.Source})
	}
	return out
}

func (b *Backend) editTarget(ctx context.Context, issue uint64, write bool) (edit.Target, garden.RepoID, error) {
	t, id, err := b.target(ctx, write)
	if err != nil {
		return edit.Target{}, 0, err
	}
	if issue == 0 {
		return edit.Target{}, 0, apiError(edit.ErrWrongIssue)
	}
	return edit.Target{Repo: t.Repository.ID, Issue: garden.IssueID(issue), Viewer: id.ID}, t.Repository.ID, nil
}

// Document implements gardenapi.Backend.
func (b *Backend) Document(ctx context.Context, issue uint64) (gardenapi.DocumentView, error) {
	gen := b.gen.Add(1)
	svc, err := b.editService()
	if err != nil {
		return gardenapi.DocumentView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.editTarget(ctx, issue, false)
	if err != nil {
		return gardenapi.DocumentView{}, err
	}
	d, err := svc.Load(ctx, t)
	if err != nil {
		return gardenapi.DocumentView{}, apiError(err)
	}
	v := gardenapi.DocumentView{
		Stamp: b.stamp(gen, repo), IssueID: issue, Number: d.Issue.Number, Title: d.Issue.Title, Type: garden.Classify(d.Issue.Labels).Type,
		Body: d.Body, Revisions: remoteViews(d.Revisions), Base: baseView(d.Base), Seed: d.Seed,
	}
	if len(d.Heads) > 1 {
		v.Conflict = conflictView(issue, &edit.Conflict{Base: d.Base, Remote: d.Heads, BodyHash: d.Base.BodyHash})
	}
	return v, nil
}

func previewView(stamp gardenapi.Stamp, op string, issue uint64, p *edit.SavePreview) gardenapi.SavePreviewView {
	v := gardenapi.SavePreviewView{Stamp: stamp, OperationID: op, IssueID: issue, Base: baseView(p.Base), Content: p.Content, NewHash: p.NewHash,
		Parents: append([]string(nil), p.Parents...), BodyChanged: p.BodyChanged, AlreadySaved: p.AlreadySaved, SavedCommentID: p.SavedCommentID}
	for _, d := range p.Diff {
		v.Diff = append(v.Diff, gardenapi.SectionDiff{Section: d.Section, Added: d.Added, Removed: d.Removed})
	}
	return v
}

// PreviewSave implements gardenapi.Backend.
func (b *Backend) PreviewSave(ctx context.Context, req gardenapi.SaveRequest) (gardenapi.SavePreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.editService()
	if err != nil {
		return gardenapi.SavePreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.editTarget(ctx, req.IssueID, true)
	if err != nil {
		return gardenapi.SavePreviewView{}, err
	}
	out, err := svc.Preview(ctx, t, baseOf(req.Base), req.Content)
	if err != nil {
		return gardenapi.SavePreviewView{}, apiError(err)
	}
	stamp, op := b.stamp(gen, repo), "save-"+b.deps.NewID()
	if out.Conflict != nil {
		return gardenapi.SavePreviewView{Stamp: stamp, OperationID: op, IssueID: req.IssueID, Conflict: conflictView(req.IssueID, out.Conflict)}, nil
	}
	return previewView(stamp, op, req.IssueID, out.Save), nil
}

// PreviewResolve implements gardenapi.Backend.
func (b *Backend) PreviewResolve(ctx context.Context, req gardenapi.ResolveRequest) (gardenapi.SavePreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.editService()
	if err != nil {
		return gardenapi.SavePreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.editTarget(ctx, req.IssueID, true)
	if err != nil {
		return gardenapi.SavePreviewView{}, err
	}
	out, err := svc.PreviewResolve(ctx, t, conflictOf(req.Conflict), req.Result)
	if err != nil {
		return gardenapi.SavePreviewView{}, apiError(err)
	}
	stamp, op := b.stamp(gen, repo), "resolve-"+b.deps.NewID()
	if out.Conflict != nil {
		return gardenapi.SavePreviewView{Stamp: stamp, OperationID: op, IssueID: req.IssueID, Conflict: conflictView(req.IssueID, out.Conflict)}, nil
	}
	v := previewView(stamp, op, req.IssueID, out.Save)
	res := req.Conflict
	v.Resolution = &res
	return v, nil
}

// Save implements gardenapi.Backend.
func (b *Backend) Save(ctx context.Context, p gardenapi.SavePreviewView) (gardenapi.SaveView, error) {
	return b.write(ctx, p, false)
}

// Resolve implements gardenapi.Backend.
func (b *Backend) Resolve(ctx context.Context, p gardenapi.SavePreviewView) (gardenapi.SaveView, error) {
	return b.write(ctx, p, true)
}

func (b *Backend) write(ctx context.Context, p gardenapi.SavePreviewView, resolve bool) (gardenapi.SaveView, error) {
	gen := b.gen.Add(1)
	svc, err := b.editService()
	if err != nil {
		return gardenapi.SaveView{}, apiError(err)
	}
	if p.Stamp.LocalRepoID != b.local.String() || p.Conflict != nil || (resolve && p.Resolution == nil) {
		return gardenapi.SaveView{}, apiError(edit.ErrWrongIssue)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.editTarget(ctx, p.IssueID, true)
	if err != nil {
		return gardenapi.SaveView{}, err
	}
	if uint64(repo) != p.Stamp.GardenID {
		return gardenapi.SaveView{}, apiError(ErrRemoteChanged)
	}
	var res edit.Result
	var out edit.Outcome
	if resolve {
		res, out, err = svc.Resolve(ctx, t, conflictOf(*p.Resolution), p.Content)
	} else {
		res, out, err = svc.Commit(ctx, edit.SavePreview{Target: t, Base: baseOf(p.Base), Content: p.Content, NewHash: p.NewHash, Parents: p.Parents})
	}
	v := gardenapi.SaveView{Stamp: b.stamp(gen, repo), OperationID: p.OperationID, IssueID: p.IssueID, CommentID: res.CommentID, Hash: res.Hash, Replayed: res.Replayed}
	switch {
	case out.Conflict != nil:
		v.State, v.Conflict = gardenapi.SaveConflict, conflictView(p.IssueID, out.Conflict)
		v.CommentID = 0
		if out.Conflict.Saved {
			v.CommentID = res.CommentID
		}
		return v, nil
	case err == nil:
		v.State = gardenapi.SaveDone
		return v, nil
	case !errors.Is(err, edit.ErrAppendFailed) || definitiveWrite(err):
		// Refused or failed before anything was appended, or answered with a definite refusal.
		return gardenapi.SaveView{}, apiError(err)
	}
	// The append may have reached GitHub. Saving the same preview again finds it by hash, author and
	// parents instead of appending a second time.
	v.State = gardenapi.SaveUnknown
	v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "The revision may have been saved.",
		Next: "Save again: Bonsai checks GitHub first and never appends it twice."}
	return v, nil
}

// definitiveWrite reports a refusal that proves nothing was appended.
func definitiveWrite(err error) bool {
	return errors.Is(err, garden.ErrAuthRequired) || errors.Is(err, garden.ErrPermissionDenied) || errors.Is(err, garden.ErrRateLimited) ||
		errors.Is(err, garden.ErrForbidden) || errors.Is(err, garden.ErrCommentTooLarge) || errors.Is(err, connect.ErrGitHubValidation) || errors.Is(err, garden.ErrNotFound)
}
