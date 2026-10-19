// Package fake provides a deterministic, in-memory Garden adapter for contract tests. Its synchronized setup methods keep mutation out of adapter state.
package fake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Fake stores isolated issue and project snapshots for contract tests.
type Fake struct {
	mu        sync.Mutex
	issues    map[garden.RepoID]map[garden.IssueID]garden.Issue
	comments  map[garden.RepoID]map[garden.IssueID][]garden.IssueComment
	projects  map[garden.RepoID][]garden.Project
	pageSize  int
	fail      error
	deny      bool
	next      int64
	now       func() time.Time
	clockStep int64
}

// New constructs a fake with an empty store and a 100 item page size.
func New() *Fake {
	return &Fake{issues: map[garden.RepoID]map[garden.IssueID]garden.Issue{}, comments: map[garden.RepoID]map[garden.IssueID][]garden.IssueComment{}, projects: map[garden.RepoID][]garden.Project{}, pageSize: 100, next: 0, now: func() time.Time { return time.Unix(0, 0).UTC() }}
}

// SetClock injects the fake server clock; each appended comment advances it by one nanosecond.
func (f *Fake) SetClock(now func() time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if now == nil {
		f.now = func() time.Time { return time.Unix(0, 0).UTC() }
	} else {
		f.now = now
	}
	f.clockStep = 0
}

// SeedIssue inserts an issue for deterministic contract tests.
func (f *Fake) SeedIssue(i garden.Issue) { f.PutIssue(i) }

// ReplaceIssues replaces one repository snapshot to model concurrent collection changes.
func (f *Fake) ReplaceIssues(repo garden.RepoID, items []garden.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues[repo] = make(map[garden.IssueID]garden.Issue)
	for _, i := range items {
		f.issues[repo][i.ID] = cloneIssue(i)
	}
}

// SetPageSize configures pagination; nonpositive values use the documented maximum of 100.
func (f *Fake) SetPageSize(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n <= 0 {
		n = 100
	}
	f.pageSize = n
}

// FailNext injects one adapter failure.
func (f *Fake) FailNext(err error) { f.mu.Lock(); defer f.mu.Unlock(); f.fail = err }

// SetDenied configures permission denial for contract tests.
func (f *Fake) SetDenied(v bool) { f.mu.Lock(); defer f.mu.Unlock(); f.deny = v }
func (f *Fake) check() error {
	if f.deny {
		return garden.ErrPermissionDenied
	}
	err := f.fail
	f.fail = nil
	return err
}
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("garden fake context: %w", err)
	}
	return nil
}

// PutIssue seeds or replaces one issue for test setup.
func (f *Fake) PutIssue(i garden.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issues[i.Repo] == nil {
		f.issues[i.Repo] = map[garden.IssueID]garden.Issue{}
	}
	f.issues[i.Repo][i.ID] = cloneIssue(i)
}

