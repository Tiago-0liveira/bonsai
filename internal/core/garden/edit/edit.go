// Package edit saves specification edits as appended revisions. GitHub offers no
// conditional write for an Issue, so a read-before-write check alone cannot prevent
// a lost update. Every write therefore reads and verifies the whole comment log
// immediately before appending, appends a revision whose parents are exactly the
// heads that were seen, and verifies again afterwards: when another revision landed
// in between, both heads are shown and nothing is silently preferred. The Issue
// body, title and labels are never written.
package edit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

// Source labels a remote revision for the conflict view.
const Source = "Bonsai/GitHub comment"

// BodySource labels a remote edit of the Issue body, which is not part of the specification record.
const BodySource = "Issue body edited on GitHub (not part of the specification)"

// MaxSpecBytes keeps a rendered revision, with its envelope, under the adapter's comment limit.
const MaxSpecBytes = 56 << 10

// Appender is the only write the service needs.
type Appender interface {
	AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error)
}

// Deps are the seams of Service.
type Deps struct {
	Reader garden.IssueReader
	Writer Appender
}

// Service saves revisions. It is safe for concurrent use; writes are serialized so two saves of one
// preview in this process cannot both pass the replay check and append twice.
type Service struct {
	d  Deps
	mu sync.Mutex
}

// New builds a Service; both seams are required.
func New(d Deps) (*Service, error) {
	if d.Reader == nil || d.Writer == nil {
		return nil, errors.New("edit needs a reader and a writer")
	}
	return &Service{d: d}, nil
}

// Target names one Issue and the account that acts on it. Viewer is the numeric account ID the approval
// policy trusts.
type Target struct {
	Repo   garden.RepoID
	Issue  garden.IssueID
	Viewer string
}

// Base is what an edit started from: the single head (or none) and the body it saw.
type Base struct {
	HeadHash      string // empty when the Issue has no specification revision yet
	HeadCommentID int64
	BodyHash      string // ContentHash of the user-owned body at that time
	Content       string // the text the editor was seeded with
}

// Remote is one revision (or the body) another writer produced.
type Remote struct {
	Hash      string
	CommentID int64
	Author    string
	At        time.Time
	Content   string
	Source    string
}

// Document is the fresh, verified read of one Issue for editing.
type Document struct {
	Issue     garden.Issue
	Body      string   // user-owned body without the metadata and capture lines
	Revisions []Remote // every specification revision, oldest first
	Heads     []Remote // current specification heads; more than one is a conflict
	Base      Base     // what editing from now on is based on; meaningful when len(Heads) <= 1
	Seed      string   // canonical text for the editor, empty when Heads conflict
}

type state struct {
	issue garden.Issue
	revs  []garden.Revision
	heads []garden.Revision
}

func remoteOf(r garden.Revision) Remote {
	return Remote{Hash: r.Hash, CommentID: r.CommentID, Author: r.Author, At: r.CreatedAt, Content: r.Content, Source: Source}
}

func remotes(rs []garden.Revision) []Remote {
	out := make([]Remote, len(rs))
	for i, r := range rs {
		out[i] = remoteOf(r)
	}
	return out
}

