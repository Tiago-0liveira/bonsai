package garden

import "context"
import "time"

type Issue struct {
	Repo     RepoID
	ID       IssueID
	NodeID   string
	Labels   []string
	Body     string
	Metadata []byte
}
type IssuePage struct {
	Items      []Issue
	NextCursor string
	Complete   bool
}
type IssueComment struct {
	ID        int64
	Author    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// IssueReader separates observation from any capability to mutate remote data.
type IssueReader interface {
	ListManagedIssues(context.Context, RepoID, string) (IssuePage, error)
	GetIssue(context.Context, RepoID, IssueID) (Issue, error)
	ListComments(context.Context, RepoID, IssueID) ([]IssueComment, error)
}

// IssueWriter contains only the mutations needed by the canonical contract.
type IssueWriter interface {
	AppendComment(context.Context, RepoID, IssueID, string) (IssueComment, error)
	UpdateLabels(context.Context, RepoID, IssueID, []string) error
}
type Project struct {
	ID     string
	Name   string
	Fields []ProjectField
}
type ProjectField struct {
	ID     string
	Name   string
	Values []string
}

// ProjectReader observes Projects as a view of Issue state.
type ProjectReader interface {
	ListProjects(context.Context, RepoID) ([]Project, error)
}

// ProjectSetupWriter creates project views and fields without granting issue mutation access.
type ProjectSetupWriter interface {
	EnsureProject(context.Context, RepoID, string) (Project, error)
	EnsureField(context.Context, string, ProjectField) (ProjectField, error)
}
