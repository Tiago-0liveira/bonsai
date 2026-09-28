package app

import (
	"context"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) Checks(ctx context.Context, repo, sha string) ([]gh.Check, error) {
	p, e := repoPath(repo)
	if e != nil {
		return nil, e
	}
	if sha == "" {
		return nil, domain.ErrInvalid
	}
	out := []gh.Check{}
	for page := 1; page <= 100; page++ {
		var raw struct {
			CheckRuns []struct {
				ID                       int64
				Name, Status, Conclusion string
				HTMLURL                  string `json:"html_url"`
			} `json:"check_runs"`
		}
		h, e := c.request(ctx, repo, "GET", p+"/commits/"+escaped(sha)+"/check-runs?per_page=100&page="+strconv.Itoa(page), nil, &raw)
		if e != nil {
			return nil, e
		}
		for _, v := range raw.CheckRuns {
			out = append(out, gh.Check{ID: v.ID, Name: v.Name, Status: v.Status, Conclusion: v.Conclusion, URL: v.HTMLURL})
		}
		if !strings.Contains(h.Get("Link"), `rel="next"`) {
			break
		}
		if page == 100 {
			return nil, domain.E("too_large", "too many check pages")
		}
	}
	statuses, e := pages[struct {
		ID             int64
		Context, State string
		TargetURL      string `json:"target_url"`
	}](ctx, c, repo, p+"/commits/"+escaped(sha)+"/statuses")
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for _, v := range statuses {
		if seen[v.Context] {
			continue
		}
		seen[v.Context] = true
		status := "completed"
		conclusion := v.State
		if v.State == "pending" {
			status = "in_progress"
			conclusion = ""
		}
		out = append(out, gh.Check{ID: v.ID, Name: v.Context, Status: status, Conclusion: conclusion, URL: v.TargetURL})
	}
	return out, nil
}
func (c *Client) WorkflowRuns(ctx context.Context, repo, branch string) ([]gh.WorkflowRun, error) {
	p, e := repoPath(repo)
	if e != nil {
		return nil, e
	}
	out := []gh.WorkflowRun{}
	for page := 1; page <= 100; page++ {
		var raw struct {
			Runs []struct {
				ID                       int64
				Name, Status, Conclusion string
				HeadBranch               string `json:"head_branch"`
				HeadSHA                  string `json:"head_sha"`
				HTMLURL                  string `json:"html_url"`
			} `json:"workflow_runs"`
		}
		h, e := c.request(ctx, repo, "GET", p+"/actions/runs?branch="+url.QueryEscape(branch)+"&per_page=100&page="+strconv.Itoa(page), nil, &raw)
		if e != nil {
			return nil, e
		}
		for _, v := range raw.Runs {
			out = append(out, gh.WorkflowRun{ID: v.ID, Name: v.Name, Branch: v.HeadBranch, HeadSHA: v.HeadSHA, Status: v.Status, Conclusion: v.Conclusion, URL: v.HTMLURL})
		}
		if !strings.Contains(h.Get("Link"), `rel="next"`) {
			return out, nil
		}
	}
	return nil, domain.E("too_large", "too many workflow pages")
}