// read fetches and verifies the Issue and its whole comment log. Anything short of a complete, problem-free
// verification is an error: heads read from a partial log could be wrong.
func (s *Service) read(ctx context.Context, t Target) (state, error) {
	issue, err := s.d.Reader.GetIssue(ctx, t.Repo, t.Issue)
	if err != nil {
		return state{}, err
	}
	if issue.ID != t.Issue || issue.Repo != t.Repo {
		return state{}, ErrWrongIssue
	}
	comments, err := s.d.Reader.ListComments(ctx, t.Repo, t.Issue)
	if err != nil {
		if errors.Is(err, garden.ErrTooManyComments) {
			return state{}, fmt.Errorf("%w: %w", ErrIncomplete, err)
		}
		return state{}, err
	}
	conv := make([]garden.Comment, 0, len(comments))
	for _, c := range comments {
		conv = append(conv, garden.Comment{ID: c.ID, Author: c.Author, AuthorID: c.AuthorID, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	opt := garden.VerifyOptions{Complete: true}
	if m, merr := garden.DecodeMetadata(issue.Metadata); merr == nil {
		opt.Revocations = m.Revocations
	}
	v := garden.Verify(garden.IssueIdentity{Repo: t.Repo, Issue: t.Issue}, conv, garden.ApprovalPolicy{AllowedActorIDs: []string{t.Viewer}}, opt)
	if !v.Complete {
		return state{}, ErrIncomplete
	}
	if len(v.Problems) > 0 {
		return state{}, ErrIntegrity
	}
	st := state{issue: issue}
	for _, r := range v.Revisions {
		if r.Kind == garden.RevisionSpec {
			st.revs = append(st.revs, r)
		}
	}
	for _, h := range v.Heads {
		if h.Kind == garden.RevisionSpec {
			st.heads = append(st.heads, h)
		}
	}
	byOrder := func(rs []garden.Revision) {
		sort.Slice(rs, func(i, j int) bool { return rs[i].CommentID < rs[j].CommentID })
	}
	byOrder(st.revs)
	byOrder(st.heads)
	return st, nil
}

func bodyHash(issue garden.Issue) string { return garden.ContentHash(garden.DisplayBody(issue.Body)) }

// bodySeed is the text a fresh editor starts from when no revision exists: the user's own body, structured.
func bodySeed(issue garden.Issue) string {
	return spec.Render(spec.Parse(garden.DisplayBody(issue.Body)))
}

// Load reads one Issue for editing.
func (s *Service) Load(ctx context.Context, t Target) (Document, error) {
	st, err := s.read(ctx, t)
	if err != nil {
		return Document{}, err
	}
	d := Document{Issue: st.issue, Body: garden.DisplayBody(st.issue.Body), Revisions: remotes(st.revs), Heads: remotes(st.heads)}
	d.Base = Base{BodyHash: bodyHash(st.issue)}
	switch len(st.heads) {
	case 0:
		d.Seed = bodySeed(st.issue)
	case 1:
		d.Base.HeadHash, d.Base.HeadCommentID = st.heads[0].Hash, st.heads[0].CommentID
		d.Seed = spec.Render(spec.Parse(st.heads[0].Content))
	}
	d.Base.Content = d.Seed
	return d, nil
}

// SavePreview is exactly what Commit or Resolve would append.
type SavePreview struct {
	Target      Target
	Base        Base
	Content     string // canonical rendered specification
	NewHash     string
	Parents     []string // hashes of the heads this revision supersedes
	Diff        []SectionDiff
	BodyChanged bool // the Issue body was edited on GitHub since the base (informational)
	// AlreadySaved is set when this exact revision, by this account and with these parents, is already in
	// the log: saving again returns it instead of appending.
	AlreadySaved   bool
	SavedCommentID int64
}

// Conflict is a divergence the user must resolve visibly.
type Conflict struct {
	Base   Base
	Yours  string
	Remote []Remote // the current heads, or the edited body when no revision exists
	// BodyHash is the body at the time of the read; with no heads it identifies what Remote holds.
	BodyHash string
	// Saved is set when Yours was appended but another revision landed at the same time;
	// Remote then includes our own head, identified by SavedCommentID.
	Saved          bool
	SavedCommentID int64
}

// Outcome is a Save preview or a Conflict, never both.
type Outcome struct {
	Save     *SavePreview
	Conflict *Conflict
}

func hashes(rs []garden.Revision) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Hash
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// shownHashes are the heads a conflict displayed, which a resolution supersedes.
func (c Conflict) shownHashes() []string {
	var out []string
	for _, r := range c.Remote {
		if r.Hash != "" {
			out = append(out, r.Hash)
		}
	}
	return out
}

// replayOf returns the comment of an existing revision written by this account with this hash and parents.
func (st state) replayOf(t Target, hash string, parents []string) int64 {
	for _, r := range st.revs {
		if r.Hash == hash && r.AuthorID == t.Viewer && sameSet(r.Parents, parents) {
			return r.CommentID
		}
	}
	return 0
}

func canonical(content string) (string, error) {
	text := spec.Render(spec.Parse(content))
	switch {
	case text == "":
		return "", ErrEmpty
	case len(text) > MaxSpecBytes:
		return "", ErrTooLarge
	}
	return text, nil
}

// collision decides what an equal hash means. The same revision (same author, same parents) is a replay and
// is returned as such; anything else would collide with an existing revision's hash and is refused.
func collision(st state, t Target, hash string, parents []string) (replay int64, err error) {
	for _, r := range st.revs {
		if r.Hash != hash {
			continue
		}
		if r.AuthorID == t.Viewer && sameSet(r.Parents, parents) {
			return r.CommentID, nil
		}
		if len(st.heads) == 1 && st.heads[0].CommentID == r.CommentID {
			return 0, ErrNoChange
		}
		return 0, MatchesEarlierError{CommentID: r.CommentID}
	}
	return 0, nil
}

func (st state) conflictFor(base Base, yours string) *Conflict {
	c := &Conflict{Base: base, Yours: yours, BodyHash: bodyHash(st.issue)}
	if len(st.heads) > 0 {
		c.Remote = remotes(st.heads)
		return c
	}
	c.Remote = []Remote{{Content: bodySeed(st.issue), Source: BodySource}}
	return c
}

// Preview verifies the log now and reports what saving content would append, or the conflict that stands in
// the way. It writes nothing.
func (s *Service) Preview(ctx context.Context, t Target, base Base, content string) (Outcome, error) {
	text, err := canonical(content)
	if err != nil {
		return Outcome{}, err
	}
	st, err := s.read(ctx, t)
	if err != nil {
		return Outcome{}, err
	}
	return s.plan(t, st, base, text)
}

func (s *Service) plan(t Target, st state, base Base, text string) (Outcome, error) {
	current := ""
	if len(st.heads) == 1 {
		current = st.heads[0].Hash
	}
	bodyChanged := bodyHash(st.issue) != base.BodyHash
	// A body edit matters only while the editor was seeded from that body: with a revision as the base, the
	// body is not part of the specification and is reported without blocking.
	if len(st.heads) > 1 || current != base.HeadHash || (len(st.heads) == 0 && bodyChanged) {
		return Outcome{Conflict: st.conflictFor(base, text)}, nil
	}
	var parents []string
	if current != "" {
		parents = []string{current}
	}
	hash := garden.ContentHash(text)
	if hash == garden.ContentHash(base.Content) {
		return Outcome{}, ErrNoChange // the editor text is what it started with
	}
	replay, err := collision(st, t, hash, parents)
	if err != nil {
		return Outcome{}, err
	}
	p := &SavePreview{Target: t, Base: base, Content: text, NewHash: hash, Parents: parents, BodyChanged: bodyChanged,
		Diff: Diff(spec.Parse(base.Content), spec.Parse(text)), AlreadySaved: replay != 0, SavedCommentID: replay}
	return Outcome{Save: p}, nil
}

// Result is what a write produced.
type Result struct {
	CommentID int64
	Hash      string
	// Replayed is set when the revision already existed and nothing was appended.
	Replayed bool
	// Conflict is set when another revision landed at the same time as ours: both heads exist.
	Conflict *Conflict
	// Unverified is set when the revision was appended but the log could not be read again.
	Unverified bool
}

// Commit appends the previewed revision after reading and verifying the log once more. A head that moved
// since the preview, or since the base, is returned as a Conflict and nothing is written.
func (s *Service) Commit(ctx context.Context, p SavePreview) (Result, Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.read(ctx, p.Target)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	// A revision already in the log with this hash, author and parents is this very save: heads may have moved
	// on since (even onto our own revision), which must not turn a replay into a conflict.
	if id := st.replayOf(p.Target, p.NewHash, p.Parents); id != 0 {
		return Result{CommentID: id, Hash: p.NewHash, Replayed: true}, Outcome{}, nil
	}
	out, err := s.plan(p.Target, st, p.Base, p.Content)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	if out.Conflict != nil {
		return Result{}, out, nil
	}
	if out.Save.NewHash != p.NewHash || !sameSet(out.Save.Parents, p.Parents) {
		return Result{}, Outcome{Conflict: st.conflictFor(p.Base, p.Content)}, nil
	}
	return s.write(ctx, p.Target, out.Save, p.Base, p.Content)
}

// Resolve publishes the user's resolution of a conflict as a revision superseding every head the conflict
// showed. When the remote side moved again it returns the new Conflict instead.
func (s *Service) Resolve(ctx context.Context, t Target, c Conflict, result string) (Result, Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, err := canonical(result)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	st, err := s.read(ctx, t)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	if id := st.replayOf(t, garden.ContentHash(text), c.shownHashes()); id != 0 {
		return Result{CommentID: id, Hash: garden.ContentHash(text), Replayed: true}, Outcome{}, nil
	}
	pv, nc, err := s.resolvePlan(t, st, c, text)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	if nc != nil {
		return Result{}, Outcome{Conflict: nc}, nil
	}
	return s.write(ctx, t, pv, c.Base, c.Yours)
}

// PreviewResolve reports what Resolve would append. It writes nothing.
func (s *Service) PreviewResolve(ctx context.Context, t Target, c Conflict, result string) (Outcome, error) {
	text, err := canonical(result)
	if err != nil {
		return Outcome{}, err
	}
	st, err := s.read(ctx, t)
	if err != nil {
		return Outcome{}, err
	}
	pv, nc, err := s.resolvePlan(t, st, c, text)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Save: pv, Conflict: nc}, nil
}

func (s *Service) resolvePlan(t Target, st state, c Conflict, text string) (*SavePreview, *Conflict, error) {
	moved := !sameSet(c.shownHashes(), hashes(st.heads))
	if len(st.heads) == 0 && bodyHash(st.issue) != c.BodyHash {
		moved = true
	}
	if moved {
		return nil, st.conflictFor(c.Base, c.Yours), nil
	}
	parents := hashes(st.heads)
	hash := garden.ContentHash(text)
	replay, err := collision(st, t, hash, parents)
	if err != nil {
		return nil, nil, err
	}
	base := Base{BodyHash: bodyHash(st.issue)}
	if len(st.heads) > 0 {
		base.Content = spec.Render(spec.Parse(st.heads[len(st.heads)-1].Content))
	} else {
		base.Content = bodySeed(st.issue)
	}
	return &SavePreview{Target: t, Base: base, Content: text, NewHash: hash, Parents: parents,
		Diff: Diff(spec.Parse(base.Content), spec.Parse(text)), AlreadySaved: replay != 0, SavedCommentID: replay}, nil, nil
}

// write appends pv, or returns the existing revision for a replay, then verifies the log again.
func (s *Service) write(ctx context.Context, t Target, pv *SavePreview, base Base, yours string) (Result, Outcome, error) {
	if pv.AlreadySaved {
		return Result{CommentID: pv.SavedCommentID, Hash: pv.NewHash, Replayed: true}, Outcome{}, nil
	}
	body, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: t.Repo, Issue: t.Issue}, pv.Content, pv.Parents)
	if err != nil {
		return Result{}, Outcome{}, err
	}
	comment, err := s.d.Writer.AppendComment(ctx, t.Repo, t.Issue, body)
	if err != nil {
		return Result{}, Outcome{}, fmt.Errorf("%w: %w", ErrAppendFailed, err)
	}
	res := Result{CommentID: comment.ID, Hash: pv.NewHash}
	after, err := s.read(context.WithoutCancel(ctx), t)
	if err != nil {
		res.Unverified = true
		return res, Outcome{}, nil
	}
	found := false
	for _, r := range after.revs {
		if r.CommentID == comment.ID && r.Hash == pv.NewHash {
			found = true
		}
	}
	if !found {
		return res, Outcome{}, ErrUnverified
	}
	if len(after.heads) > 1 {
		c := after.conflictFor(base, yours)
		// Remote keeps every head, ours included, so a later Resolve supersedes all of them.
		c.Saved, c.SavedCommentID = true, comment.ID
		res.Conflict = c
		return res, Outcome{Conflict: c}, nil
	}
	return res, Outcome{}, nil
}
