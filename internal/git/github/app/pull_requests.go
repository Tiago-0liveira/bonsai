package app

import (
	"context"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type rawPR struct {
	Number             int
	Title, Body, State string
	Head               struct {
		Ref, SHA string
		Repo     *struct {
			FullName string `json:"full_name"`
		}
	}
	Base                 struct{ Ref, SHA string }
	User                 struct{ Login string }
	Draft                bool
	Merged               bool
	MergedAt             *time.Time `json:"merged_at"`
	HTMLURL              string     `json:"html_url"`
	NodeID               string     `json:"node_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	Mergeable            *bool
	Additions, Deletions int
	ChangedFiles         int                      `json:"changed_files"`
	RequestedReviewers   []struct{ Login string } `json:"requested_reviewers"`
	RequestedTeams       []struct{ Slug string }  `json:"requested_teams"`
}

func (p rawPR) domain() gh.PullRequest {
	state := p.State
	if p.Merged || p.MergedAt != nil {
		state = "merged"
	}
	headRepository := ""
	if p.Head.Repo != nil {
		headRepository = p.Head.Repo.FullName
	}
	return gh.PullRequest{Number: p.Number, Title: p.Title, Body: p.Body, State: state, Head: p.Head.Ref, HeadRepository: headRepository, Base: p.Base.Ref, HeadSHA: p.Head.SHA, URL: p.HTMLURL, Draft: p.Draft, Author: p.User.Login, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, NodeID: p.NodeID}
}
func (c *Client) PullRequests(ctx context.Context, repo string, f gh.PRFilter) ([]gh.PullRequest, error) {
	out := []gh.PullRequest{}
	for page := 1; page != 0; {
		batch, err := c.PullRequestPage(ctx, repo, f, page)
		if err != nil {
			return nil, err
		}
		out = append(out, batch.Items...)
		page = batch.NextPage
	}
	return out, nil
}

func (c *Client) PullRequestPage(ctx context.Context, repo string, f gh.PRFilter, page int) (gh.PullRequestPage, error) {
	if page < 1 {
		return gh.PullRequestPage{}, domain.ErrInvalid
	}
	p, e := repoPath(repo)
	if e != nil {
		return gh.PullRequestPage{}, e
	}
	if f.State == "" {
		f.State = "open"
	}
	if f.State != "open" && f.State != "closed" && f.State != "all" {
		return gh.PullRequestPage{}, domain.ErrInvalid
	}
	q := url.Values{"state": {f.State}, "sort": {"updated"}, "direction": {"desc"}}
	q.Set("per_page", "100")
	q.Set("page", strconv.Itoa(page))
	if f.Head != "" {
		q.Set("head", f.Head)
	}
	if f.Base != "" {
		q.Set("base", f.Base)
	}
	var rows []rawPR
	h, e := c.request(ctx, repo, "GET", p+"/pulls?"+q.Encode(), nil, &rows)
	if e != nil {
		return gh.PullRequestPage{}, e
	}
	out := []gh.PullRequest{}
	for _, p := range rows {
		out = append(out, p.domain())
	}
	batch := gh.PullRequestPage{Items: out, NotModified: h.Get(NotModifiedHeader) != ""}
	if strings.Contains(h.Get("Link"), `rel="next"`) {
		batch.NextPage = page + 1
	}
	return batch, nil
}
func (c *Client) PullRequest(ctx context.Context, repo string, n int) (gh.PullRequestDetail, error) {
	p, e := pullPath(repo, n)
	if e != nil {
		return gh.PullRequestDetail{}, e
	}
	var raw rawPR
	if _, e = c.request(ctx, repo, "GET", p, nil, &raw); e != nil {
		return gh.PullRequestDetail{}, e
	}
	d := gh.PullRequestDetail{PullRequest: raw.domain(), Mergeable: "unknown", Additions: raw.Additions, Deletions: raw.Deletions, ChangedFiles: raw.ChangedFiles, RequestedReviewers: []string{}, Comments: []gh.Comment{}, Reviews: []gh.Review{}, Commits: []gh.Commit{}, Files: []gh.File{}}
	if raw.Mergeable != nil {
		d.Mergeable = "conflicting"
		if *raw.Mergeable {
			d.Mergeable = "mergeable"
		}
	}
	for _, reviewer := range raw.RequestedReviewers {
		d.RequestedReviewers = append(d.RequestedReviewers, reviewer.Login)
	}
	for _, team := range raw.RequestedTeams {
		d.RequestedReviewers = append(d.RequestedReviewers, team.Slug)
	}
	comments, e := pages[struct {
		ID        int64
		User      struct{ Login string }
		Body      string
		CreatedAt time.Time `json:"created_at"`
	}](ctx, c, repo, strings.Replace(p, "/pulls/", "/issues/", 1)+"/comments")
	if e != nil {
		return d, e
	}
	for _, v := range comments {
		d.Comments = append(d.Comments, gh.Comment{ID: v.ID, Author: v.User.Login, Body: v.Body, CreatedAt: v.CreatedAt})
	}
	inline, e := pages[struct {
		ID         int64
		User       struct{ Login string }
		Body, Path string
		Line       int
		CreatedAt  time.Time `json:"created_at"`
	}](ctx, c, repo, p+"/comments")
	if e != nil {
		return d, e
	}
	for _, v := range inline {
		d.Comments = append(d.Comments, gh.Comment{ID: v.ID, Author: v.User.Login, Body: v.Body, Path: v.Path, Line: v.Line, CreatedAt: v.CreatedAt})
	}
	reviews, e := pages[struct {
		ID          int64
		User        struct{ Login string }
		State, Body string
		SubmittedAt time.Time `json:"submitted_at"`
	}](ctx, c, repo, p+"/reviews")
	if e != nil {
		return d, e
	}
	for _, v := range reviews {
		d.Reviews = append(d.Reviews, gh.Review{ID: v.ID, Author: v.User.Login, State: v.State, Body: v.Body, SubmittedAt: v.SubmittedAt})
	}
	d.ReviewSummary = gh.SummarizeReviews(d.Reviews)
	commits, e := pages[struct {
		SHA    string
		Commit struct {
			Message string
			Author  struct {
				Name string
				Date time.Time
			}
		}
	}](ctx, c, repo, p+"/commits")
	if e != nil {
		return d, e
	}
	for _, v := range commits {
		d.Commits = append(d.Commits, gh.Commit{SHA: v.SHA, Message: v.Commit.Message, Author: v.Commit.Author.Name, CreatedAt: v.Commit.Author.Date})
	}
	files, e := pages[struct {
		Filename, Status, Patch string
		Additions, Deletions    int
	}](ctx, c, repo, p+"/files")
	if e != nil {
		return d, e
	}
	for _, v := range files {
		d.Files = append(d.Files, gh.File{Path: v.Filename, Status: v.Status, Patch: v.Patch, Additions: v.Additions, Deletions: v.Deletions})
	}
	if d.State == "open" {
		d.BehindBy = c.behindBy(ctx, repo, d.Base, d.HeadSHA)
	}
	return d, nil
}

// behindBy is best effort: the row is hidden rather than failing the whole
// pull request when the comparison is unavailable. It asks GraphQL for the
// count alone; the REST compare endpoint would also return the file diffs.
func (c *Client) behindBy(ctx context.Context, repo, base, headSHA string) *int {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || base == "" || headSHA == "" {
		return nil
	}
	const query = `query($owner:String!,$name:String!,$base:String!,$head:String!){repository(owner:$owner,name:$name){ref(qualifiedName:$base){compare(headRef:$head){behindBy}}}}`
	var response struct {
		Data struct {
			Repository *struct {
				Ref *struct {
					Compare *struct{ BehindBy *int } `json:"compare"`
				} `json:"ref"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	variables := map[string]string{"owner": owner, "name": name, "base": "refs/heads/" + base, "head": headSHA}
	if _, e := c.do(ctx, repo, "POST", "/graphql", map[string]any{"query": query, "variables": variables}, &response, false); e != nil || len(response.Errors) > 0 {
		return nil
	}
	if r := response.Data.Repository; r != nil && r.Ref != nil && r.Ref.Compare != nil {
		return r.Ref.Compare.BehindBy
	}
	return nil
}
func (c *Client) CreatePullRequest(ctx context.Context, r gh.CreatePullRequestRequest) (gh.PullRequest, error) {
	p, e := repoPath(r.Repository)
	if e != nil {
		return gh.PullRequest{}, e
	}
	if r.Title == "" || r.Head == "" || r.Base == "" {
		return gh.PullRequest{}, domain.ErrInvalid
	}
	var raw rawPR
	_, e = c.request(ctx, r.Repository, "POST", p+"/pulls", map[string]any{"title": r.Title, "body": r.Body, "head": r.Head, "base": r.Base, "draft": r.Draft}, &raw)
	return raw.domain(), e
}
func (c *Client) ReviewPullRequest(ctx context.Context, r gh.ReviewRequest) error {
	p, e := pullPath(r.Repository, r.Number)
	if e != nil {
		return e
	}
	switch r.Event {
	case "APPROVE", "REQUEST_CHANGES", "COMMENT":
	default:
		return domain.ErrInvalid
	}
	body := map[string]any{"event": r.Event, "body": r.Body}
	if r.CommitID != "" {
		body["commit_id"] = r.CommitID
	}
	_, e = c.request(ctx, r.Repository, "POST", p+"/reviews", body, nil)
	return e
}
func (c *Client) ReadyPullRequest(ctx context.Context, repo string, n int) error {
	p, e := pullPath(repo, n)
	if e != nil {
		return e
	}
	var raw rawPR
	if _, e = c.request(ctx, repo, "GET", p, nil, &raw); e != nil {
		return e
	}
	var response struct{ Errors []struct{ Message string } }
	_, e = c.request(ctx, repo, "POST", "/graphql", map[string]any{"query": `mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{id}}}`, "variables": map[string]string{"id": raw.NodeID}}, &response)
	if e == nil && len(response.Errors) > 0 {
		return domain.E("protected", response.Errors[0].Message)
	}
	return e
}
func (c *Client) state(ctx context.Context, repo string, n int, state string) error {
	p, e := pullPath(repo, n)
	if e != nil {
		return e
	}
	_, e = c.request(ctx, repo, "PATCH", p, map[string]string{"state": state}, nil)
	return e
}
func (c *Client) ClosePullRequest(ctx context.Context, repo string, n int) error {
	return c.state(ctx, repo, n, "closed")
}
func (c *Client) ReopenPullRequest(ctx context.Context, repo string, n int) error {
	return c.state(ctx, repo, n, "open")
}
func (c *Client) MergePullRequest(ctx context.Context, r gh.MergePullRequestRequest) error {
	p, e := pullPath(r.Repository, r.Number)
	if e != nil {
		return e
	}
	if r.Method != "merge" && r.Method != "squash" && r.Method != "rebase" {
		return domain.ErrInvalid
	}
	if r.HeadSHA == "" {
		return domain.E("invalid", "expected head SHA is required")
	}
	var result struct {
		Merged  bool
		Message string
	}
	_, e = c.request(ctx, r.Repository, "PUT", p+"/merge", map[string]string{"merge_method": r.Method, "sha": r.HeadSHA}, &result)
	if e == nil && !result.Merged {
		return domain.E("protected", result.Message)
	}
	return e
}
func (c *Client) Comment(ctx context.Context, repo string, n int, body string) error {
	p, e := pullPath(repo, n)
	if e != nil {
		return e
	}
	if strings.TrimSpace(body) == "" {
		return domain.ErrInvalid
	}
	_, e = c.request(ctx, repo, "POST", strings.Replace(p, "/pulls/", "/issues/", 1)+"/comments", map[string]string{"body": body}, nil)
	return e
}
