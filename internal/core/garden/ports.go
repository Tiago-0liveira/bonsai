package garden

import "context"
import "time"

type Issue struct {
	Repo      RepoID
	ID        IssueID
	Number    int64
	Title     string
	NodeID    string
	AuthorID  string
	CreatedAt time.Time
	Labels    []string
	Body      string
	Metadata  []byte
	// Problems lists stable adapter-detected data defects (IssueProblem*). Readers surface each as an
	// Attention problem instead of failing the page.
	Problems []string
}

// IssueProblemAuthor marks an Issue whose author GitHub reported without a usable numeric ID (for example a
// deleted account). The Issue stays visible; the author is never trusted.
const IssueProblemAuthor = "invalid-author"

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

// NewIssue is the only Issue content Garden ever sends to GitHub: capture creates
// an Issue once. Every later mutation is a comment or a bonsai: label change.
type NewIssue struct {
	Title string
	Body  string
	// Labels must all be in the bonsai: namespace.
	Labels []string
}

// IssueCreator is separate from IssueWriter so that only capture holds the
// capability to create Issues.
type IssueCreator interface {
	CreateIssue(context.Context, RepoID, NewIssue) (Issue, error)
}
type Project struct {
	ID   string
	Name string
	// Number and Owner let setup validate a user-selected Project reference.
	Number string
	Owner  string
	// Description carries the operation marker used to adopt remote creates after a crash.
	Description string
	// CreatorLogin is the display login reported by ProjectV2.creator.
	CreatorLogin string
	Fields       []ProjectField
	// IssueFields is the observed item-value projection keyed by stable Issue ID and field name.
	IssueFields map[IssueID]map[string]string
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