// ListManagedIssues returns one page of managed issue snapshots.
func (f *Fake) ListManagedIssues(ctx context.Context, r garden.RepoID, cursor string) (garden.IssuePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return garden.IssuePage{Complete: false}, err
	}
	if err := f.check(); err != nil {
		return garden.IssuePage{}, err
	}
	ids := make([]garden.IssueID, 0)
	for id, i := range f.issues[r] {
		for _, l := range i.Labels {
			if l == garden.ManagedLabel {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	start := 0
	if cursor != "" {
		if strings.TrimSpace(cursor) != cursor || cursor == "" {
			return garden.IssuePage{}, fmt.Errorf("%w: %q", garden.ErrInvalidCursor, cursor)
		}
		parsed, e := strconv.Atoi(cursor)
		if e != nil || parsed < 0 {
			return garden.IssuePage{}, fmt.Errorf("%w: %q", garden.ErrInvalidCursor, cursor)
		}
		start = parsed
	}
	if start > len(ids) {
		return garden.IssuePage{}, fmt.Errorf("%w: %d exceeds collection size %d", garden.ErrInvalidCursor, start, len(ids))
	}
	end := len(ids)
	if f.pageSize > 0 && end > start+f.pageSize {
		end = start + f.pageSize
	}
	out := garden.IssuePage{Complete: end == len(ids)}
	for _, id := range ids[start:end] {
		if err := checkContext(ctx); err != nil {
			out.Complete = false
			out.NextCursor = fmt.Sprint(len(out.Items) + start)
			return out, err
		}
		out.Items = append(out.Items, cloneIssue(f.issues[r][id]))
	}
	if !out.Complete {
		out.NextCursor = fmt.Sprint(end)
	}
	return out, nil
}

// GetIssue returns one issue snapshot.
func (f *Fake) GetIssue(ctx context.Context, r garden.RepoID, id garden.IssueID) (garden.Issue, error) {
	if e := checkContext(ctx); e != nil {
		return garden.Issue{}, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return garden.Issue{}, e
	}
	i, ok := f.issues[r][id]
	if !ok {
		return i, garden.ErrNotFound
	}
	return cloneIssue(i), nil
}

// ListComments returns the ordered comment snapshot for an issue.
func (f *Fake) ListComments(ctx context.Context, r garden.RepoID, id garden.IssueID) ([]garden.IssueComment, error) {
	if e := checkContext(ctx); e != nil {
		return nil, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return nil, e
	}
	return cloneComments(f.comments[r][id]), nil
}

// AppendComment appends a comment with fake server identity and creation time.
func (f *Fake) AppendComment(ctx context.Context, r garden.RepoID, id garden.IssueID, body string) (garden.IssueComment, error) {
	if e := checkContext(ctx); e != nil {
		return garden.IssueComment{}, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return garden.IssueComment{}, e
	}
	f.next++
	f.clockStep++
	c := garden.IssueComment{ID: f.next, Body: body, Author: "fake-authenticated", AuthorID: "fake-authenticated-id", CreatedAt: f.now().Add(time.Duration(f.clockStep)).UTC()}
	if f.comments[r] == nil {
		f.comments[r] = make(map[garden.IssueID][]garden.IssueComment)
	}
	f.comments[r][id] = append(f.comments[r][id], c)
	return c, nil
}

// UpdateLabels replaces an issue's label snapshot.
func (f *Fake) UpdateLabels(ctx context.Context, r garden.RepoID, id garden.IssueID, labels []string) error {
	if e := checkContext(ctx); e != nil {
		return e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return e
	}
	i, ok := f.issues[r][id]
	if !ok {
		return garden.ErrNotFound
	}
	i.Labels = append([]string(nil), labels...)
	f.issues[r][id] = i
	return nil
}

// ListProjects returns a repository's project view.
func (f *Fake) ListProjects(ctx context.Context, r garden.RepoID) ([]garden.Project, error) {
	if e := checkContext(ctx); e != nil {
		return nil, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return nil, e
	}
	return cloneProjects(f.projects[r]), nil
}

// EnsureProject returns or creates a project view.
func (f *Fake) EnsureProject(ctx context.Context, r garden.RepoID, name string) (garden.Project, error) {
	if e := checkContext(ctx); e != nil {
		return garden.Project{}, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return garden.Project{}, e
	}
	for _, p := range f.projects[r] {
		if p.Name == name {
			return cloneProject(p), nil
		}
	}
	p := garden.Project{ID: fmt.Sprintf("project-%d", len(f.projects[r])+1), Name: name}
	f.projects[r] = append(f.projects[r], p)
	return cloneProject(p), nil
}

// EnsureField returns the supplied project field as the fake setup result.
func (f *Fake) EnsureField(ctx context.Context, id string, field garden.ProjectField) (garden.ProjectField, error) {
	if e := checkContext(ctx); e != nil {
		return garden.ProjectField{}, e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.check(); e != nil {
		return garden.ProjectField{}, e
	}
	return field, nil
}

// EditComment changes an existing comment to model external edits.
func (f *Fake) EditComment(r garden.RepoID, id garden.IssueID, cid int64, body string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.comments[r][id] {
		if f.comments[r][id][i].ID == cid {
			f.comments[r][id][i].Body = body
			return true
		}
	}
	return false
}

// DeleteComment removes a comment to model external deletion.
func (f *Fake) DeleteComment(r garden.RepoID, id garden.IssueID, cid int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	cs := f.comments[r][id]
	for i := range cs {
		if cs[i].ID == cid {
			f.comments[r][id] = append(cs[:i], cs[i+1:]...)
			return true
		}
	}
	return false
}

// SimulateConcurrentEdit changes an issue body to model external edits.
func (f *Fake) SimulateConcurrentEdit(r garden.RepoID, id garden.IssueID, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.issues[r][id]
	i.Body = body
	f.issues[r][id] = i
}

func cloneIssue(i garden.Issue) garden.Issue {
	i.Labels = append([]string(nil), i.Labels...)
	i.Metadata = append([]byte(nil), i.Metadata...)
	return i
}
func cloneComments(in []garden.IssueComment) []garden.IssueComment {
	return append([]garden.IssueComment(nil), in...)
}
func cloneProject(p garden.Project) garden.Project {
	p.Fields = append([]garden.ProjectField(nil), p.Fields...)
	for i := range p.Fields {
		p.Fields[i].Values = append([]string(nil), p.Fields[i].Values...)
	}
	return p
}
func cloneProjects(in []garden.Project) []garden.Project {
	out := make([]garden.Project, len(in))
	for i, p := range in {
		out[i] = cloneProject(p)
	}
	return out
}
