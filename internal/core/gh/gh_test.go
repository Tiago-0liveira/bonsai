package gh

import (
	"encoding/json"
	"testing"
)

func TestMergeArgs(t *testing.T) {
	cases := map[string]string{
		"merge":   "--merge",
		"squash":  "--squash",
		"rebase":  "--rebase",
		"unknown": "--merge", // default
	}
	for strat, want := range cases {
		args := mergeArgs(7, strat)
		if len(args) != 4 || args[0] != "pr" || args[1] != "merge" || args[2] != "7" || args[3] != want {
			t.Errorf("mergeArgs(7,%q) = %v, want flag %q", strat, args, want)
		}
	}
}

func TestReviewArgs(t *testing.T) {
	got := reviewArgs(3, "approve", "")
	if len(got) != 4 || got[3] != "--approve" {
		t.Errorf("approve without body = %v", got)
	}
	got = reviewArgs(3, "request-changes", "fix it")
	if got[3] != "--request-changes" || got[len(got)-2] != "--body" || got[len(got)-1] != "fix it" {
		t.Errorf("request-changes = %v", got)
	}
	got = reviewArgs(3, "comment", "")
	if got[3] != "--comment" {
		t.Errorf("comment = %v", got)
	}
}

func TestRollup(t *testing.T) {
	cases := []struct {
		name   string
		in     []Check
		expect string
	}{
		{"empty", nil, ""},
		{"all pass", []Check{{Bucket: "pass"}, {Bucket: "pass"}}, "pass"},
		{"one fail", []Check{{Bucket: "pass"}, {Bucket: "fail"}}, "fail"},
		{"cancel is fail", []Check{{Bucket: "cancel"}}, "fail"},
		{"pending wins over pass", []Check{{Bucket: "pass"}, {Bucket: "pending"}}, "pending"},
		{"fail wins over pending", []Check{{Bucket: "pending"}, {Bucket: "fail"}}, "fail"},
		{"skipping is pass", []Check{{Bucket: "skipping"}}, "pass"},
	}
	for _, c := range cases {
		if got := Rollup(c.in); got != c.expect {
			t.Errorf("%s: Rollup = %q, want %q", c.name, got, c.expect)
		}
	}
}

func TestParseTrailingNumber(t *testing.T) {
	cases := map[string]int{
		"https://github.com/o/r/pull/42\n":     42,
		"noise\nhttps://github.com/o/r/pull/7": 7,
		"123":                                  123,
		"garbage":                              0,
	}
	for in, want := range cases {
		if got := parseTrailingNumber(in); got != want {
			t.Errorf("parseTrailingNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestRunBucket(t *testing.T) {
	cases := []struct {
		name string
		run  Run
		want string
	}{
		{"in progress", Run{Status: "in_progress", Conclusion: ""}, "pending"},
		{"queued", Run{Status: "queued"}, "pending"},
		{"success", Run{Status: "completed", Conclusion: "success"}, "pass"},
		{"skipped passes", Run{Status: "completed", Conclusion: "skipped"}, "pass"},
		{"failure", Run{Status: "completed", Conclusion: "failure"}, "fail"},
		{"cancelled fails", Run{Status: "completed", Conclusion: "cancelled"}, "fail"},
		{"unknown conclusion", Run{Status: "completed", Conclusion: "stale"}, "pending"},
	}
	for _, c := range cases {
		if got := c.run.Bucket(); got != c.want {
			t.Errorf("%s: Bucket = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestListRunsUnmarshal(t *testing.T) {
	raw := `[
      {"databaseId": 1, "displayTitle": "fix: thing", "workflowName": "ci",
       "headBranch": "feat", "event": "push", "status": "completed",
       "conclusion": "success", "createdAt": "2026-08-01T10:00:00Z",
       "url": "https://github.com/o/r/actions/runs/1"},
      {"databaseId": 2, "displayTitle": "wip", "workflowName": "ci",
       "headBranch": "feat", "event": "pull_request", "status": "in_progress",
       "conclusion": "", "createdAt": "2026-08-02T10:00:00Z",
       "url": "https://github.com/o/r/actions/runs/2"}
    ]`
	var runs []Run
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(runs) != 2 || runs[0].ID != 1 || runs[0].Workflow != "ci" || runs[0].Branch != "feat" {
		t.Fatalf("runs wrong: %+v", runs)
	}
	if runs[0].Bucket() != "pass" || runs[1].Bucket() != "pending" {
		t.Errorf("buckets wrong: %q %q", runs[0].Bucket(), runs[1].Bucket())
	}
}

func TestViewPRUnmarshal(t *testing.T) {
	// A representative gh pr view --json payload.
	raw := `{
      "number": 12, "title": "Add login", "state": "OPEN", "isDraft": true,
      "author": {"login": "alice"},
      "baseRefName": "main", "headRefName": "feat-login", "mergeable": "CONFLICTING",
      "labels": [{"name": "enhancement"}, {"name": "auth"}],
      "assignees": [{"login": "bob"}],
      "reviewRequests": [{"login": "carol"}, {"name": "team-x"}],
      "body": "does the thing", "url": "https://github.com/o/r/pull/12",
      "additions": 40, "deletions": 3, "changedFiles": 5,
      "comments": [{"author": {"login": "dan"}, "body": "nit", "createdAt": "2026-01-02T00:00:00Z"}],
      "reviews": [{"author": {"login": "carol"}, "state": "APPROVED", "body": "lgtm", "submittedAt": "2026-01-03T00:00:00Z"}],
      "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-04T00:00:00Z"
    }`
	var r prViewRaw
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Reuse the same flattening ViewPR performs.
	d := flatten(r)
	if d.Number != 12 || d.Title != "Add login" || !d.IsDraft || d.Author != "alice" {
		t.Errorf("header wrong: %+v", d)
	}
	if d.Mergeable != "CONFLICTING" || d.Additions != 40 || d.ChangedFiles != 5 {
		t.Errorf("metrics wrong: %+v", d)
	}
	if len(d.Labels) != 2 || d.Labels[0] != "enhancement" {
		t.Errorf("labels wrong: %v", d.Labels)
	}
	if len(d.Reviewers) != 2 || d.Reviewers[1] != "team-x" {
		t.Errorf("reviewers wrong: %v", d.Reviewers)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author != "dan" {
		t.Errorf("comments wrong: %v", d.Comments)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "APPROVED" {
		t.Errorf("reviews wrong: %v", d.Reviews)
	}
}
