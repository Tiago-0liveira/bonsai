// Package gh wraps the GitHub CLI (`gh`) for the operations bonsai needs.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ghTimeout bounds every gh subprocess so a hung network call can't block a
// tea.Cmd goroutine forever. A var (not const) as a seam for future injection.
var ghTimeout = 30 * time.Second

// runGHOutput executes a gh subcommand in dir bounded by ghTimeout, returning
// stdout (like cmd.Output).
func runGHOutput(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	return cmd.Output()
}

// runGHCombined executes a gh subcommand in dir bounded by ghTimeout, returning
// combined stdout+stderr (like cmd.CombinedOutput).
func runGHCombined(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// PR states as reported by GitHub.
const (
	StateOpen   = "OPEN"
	StateMerged = "MERGED"
	StateClosed = "CLOSED"
)

// PR review decisions as reported by GitHub.
const (
	ReviewApproved         = "APPROVED"
	ReviewChangesRequested = "CHANGES_REQUESTED"
)

// PR is a pull request with its current state.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Head   string `json:"headRefName"`
	Base   string `json:"baseRefName"`
	URL    string `json:"url"`
	// IsDraft marks a draft pull request.
	IsDraft bool `json:"isDraft"`
	// ReviewDecision is the overall review verdict: APPROVED,
	// CHANGES_REQUESTED, REVIEW_REQUIRED, or "".
	ReviewDecision string `json:"reviewDecision"`
}

// ListPRs returns pull requests for the repo containing dir via
// `gh pr list --json`. state is an `--state` filter such as "open" or "all";
// with "all" results are sorted by most recently updated so recent merges are
// never pushed out of the window by older PRs. Requires the gh CLI to be
// installed and authenticated.
func ListPRs(dir, state string) ([]PR, error) {
	args := []string{"pr", "list", "--state", state, "--json", "number,title,state,headRefName,baseRefName,url,isDraft,reviewDecision", "--limit", "50"}
	if state == "all" {
		args = append(args, "--search", "sort:updated-desc")
	}
	out, err := runGHOutput(dir, args...)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh pr list: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh pr list (is gh installed?): %w", err)
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	return prs, nil
}

// TimelineItem is a single issue comment on a PR.
type TimelineItem struct {
	Author    string
	Body      string
	CreatedAt string
}

// Review is a single PR review (approval, change request, or comment).
type Review struct {
	Author      string
	State       string // APPROVED / CHANGES_REQUESTED / COMMENTED
	Body        string
	SubmittedAt string
}

// Commit is a single commit on a PR.
type Commit struct {
	OID      string // full sha
	Headline string
	Author   string
}

// Short returns the abbreviated (7-char) commit sha.
func (c Commit) Short() string {
	if len(c.OID) >= 7 {
		return c.OID[:7]
	}
	return c.OID
}

// PRDetail is the full view of a single pull request, mirroring what GitHub
// shows: metadata, body, and the comment/review timeline.
type PRDetail struct {
	Number       int
	Title        string
	State        string // OPEN / MERGED / CLOSED
	IsDraft      bool
	Author       string
	Base, Head   string
	Mergeable    string // MERGEABLE / CONFLICTING / UNKNOWN
	Labels       []string
	Assignees    []string
	Reviewers    []string // requested reviewers still pending
	Body         string   // markdown
	URL          string
	Additions    int
	Deletions    int
	ChangedFiles int
	Comments     []TimelineItem
	Reviews      []Review
	Commits      []Commit
	CreatedAt    string
	UpdatedAt    string
}

