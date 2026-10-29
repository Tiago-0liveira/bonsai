// Package fake is an in-memory playgroundapi.Backend for TUI tests. It is
// deterministic and never touches Git, GitHub or the disk: it returns the views
// a test scripted, stamped like the real backend stamps them (a new Generation
// per call of each kind).
package fake

import (
	"context"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// Scope is the Stamp.Scope of every fake reply.
const Scope = "project-v1-fake-clone"

// Backend replays scripted views per session. Fill the maps before use; a
// session without an entry gets the zero view (Changes: worktree ok, no files).
// Set an Err field to make that kind of call fail. Call New for a usable value.
type Backend struct {
	ChangesBySession map[string]playgroundapi.ChangesView
	ChangesErr       error
	// DiffBySession is keyed by session ID then FileDiffRequest.Path.
	DiffBySession map[string]map[string]playgroundapi.FileDiffView
	DiffErr       error
	PRBySession   map[string]playgroundapi.PRView
	PRErr         error
	// ChecksBySession is keyed by session ID.
	ChecksBySession map[string]playgroundapi.ChecksView
	ChecksErr       error
	LinkErr         error
	LinksList       []playgroundapi.Link
	LinksErr        error

	// Hook runs at the start of every call, outside any lock, with the method
	// name. A test blocks in it to make a reply late.
	Hook func(method string)
	Now  func() time.Time

	mu    sync.Mutex
	gens  map[string]uint64
	Calls []string
	// Requests records the arguments of the calls, newest last.
	LinkRequests []playgroundapi.LinkRequest
	DiffRequests []playgroundapi.FileDiffRequest
}

// New returns a Backend with empty scripts.
func New() *Backend {
	return &Backend{
		ChangesBySession: map[string]playgroundapi.ChangesView{},
		DiffBySession:    map[string]map[string]playgroundapi.FileDiffView{},
		PRBySession:      map[string]playgroundapi.PRView{},
		ChecksBySession:  map[string]playgroundapi.ChecksView{},
		gens:             map[string]uint64{},
		Now:              func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

var _ playgroundapi.Backend = (*Backend)(nil)

// begin records the call and returns its stamp.
func (b *Backend) begin(method string) playgroundapi.Stamp {
	if b.Hook != nil {
		b.Hook(method)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Calls = append(b.Calls, method)
	b.gens[method]++
	return playgroundapi.Stamp{Scope: Scope, Generation: b.gens[method], FetchedAt: b.Now()}
}

func (b *Backend) Changes(_ context.Context, req playgroundapi.ChangesRequest) (playgroundapi.ChangesView, error) {
	st := b.begin("Changes")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ChangesErr != nil {
		return playgroundapi.ChangesView{}, b.ChangesErr
	}
	v, ok := b.ChangesBySession[req.SessionID]
	if !ok {
		v = playgroundapi.ChangesView{State: playgroundapi.WorktreeOK, Base: playgroundapi.Base{Source: playgroundapi.BaseNone}, Task: playgroundapi.TaskLink{Source: playgroundapi.TaskNone}}
	}
	v.Stamp, v.SessionID = st, req.SessionID
	return v, nil
}

func (b *Backend) FileDiff(_ context.Context, req playgroundapi.FileDiffRequest) (playgroundapi.FileDiffView, error) {
	st := b.begin("FileDiff")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.DiffRequests = append(b.DiffRequests, req)
	if b.DiffErr != nil {
		return playgroundapi.FileDiffView{}, b.DiffErr
	}
	v, ok := b.DiffBySession[req.SessionID][req.Path]
	if !ok {
		return playgroundapi.FileDiffView{}, &playgroundapi.Error{Code: playgroundapi.CodeFileNotChanged, Message: "That file is not among the changed files."}
	}
	v.Stamp, v.SessionID, v.Path = st, req.SessionID, req.Path
	return v, nil
}

func (b *Backend) PullRequest(_ context.Context, req playgroundapi.PRRequest) (playgroundapi.PRView, error) {
	st := b.begin("PullRequest")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.PRErr != nil {
		return playgroundapi.PRView{}, b.PRErr
	}
	v, ok := b.PRBySession[req.SessionID]
	if !ok {
		v = playgroundapi.PRView{State: playgroundapi.PRNone}
	}
	v.Stamp, v.SessionID = st, req.SessionID
	return v, nil
}

func (b *Backend) Checks(_ context.Context, req playgroundapi.ChecksRequest) (playgroundapi.ChecksView, error) {
	st := b.begin("Checks")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ChecksErr != nil {
		return playgroundapi.ChecksView{}, b.ChecksErr
	}
	v, ok := b.ChecksBySession[req.SessionID]
	if !ok {
		v = playgroundapi.ChecksView{State: playgroundapi.ChecksNone, HeadSHA: req.HeadSHA}
	}
	v.Stamp, v.SessionID = st, req.SessionID
	return v, nil
}

// LinkSession records the link in LinksList unless LinkErr is set.
func (b *Backend) LinkSession(_ context.Context, req playgroundapi.LinkRequest) (playgroundapi.LinkView, error) {
	st := b.begin("LinkSession")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.LinkRequests = append(b.LinkRequests, req)
	if b.LinkErr != nil {
		return playgroundapi.LinkView{}, b.LinkErr
	}
	l := playgroundapi.Link{SessionID: req.SessionID, IssueNumber: req.IssueNumber, IssueID: uint64(req.IssueNumber) * 100, LinkedAt: b.Now(), Origin: "manual"}
	for i := range b.LinksList {
		if b.LinksList[i].SessionID == req.SessionID {
			if b.LinksList[i].IssueNumber == req.IssueNumber {
				return playgroundapi.LinkView{Stamp: st, Link: b.LinksList[i]}, nil
			}
			if !req.Replace {
				return playgroundapi.LinkView{}, &playgroundapi.Error{Code: playgroundapi.CodeLinkConflict, Message: "This session is already linked to another Issue. Replace the link to change it."}
			}
			b.LinksList[i] = l
			return playgroundapi.LinkView{Stamp: st, Link: l, Created: true}, nil
		}
	}
	b.LinksList = append(b.LinksList, l)
	return playgroundapi.LinkView{Stamp: st, Link: l, Created: true}, nil
}

func (b *Backend) Links(context.Context) (playgroundapi.LinksView, error) {
	st := b.begin("Links")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.LinksErr != nil {
		return playgroundapi.LinksView{}, b.LinksErr
	}
	return playgroundapi.LinksView{Stamp: st, Links: append([]playgroundapi.Link(nil), b.LinksList...)}, nil
}
