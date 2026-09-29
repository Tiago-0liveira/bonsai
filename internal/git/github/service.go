package github

import (
	"context"
	"time"
)

type RemoteRepository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}
type RemoteBranch struct {
	Name          string `json:"name"`
	RemoteHeadSHA string `json:"remote_head_sha"`
	Protected     bool   `json:"protected"`
}
type PRFilter struct {
	State string
	Head  string
	Base  string
}
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Head           string    `json:"head"`
	HeadRepository string    `json:"head_repository,omitempty"`
	Base           string    `json:"base"`
	HeadSHA        string    `json:"head_sha"`
	URL       string    `json:"url"`
	Draft     bool      `json:"draft"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	NodeID    string    `json:"node_id"`
}
type PullRequestDetail struct {
	PullRequest
	Mergeable string    `json:"mergeable"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	Comments  []Comment `json:"comments"`
	Reviews   []Review  `json:"reviews"`
	Commits   []Commit  `json:"commits"`
	Files     []File    `json:"files"`
}
type Comment struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
}
type Review struct {
	ID          int64     `json:"id"`
	Author      string    `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
}
type Commit struct {
	SHA       string    `json:"sha"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}
type File struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}
type Check struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Branch     string `json:"branch"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}
type CreatePullRequestRequest struct {
	Repository string `json:"repository"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Head       string `json:"head"`
	Base       string `json:"base"`
	Draft      bool   `json:"draft"`
}
type ReviewRequest struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Event      string `json:"event"`
	Body       string `json:"body"`
	CommitID   string `json:"commit_id,omitempty"`
}
type MergePullRequestRequest struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Method     string `json:"method"`
	HeadSHA    string `json:"head_sha"`
}
type GitHubService interface {
	Repository(context.Context, string) (RemoteRepository, error)
	Branches(context.Context, string) ([]RemoteBranch, error)
	PullRequests(context.Context, string, PRFilter) ([]PullRequest, error)
	PullRequest(context.Context, string, int) (PullRequestDetail, error)
	CreatePullRequest(context.Context, CreatePullRequestRequest) (PullRequest, error)
	ReviewPullRequest(context.Context, ReviewRequest) error
	ReadyPullRequest(context.Context, string, int) error
	ClosePullRequest(context.Context, string, int) error
	ReopenPullRequest(context.Context, string, int) error
	MergePullRequest(context.Context, MergePullRequestRequest) error
	Comment(context.Context, string, int, string) error
	Checks(context.Context, string, string) ([]Check, error)
	WorkflowRuns(context.Context, string, string) ([]WorkflowRun, error)
}