// prViewRaw mirrors the JSON shape of `gh pr view --json …` before it is
// flattened into a PRDetail.
type prViewRaw struct {
	Number      int                      `json:"number"`
	Title       string                   `json:"title"`
	State       string                   `json:"state"`
	IsDraft     bool                     `json:"isDraft"`
	Author      struct{ Login string }   `json:"author"`
	BaseRefName string                   `json:"baseRefName"`
	HeadRefName string                   `json:"headRefName"`
	Mergeable   string                   `json:"mergeable"`
	Labels      []struct{ Name string }  `json:"labels"`
	Assignees   []struct{ Login string } `json:"assignees"`
	ReviewReqs  []struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"reviewRequests"`
	Body         string `json:"body"`
	URL          string `json:"url"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	Comments     []struct {
		Author    struct{ Login string } `json:"author"`
		Body      string                 `json:"body"`
		CreatedAt string                 `json:"createdAt"`
	} `json:"comments"`
	Reviews []struct {
		Author      struct{ Login string } `json:"author"`
		State       string                 `json:"state"`
		Body        string                 `json:"body"`
		SubmittedAt string                 `json:"submittedAt"`
	} `json:"reviews"`
	Commits []struct {
		OID             string `json:"oid"`
		MessageHeadline string `json:"messageHeadline"`
		Authors         []struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"authors"`
	} `json:"commits"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

const prViewFields = "number,title,state,isDraft,author,baseRefName,headRefName," +
	"mergeable,labels,assignees,reviewRequests,body,url,additions,deletions," +
	"changedFiles,comments,reviews,commits,createdAt,updatedAt"

// ViewPR fetches the full detail of pull request number via a single
// `gh pr view --json` call, including the comment/review timeline.
func ViewPR(dir string, number int) (PRDetail, error) {
	out, err := runGHOutput(dir, "pr", "view", strconv.Itoa(number), "--json", prViewFields)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return PRDetail{}, fmt.Errorf("gh pr view: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return PRDetail{}, fmt.Errorf("gh pr view (is gh installed?): %w", err)
	}
	var r prViewRaw
	if err := json.Unmarshal(out, &r); err != nil {
		return PRDetail{}, fmt.Errorf("parse gh output: %w", err)
	}
	return flatten(r), nil
}

// flatten maps the raw JSON shape to a clean PRDetail.
func flatten(r prViewRaw) PRDetail {
	d := PRDetail{
		Number: r.Number, Title: r.Title, State: r.State, IsDraft: r.IsDraft,
		Author: r.Author.Login, Base: r.BaseRefName, Head: r.HeadRefName,
		Mergeable: r.Mergeable, Body: r.Body, URL: r.URL,
		Additions: r.Additions, Deletions: r.Deletions, ChangedFiles: r.ChangedFiles,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, l := range r.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	for _, a := range r.Assignees {
		d.Assignees = append(d.Assignees, a.Login)
	}
	for _, rr := range r.ReviewReqs {
		if rr.Login != "" {
			d.Reviewers = append(d.Reviewers, rr.Login)
		} else if rr.Name != "" {
			d.Reviewers = append(d.Reviewers, rr.Name)
		}
	}
	for _, c := range r.Comments {
		d.Comments = append(d.Comments, TimelineItem{Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, rv := range r.Reviews {
		d.Reviews = append(d.Reviews, Review{Author: rv.Author.Login, State: rv.State, Body: rv.Body, SubmittedAt: rv.SubmittedAt})
	}
	for _, c := range r.Commits {
		author := ""
		if len(c.Authors) > 0 {
			if c.Authors[0].Login != "" {
				author = c.Authors[0].Login
			} else {
				author = c.Authors[0].Name
			}
		}
		d.Commits = append(d.Commits, Commit{OID: c.OID, Headline: c.MessageHeadline, Author: author})
	}
	return d
}

// MergePR merges pull request number via `gh pr merge --merge` (a merge commit),
// letting GitHub target the PR's own base branch.
func MergePR(dir string, number int) error {
	return MergePRStrategy(dir, number, "merge")
}

// mergeArgs builds the `gh pr merge` argument list for a strategy
// (merge|squash|rebase). Split out so it is unit-testable without a shell-out.
func mergeArgs(number int, strat string) []string {
	flag := "--merge"
	switch strat {
	case "squash":
		flag = "--squash"
	case "rebase":
		flag = "--rebase"
	}
	return []string{"pr", "merge", strconv.Itoa(number), flag}
}

// runGH executes a gh subcommand in dir, returning a trimmed combined-output
// error on failure. Used by the fire-and-check action wrappers. Bounded by
// ghTimeout via runGHCombined.
func runGH(dir string, args ...string) error {
	out, err := runGHCombined(dir, args...)
	if err != nil {
		return fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

// MergePRStrategy merges a PR with an explicit strategy: "merge", "squash", or
// "rebase". GitHub targets the PR's own base branch.
func MergePRStrategy(dir string, number int, strat string) error {
	return runGH(dir, mergeArgs(number, strat)...)
}

// ClosePR closes a pull request without merging.
func ClosePR(dir string, number int) error { return runGH(dir, "pr", "close", strconv.Itoa(number)) }

// ReopenPR reopens a closed pull request.
func ReopenPR(dir string, number int) error { return runGH(dir, "pr", "reopen", strconv.Itoa(number)) }

// ReadyPR marks a draft pull request ready for review.
func ReadyPR(dir string, number int) error { return runGH(dir, "pr", "ready", strconv.Itoa(number)) }

// reviewArgs builds the `gh pr review` argument list. kind is
// "approve", "request-changes", or "comment"; body is optional.
func reviewArgs(number int, kind, body string) []string {
	flag := "--comment"
	switch kind {
	case "approve":
		flag = "--approve"
	case "request-changes":
		flag = "--request-changes"
	}
	args := []string{"pr", "review", strconv.Itoa(number), flag}
	if body != "" {
		args = append(args, "--body", body)
	}
	return args
}

// ReviewPR submits a review (approve / request-changes / comment) with an
// optional body. GitHub requires a body for request-changes and comment.
func ReviewPR(dir string, number int, kind, body string) error {
	return runGH(dir, reviewArgs(number, kind, body)...)
}

// CreatePR opens a new pull request from the current branch and returns its
// number. base is the target branch; an empty title lets gh prompt is avoided by
// requiring the caller to pass one.
func CreatePR(dir, title, body, base string, draft bool) (int, error) {
	args := []string{"pr", "create", "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	if draft {
		args = append(args, "--draft")
	}
	out, err := runGHCombined(dir, args...)
	if err != nil {
		return 0, fmt.Errorf("gh pr create: %s", strings.TrimSpace(string(out)))
	}
	// gh prints the new PR URL on the last line; the trailing path segment is the
	// number.
	return parseTrailingNumber(string(out)), nil
}

// parseTrailingNumber extracts the integer at the end of the last non-empty line
// of s (e.g. the number in a printed PR/issue URL). Returns 0 if none.
func parseTrailingNumber(s string) int {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i+1:]
	}
	n, err := strconv.Atoi(strings.TrimSpace(last))
	if err != nil {
		return 0
	}
	return n
}

// Check is a single CI/status check on a PR.
type Check struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Bucket string `json:"bucket"` // pass / fail / pending / skipping / cancel
	Link   string `json:"link"`
}

// Checks returns the CI checks for a PR via `gh pr checks --json`. The command
// exits non-zero when checks are failing or pending, so a non-zero exit whose
// output still parses as JSON is treated as success.
func Checks(dir string, number int) ([]Check, error) {
	out, err := runGHOutput(dir, "pr", "checks", strconv.Itoa(number), "--json", "name,state,bucket,link")
	if checks, perr := parseChecks(out); perr == nil {
		return checks, nil
	}
	// Output did not parse — surface the real error (gh missing, no checks, etc.).
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh pr checks: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh pr checks: %w", err)
	}
	return nil, nil
}

// parseChecks parses out as a checks JSON array.
func parseChecks(out []byte) ([]Check, error) {
	var checks []Check
	err := json.Unmarshal(out, &checks)
	return checks, err
}

// Rollup reduces a set of checks to a single state: "fail" if any failed,
// else "pending" if any are still running, else "pass". Empty input yields "".
func Rollup(checks []Check) string {
	if len(checks) == 0 {
		return ""
	}
	pending := false
	for _, c := range checks {
		switch c.Bucket {
		case "fail", "cancel":
			return "fail"
		case "pending":
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "pass"
}

// ListMergedPRs returns recently merged pull requests (head branch + number),
// used to detect worktrees whose branch has already been merged.
func ListMergedPRs(dir string) ([]PR, error) {
	out, err := runGHOutput(dir, "pr", "list", "--state", "merged", "--json", "number,title,headRefName,baseRefName,url", "--limit", "100")
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh pr list --state merged: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	return prs, nil
}

// Run is a single GitHub Actions workflow run on a branch.
type Run struct {
	ID         int64  `json:"databaseId"`
	Name       string `json:"displayTitle"`
	Workflow   string `json:"workflowName"`
	Branch     string `json:"headBranch"`
	Event      string `json:"event"`
	Status     string `json:"status"`     // queued / in_progress / completed
	Conclusion string `json:"conclusion"` // success / failure / cancelled / ""
	CreatedAt  string `json:"createdAt"`
	URL        string `json:"url"`
}

// Bucket reduces a run's status+conclusion to the same pass/fail/pending buckets
// used by PR checks, so the Checks tab and row dots share one glyph path.
func (r Run) Bucket() string {
	if r.Status != "completed" {
		return "pending"
	}
	switch r.Conclusion {
	case "success", "neutral", "skipped":
		return "pass"
	case "failure", "cancelled", "timed_out", "action_required":
		return "fail"
	default:
		return "pending"
	}
}

const runListFields = "databaseId,displayTitle,workflowName,headBranch,event,status,conclusion,createdAt,url"

// ListRuns returns the most recent workflow runs for branch via
// `gh run list --branch --json`. Like Checks, gh may exit non-zero while still
// printing parseable JSON, so parseable output wins.
func ListRuns(dir, branch string, limit int) ([]Run, error) {
	out, err := runGHOutput(dir, "run", "list", "--branch", branch,
		"--json", runListFields, "--limit", strconv.Itoa(limit))
	var runs []Run
	if perr := json.Unmarshal(out, &runs); perr == nil {
		return runs, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh run list: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh run list (is gh installed?): %w", err)
	}
	return nil, fmt.Errorf("gh run list: unparseable output")
}
