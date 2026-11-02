package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	pgfake "github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi/fake"
)

// chgEnv is the Playground with the fake Changes backend, on sessions s1 (launched from Issue 151) and s2 (manual).
type chgEnv struct {
	*pgEnv
	pa *pgfake.Backend
}

func newChgEnv(t *testing.T, w, h int) *chgEnv {
	t.Helper()
	pa := pgfake.New()
	e := &chgEnv{pgEnv: newPgEnv(t, w, h, WithPlayground(pa)), pa: pa}
	e.m.pg.debounce = time.Millisecond
	old := agentNow
	agentNow = func() time.Time { return pa.Now().Add(12 * time.Second) }
	t.Cleanup(func() { agentNow = old })
	s1 := e.sess("s1", "claude", "work", "running", 1)
	s1.Name = "attempt one"
	s1.Task = &agents.TaskRef{RepositoryID: 1, IssueID: 7, AttemptID: "att1"}
	s2 := e.sess("s2", "codex", "personal", "running", 2)
	s2.Name = "manual run"
	e.fb.Sessions.Add(s1, s2)
	e.m.garden.board.collection = pgCollection()
	for _, id := range []string{"s1", "s2"} {
		pa.ChangesBySession[id] = chgView()
		pa.PRBySession[id] = prFound()
		pa.ChecksBySession[id] = checksMixed()
		pa.DiffBySession[id] = map[string]playgroundapi.FileDiffView{"usage_service.go": diffView(), "usagepane.go": diffView(), "usage_test.go": diffView()}
	}
	e.open(t)
	return e
}

func (e *chgEnv) count(method string) int {
	n := 0
	for _, c := range e.pa.Calls {
		if c == method {
			n++
		}
	}
	return n
}

func chgView() playgroundapi.ChangesView {
	return playgroundapi.ChangesView{
		State:  playgroundapi.WorktreeOK,
		Branch: "feat/151-usage",
		Head:   playgroundapi.Commit{SHA: "abcdef1234567", Short: "abcdef1", Subject: "wip"},
		Base:   playgroundapi.Base{Source: playgroundapi.BaseUpstream, Ref: "origin/main", MergeBase: "1234567890abc"},
		Task:   playgroundapi.TaskLink{Source: playgroundapi.TaskNone},
		Files: []playgroundapi.FileChange{
			{Path: "usage_service.go", Status: playgroundapi.FileModified, Additions: 18, Deletions: 7, Counted: true, Uncommitted: true},
			{Path: "usagepane.go", Status: playgroundapi.FileModified, Additions: 4, Deletions: 2, Counted: true},
			{Path: "usage_test.go", Status: playgroundapi.FileAdded, Additions: 22, Counted: true},
		},
		Totals:       playgroundapi.Totals{Files: 3, Additions: 44, Deletions: 9, Uncommitted: 1},
		Commits:      []playgroundapi.Commit{{SHA: "c1", Short: "c1aaaaa", Subject: "fix refresh", Author: "ann"}, {SHA: "c2", Short: "c2bbbbb", Subject: "add tests", Author: "bob"}},
		CommitsTotal: 2,
	}
}

func prFound() playgroundapi.PRView {
	return playgroundapi.PRView{State: playgroundapi.PRFound, PR: &playgroundapi.PullRequest{Number: 67, Title: "Keep last good usage", Status: playgroundapi.PROpen, Draft: true, Base: "main", Head: "feat/151-usage", HeadSHA: "abcdef1234567", HeadMatchesLocal: true, ReviewKnown: true, Approvals: 1}}
}

func checksMixed() playgroundapi.ChecksView {
	return playgroundapi.ChecksView{
		State: playgroundapi.ChecksFailed, HeadSHA: "abcdef1234567", LocalHead: "abcdef1234567", IsLocal: true,
		Checks: []playgroundapi.Check{
			{Name: "build", Result: playgroundapi.ChecksPassed}, {Name: "lint", Result: playgroundapi.ChecksPassed},
			{Name: "race-tests", Result: playgroundapi.ChecksFailed}, {Name: "e2e", Result: playgroundapi.ChecksPending},
		},
		Counts: playgroundapi.ChecksCounts{Passed: 2, Failed: 1, Pending: 1},
	}
}

func diffView() playgroundapi.FileDiffView {
	var hunks []playgroundapi.Hunk
	for h := 0; h < 3; h++ {
		hk := playgroundapi.Hunk{Header: fmt.Sprintf("@@ -%d,6 +%d,7 @@ func f%d()", h*20+1, h*20+1, h)}
		for i := 0; i < 8; i++ {
			kind, text := playgroundapi.DiffCtx, fmt.Sprintf("ctx h%d l%d", h, i)
			switch i {
			case 3:
				kind, text = playgroundapi.DiffDel, fmt.Sprintf("return emptySnapshot h%d", h)
			case 4:
				kind, text = playgroundapi.DiffAdd, fmt.Sprintf("return lastGood.WithStale(err) h%d", h)
			}
			hk.Lines = append(hk.Lines, playgroundapi.DiffLine{Kind: kind, Text: text, Old: h*20 + i + 1, New: h*20 + i + 1})
		}
		hunks = append(hunks, hk)
	}
	return playgroundapi.FileDiffView{Hunks: hunks, Lines: 27}
}

func (e *chgEnv) enter(t *testing.T) {
	t.Helper()
	e.key(t, "c")
	if !e.m.pg.chg.open {
		t.Fatalf("c did not open Changes:\n%s", view(e.m))
	}
}

func TestPlaygroundChangesReadsOnlyOnEnterAndRefresh(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	if n := e.count("Changes") + e.count("PullRequest") + e.count("Checks"); n != 0 {
		t.Fatalf("entering the Playground read %d changes/PR/CI calls", n)
	}
	if e.count("Links") != 1 {
		t.Fatalf("Links reads on entering = %d, want 1", e.count("Links"))
	}
	e.key(t, "down", "up", "down", "up")
	if e.count("Changes") != 0 {
		t.Fatal("list navigation read changes")
	}
	e.enter(t)
	for _, m := range []string{"Changes", "PullRequest", "Checks"} {
		if e.count(m) != 1 {
			t.Fatalf("%s calls after enter = %d", m, e.count(m))
		}
	}
	e.key(t, "down", "down", "up")
	if e.count("Changes") != 1 || e.count("FileDiff") != 0 {
		t.Fatalf("moving through files read: %v", e.pa.Calls)
	}
	e.key(t, "r")
	if e.count("Changes") != 2 || e.count("PullRequest") != 2 || e.count("Checks") != 2 {
		t.Fatalf("refresh: %v", e.pa.Calls)
	}
	e.key(t, "esc", "down", "up")
	if e.m.pg.chg.open || e.count("Changes") != 2 {
		t.Fatalf("leaving or navigating read again: open=%v calls=%v", e.m.pg.chg.open, e.pa.Calls)
	}
	e.enter(t)
	if e.count("Changes") != 3 {
		t.Fatalf("re-entering did not read: %d", e.count("Changes"))
	}
}

func TestPlaygroundChangesScreenShowsTheDesignContent(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.enter(t)
	mustContain(t, "changes", view(e.m),
		"Playground › Sessions › #151 › Changed files",
		"CHANGES / feat/151-usage / base: origin/main (upstream)",
		"HEAD abcdef1", "merge-base 1234567", "2 of 2 commits", "synced 12s ago",
		"3 files / +44 -9", "PR #67 / Draft",
		"✓ 2 checks passed", "! 1 failed: race-tests", "◷ 1 pending",
		"usage_service.go", "+18 -7", "usagepane.go", "usage_test.go", "+22",
		"No Garden task", "COMMITS 2 of 2", "fix refresh")
	if strings.Contains(view(e.m), "approved plan") && !strings.Contains(view(e.m), "No approved plan") {
		t.Error("an unlinked session talks about a plan")
	}
}

func TestPlaygroundChangesCallsFailIndependentlyAndKeepLastGood(t *testing.T) {
	cases := []struct {
		name string
		fail func(e *chgEnv)
		want []string // stale marker
		kept []string // last good data still shown
		live string   // the other calls were still re-read
	}{
		{"changes", func(e *chgEnv) { e.pa.ChangesErr = &playgroundapi.Error{Code: playgroundapi.CodeTimeout} },
			[]string{"stale: worktree read failed (timed out)"}, []string{"3 files / +44 -9", "usage_service.go", "PR #67 / Draft", "1 failed: race-tests"}, "PullRequest"},
		{"pr", func(e *chgEnv) { e.pa.PRErr = &playgroundapi.Error{Code: playgroundapi.CodeRateLimited} },
			[]string{"stale: PR lookup failed (rate limited)"}, []string{"PR #67 / Draft", "3 files / +44 -9", "1 failed: race-tests"}, "Checks"},
		{"checks", func(e *chgEnv) { e.pa.ChecksErr = &playgroundapi.Error{Code: playgroundapi.CodeOffline} },
			[]string{"stale: CI lookup failed (offline)"}, []string{"2 checks passed", "PR #67 / Draft", "3 files / +44 -9"}, "Changes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newChgEnv(t, 120, 35)
			e.enter(t)
			before := map[string]int{"Changes": e.count("Changes"), "PullRequest": e.count("PullRequest"), "Checks": e.count("Checks")}
			tc.fail(e)
			e.key(t, "r")
			if e.count(tc.live) != before[tc.live]+1 {
				t.Fatalf("%s was not read again", tc.live)
			}
			mustContain(t, tc.name, view(e.m), append(tc.want, tc.kept...)...)
			// Another call's data is never marked stale by this failure.
			if n := strings.Count(view(e.m), "stale:"); n != 1 {
				t.Errorf("%d stale markers, want exactly 1:\n%s", n, view(e.m))
			}
		})
	}
	t.Run("diff", func(t *testing.T) {
		e := newChgEnv(t, 120, 35)
		e.enter(t)
		e.key(t, "enter")
		mustContain(t, "diff", view(e.m), "return lastGood.WithStale(err) h0")
		e.pa.DiffErr = &playgroundapi.Error{Code: playgroundapi.CodeTimeout}
		e.key(t, "r") // changes lands, the open diff is read again and fails
		mustContain(t, "diff stale", view(e.m), "stale: diff read failed (timed out)", "return lastGood.WithStale(err) h0", "3 files / +44 -9", "PR #67 / Draft")
	})
	t.Run("a failed first read says unavailable and invents nothing", func(t *testing.T) {
		e := newChgEnv(t, 120, 35)
		e.pa.PRErr = &playgroundapi.Error{Code: playgroundapi.CodeAuthRequired}
		e.pa.ChecksErr = &playgroundapi.Error{Code: playgroundapi.CodeNoGitHubRemote}
		e.enter(t)
		out := view(e.m)
		mustContain(t, "first failures", out, "PR unavailable: GitHub sign-in required", "CI unavailable: no GitHub remote", "3 files / +44 -9")
		if strings.Contains(out, "No pull request") || strings.Contains(out, "passed") {
			t.Errorf("a failed read claims a verified result:\n%s", out)
		}
	})
}

func TestPlaygroundChangesDropsLateOtherSessionAndOlderReplies(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.enter(t)
	c := e.m.pg.chg
	deliver := func(msg tea.Msg) {
		t.Helper()
		var cmd tea.Cmd
		e.m, cmd = send(t, e.m, msg)
		e.m = pgDrain(t, e.m, cmd)
	}
	other := chgView()
	other.Files = other.Files[:1]
	other.Totals = playgroundapi.Totals{Files: 1, Additions: 1}
	stamp := func(gen uint64) playgroundapi.Stamp {
		return playgroundapi.Stamp{Scope: pgfake.Scope, Generation: gen, FetchedAt: c.changes.at.Add(time.Hour)}
	}
	// Another session's reply.
	o := other
	o.SessionID, o.Stamp = "s2", stamp(c.changes.gate.Last().Generation+5)
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: c.changes.gen, v: o})
	// Newest request but an older backend generation.
	o = other
	o.SessionID, o.Stamp = "s1", stamp(c.changes.gate.Last().Generation)
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: c.changes.gen, v: o})
	// A reply of an older request.
	o.Stamp = stamp(c.changes.gate.Last().Generation + 9)
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: c.changes.gen - 1, v: o})
	// Another scope.
	o.Stamp = playgroundapi.Stamp{Scope: "someone-else", Generation: 99, FetchedAt: o.Stamp.FetchedAt}
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: c.changes.gen, v: o})
	if got := len(e.m.pg.chg.changes.val.Files); got != 3 || !strings.Contains(view(e.m), "3 files / +44 -9") {
		t.Fatalf("a dropped reply changed the data: %d files\n%s", got, view(e.m))
	}
	// The same for the pull request and the checks.
	pr := playgroundapi.PRView{SessionID: "s2", State: playgroundapi.PRNone, Stamp: stamp(50)}
	deliver(pgReadMsg[playgroundapi.PRView]{key: "s1", gen: e.m.pg.chg.pr.gen, v: pr})
	ck := playgroundapi.ChecksView{SessionID: "s1", State: playgroundapi.ChecksNone, Stamp: stamp(1)}
	deliver(pgReadMsg[playgroundapi.ChecksView]{key: "s1", gen: e.m.pg.chg.checks.gen, v: ck})
	mustContain(t, "kept", view(e.m), "PR #67 / Draft", "2 checks passed")
	// A late success of the newest request still lands.
	o = other
	o.SessionID, o.Stamp = "s1", stamp(c.changes.gate.Last().Generation+20)
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: e.m.pg.chg.changes.gen, v: o})
	if len(e.m.pg.chg.changes.val.Files) != 1 {
		t.Fatal("a valid newer reply was dropped")
	}
	// A reply that arrives after the selection moved to another session is ignored.
	e.key(t, "esc", "down")
	if e.m.pg.sel != "s2" {
		t.Fatalf("sel = %q", e.m.pg.sel)
	}
	late := chgView()
	late.SessionID, late.Stamp = "s1", stamp(500)
	deliver(pgReadMsg[playgroundapi.ChangesView]{key: "s1", gen: e.m.pg.chg.changes.gen, v: late})
	if len(e.m.pg.chg.changes.val.Files) != 1 {
		t.Fatal("a reply of a session that is no longer selected was applied")
	}
}

func TestPlaygroundChangesDistinguishesVerifiedAbsenceFromFailure(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.pa.PRBySession["s1"] = playgroundapi.PRView{State: playgroundapi.PRNone}
	e.pa.ChecksBySession["s1"] = playgroundapi.ChecksView{State: playgroundapi.ChecksNone}
	e.enter(t)
	mustContain(t, "verified none", view(e.m), "No pull request (verified)", "No checks reported for this commit (verified)")

	e = newChgEnv(t, 120, 35)
	e.pa.PRBySession["s1"] = playgroundapi.PRView{State: playgroundapi.PRUnavailable, Code: playgroundapi.CodeRateLimited}
	e.pa.ChecksBySession["s1"] = playgroundapi.ChecksView{State: playgroundapi.ChecksUnavailable, Code: playgroundapi.CodeOffline}
	e.enter(t)
	out := view(e.m)
	mustContain(t, "unavailable", out, "PR unavailable: rate limited", "CI unavailable: offline")
	if strings.Contains(out, "verified") || strings.Contains(out, "passed") {
		t.Errorf("unavailable reads claim a verdict:\n%s", out)
	}

	// An unavailable reply after a good one keeps the good one and says so.
	e = newChgEnv(t, 120, 35)
	e.enter(t)
	e.pa.PRBySession["s1"] = playgroundapi.PRView{State: playgroundapi.PRUnavailable, Code: playgroundapi.CodeRateLimited}
	e.key(t, "r")
	mustContain(t, "kept", view(e.m), "PR #67 / Draft", "stale: PR lookup failed (rate limited)")
	// A later verified "none" does replace it: a success is a success.
	e.pa.PRBySession["s1"] = playgroundapi.PRView{State: playgroundapi.PRNone}
	e.key(t, "r")
	out = view(e.m)
	mustContain(t, "replaced", out, "No pull request (verified)")
	if strings.Contains(out, "stale:") || strings.Contains(out, "PR #67") {
		t.Errorf("a verified empty result did not replace the old PR:\n%s", out)
	}
}

func TestPlaygroundChangesPRAndChecksFactsAreIndependent(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	pv := prFound()
	pv.DetailErr = &playgroundapi.Error{Code: playgroundapi.CodeRateLimited}
	pv.PR.HeadMatchesLocal = false
	e.pa.PRBySession["s1"] = pv
	ck := checksMixed()
	ck.HeadSHA, ck.IsLocal, ck.LocalHead = "9999999aaaa", false, "abcdef1234567"
	e.pa.ChecksBySession["s1"] = ck
	e.enter(t)
	mustContain(t, "facts", view(e.m),
		"PR details unavailable (rate limited)", "PR head differs from local HEAD",
		"Checks are for an older commit (9999999)", "! 1 failed: race-tests")
}

func TestPlaygroundChangesWorktreeAndBaseStates(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*playgroundapi.ChangesView)
		want []string
		deny []string
	}{
		{"no base", func(v *playgroundapi.ChangesView) { v.Base = playgroundapi.Base{Source: playgroundapi.BaseNone} },
			[]string{"no base — showing uncommitted changes only"}, []string{"base:", "merge-base"}},
		{"worktree missing", func(v *playgroundapi.ChangesView) {
			*v = playgroundapi.ChangesView{State: playgroundapi.WorktreeMissing, Task: playgroundapi.TaskLink{Source: playgroundapi.TaskNone}}
		}, []string{"Worktree is missing (verified)", "(worktree missing)", "No files: the worktree is missing."}, []string{"could not be read"}},
		{"branch missing", func(v *playgroundapi.ChangesView) { v.State, v.Branch = playgroundapi.BranchMissing, "" },
			[]string{"Branch is missing (verified)", "(branch missing)"}, []string{"could not be read"}},
		{"detached", func(v *playgroundapi.ChangesView) { v.State, v.Branch = playgroundapi.WorktreeDetached, "" },
			[]string{"Detached HEAD: files and commits are read", "(detached HEAD)", "usage_service.go"}, nil},
		{"unavailable", func(v *playgroundapi.ChangesView) {
			*v = playgroundapi.ChangesView{State: playgroundapi.WorktreeUnavailable, Err: &playgroundapi.Error{Code: playgroundapi.CodeTimeout}}
		}, []string{"Worktree could not be read (timed out): nothing is known about it.", "(unknown)"}, []string{"verified", "No files: the worktree is missing"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newChgEnv(t, 120, 35)
			v := chgView()
			tc.mut(&v)
			e.pa.ChangesBySession["s1"] = v
			e.enter(t)
			out := view(e.m)
			mustContain(t, tc.name, out, tc.want...)
			for _, d := range tc.deny {
				if strings.Contains(out, d) {
					t.Errorf("%s: %q must not appear:\n%s", tc.name, d, out)
				}
			}
		})
	}
	t.Run("unavailable after a good read keeps the last good view", func(t *testing.T) {
		e := newChgEnv(t, 120, 35)
		e.enter(t)
		e.pa.ChangesBySession["s1"] = playgroundapi.ChangesView{State: playgroundapi.WorktreeUnavailable, Err: &playgroundapi.Error{Code: playgroundapi.CodeUnavailable}}
		e.key(t, "r")
		mustContain(t, "kept", view(e.m), "usage_service.go", "3 files / +44 -9", "feat/151-usage", "stale: worktree read failed (backend unavailable)")
	})
}

func TestPlaygroundChangesTruncatedListKeepsTotals(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	v := chgView()
	v.Files = v.Files[:2]
	v.FilesTrunc = true
	v.Totals = playgroundapi.Totals{Files: 700, Additions: 9001, Deletions: 123}
	e.pa.ChangesBySession["s1"] = v
	e.enter(t)
	out := view(e.m)
	mustContain(t, "truncated", out, "700 files / +9001 -123", "list truncated", "showing 2 of 700 files")
	if strings.Contains(out, "2 files /") {
		t.Errorf("totals were recomputed from the truncated list:\n%s", out)
	}
}

func TestPlaygroundChangesFileMarkersAndSelectionByPath(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	v := chgView()
	v.Files = append(v.Files,
		playgroundapi.FileChange{Path: "new.txt", Status: playgroundapi.FileAdded, Untracked: true, Uncommitted: true},
		playgroundapi.FileChange{Path: "logo.png", Status: playgroundapi.FileModified, Binary: true})
	v.Totals.Files = 5
	e.pa.ChangesBySession["s1"] = v
	e.enter(t)
	mustContain(t, "markers", view(e.m), "U ? ~", "B", "uncounted", "binary")
	e.key(t, "down", "down", "down")
	mustContain(t, "marker words", view(e.m), "uncommitted untracked uncounted")
	e.key(t, "up")
	if e.m.pg.chg.sel != "usage_test.go" {
		t.Fatalf("sel = %q", e.m.pg.chg.sel)
	}
	// A refresh that reorders the list keeps the cursor on the same path.
	v.Files = []playgroundapi.FileChange{v.Files[4], v.Files[3], v.Files[2], v.Files[1], v.Files[0]}
	e.pa.ChangesBySession["s1"] = v
	e.key(t, "r")
	if e.m.pg.chg.sel != "usage_test.go" || e.m.pg.chg.idx != 2 {
		t.Fatalf("selection moved: %q at %d", e.m.pg.chg.sel, e.m.pg.chg.idx)
	}
	// A file that left hands the cursor to the one that took its place.
	v.Files = append(v.Files[:2:2], v.Files[3:]...)
	v.Totals.Files = 4
	e.pa.ChangesBySession["s1"] = v
	e.key(t, "r")
	if e.m.pg.chg.sel != "usagepane.go" {
		t.Fatalf("neighbor = %q", e.m.pg.chg.sel)
	}
}

func TestPlaygroundChangesDiffNavigatesByHunkAndScrollIsBounded(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.enter(t)
	gen := e.m.pg.chg.changes.val.Stamp.Generation
	e.key(t, "enter")
	req := e.pa.DiffRequests[len(e.pa.DiffRequests)-1]
	if req.Path != "usage_service.go" || req.Base != "origin/main" || req.Generation != gen || req.SessionID != "s1" {
		t.Fatalf("diff request = %+v (changes generation %d)", req, gen)
	}
	out := view(e.m)
	mustContain(t, "diff", out, "hunk 1/3", "@@ -1,6 +1,7 @@ func f0()", "- return emptySnapshot h0", "+ return lastGood.WithStale(err) h0")
	e.key(t, "n")
	mustContain(t, "hunk 2", view(e.m), "hunk 2/3", "func f1()")
	e.key(t, "n", "n", "n")
	if e.m.pg.chg.hunk != 2 {
		t.Fatalf("hunk = %d, hunk jumps must stop at the last", e.m.pg.chg.hunk)
	}
	e.key(t, "p", "p", "p", "p")
	if e.m.pg.chg.hunk != 0 || e.m.pg.chg.scroll != 0 {
		t.Fatalf("hunk=%d scroll=%d after jumping back", e.m.pg.chg.hunk, e.m.pg.chg.scroll)
	}
	rows, _ := pgDiffFlatten(e.m.pg.chg.diff.val)
	for i := 0; i < len(rows)+10; i++ {
		e.key(t, "down")
	}
	maxTop := len(rows) - e.m.pgChgDiffRows()
	if got := e.m.pg.chg.scroll; got != maxTop {
		t.Fatalf("scroll = %d, want the bound %d", got, maxTop)
	}
	for i := 0; i < len(rows)+10; i++ {
		e.key(t, "up")
	}
	if e.m.pg.chg.scroll != 0 {
		t.Fatalf("scroll = %d after scrolling up", e.m.pg.chg.scroll)
	}
	// Esc leaves the diff for the files, then the screen for the list; an Esc never quits.
	e.key(t, "esc")
	if !e.m.pg.chg.open || e.m.pg.chg.diffOpen {
		t.Fatal("esc in the diff did not return to the files")
	}
	e.key(t, "esc")
	if e.m.pg.chg.open || e.m.workspace != wsPlayground {
		t.Fatal("esc in the files did not return to the sessions")
	}
}

func TestPlaygroundChangesDiffStates(t *testing.T) {
	cases := []struct {
		name string
		v    playgroundapi.FileDiffView
		want []string
	}{
		{"binary", playgroundapi.FileDiffView{Binary: true}, []string{"Binary file: the content changed but there is no text diff."}},
		{"empty", playgroundapi.FileDiffView{Empty: true}, []string{"No textual change"}},
		{"truncated lines", func() playgroundapi.FileDiffView {
			v := diffView()
			v.Truncated, v.TruncReason = true, playgroundapi.TruncLines
			return v
		}(), []string{"TRUNCATED at 5000 lines"}},
		{"truncated bytes", func() playgroundapi.FileDiffView {
			v := diffView()
			v.Truncated, v.TruncReason = true, playgroundapi.TruncBytes
			return v
		}(), []string{"TRUNCATED at 256 KiB"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newChgEnv(t, 120, 35)
			e.pa.DiffBySession["s1"]["usage_service.go"] = tc.v
			e.enter(t)
			e.key(t, "enter")
			mustContain(t, tc.name, view(e.m), tc.want...)
		})
	}
	t.Run("stale diff request", func(t *testing.T) {
		e := newChgEnv(t, 120, 35)
		e.pa.DiffErr = &playgroundapi.Error{Code: playgroundapi.CodeStale}
		e.enter(t)
		e.key(t, "enter")
		mustContain(t, "stale", view(e.m), "diff unavailable (the list changed since it was read)", "Press r to refresh")
	})
}

func TestPlaygroundChangesLayoutReplacesOrSplits(t *testing.T) {
	narrow := newChgEnv(t, 80, 24)
	narrow.enter(t)
	out := view(narrow.m)
	mustContain(t, "files screen", out, "usage_service.go", "usagepane.go")
	narrow.key(t, "enter")
	out = view(narrow.m)
	mustContain(t, "diff screen", out, "hunk 1/3", "return emptySnapshot")
	if strings.Contains(out, "usagepane.go") {
		t.Errorf("at 80 columns the diff must replace the file list:\n%s", out)
	}
	narrow.key(t, "esc")
	if strings.Contains(view(narrow.m), "return emptySnapshot") || !strings.Contains(view(narrow.m), "usagepane.go") {
		t.Error("esc did not return to the file list")
	}

	wide := newChgEnv(t, 120, 35)
	wide.enter(t)
	wide.key(t, "enter")
	out = view(wide.m)
	mustContain(t, "split", out, "usagepane.go", "usage_test.go", "return emptySnapshot", "hunk 1/3")
	// Readable lines: a long diff line is cut with an ellipsis, never wrapped.
	long := diffView()
	long.Hunks[0].Lines[1].Text = strings.Repeat("0123456789", 20)
	wide.pa.DiffBySession["s1"]["usage_service.go"] = long
	wide.key(t, "r")
	for _, l := range strings.Split(view(wide.m), "\n") {
		if strings.Contains(l, "0123456789") && !strings.Contains(l, "…") {
			t.Errorf("long line not truncated: %q", l)
		}
	}
}

func TestPlaygroundChangesReenterKeepsLastGoodUntilNewReadLands(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.enter(t)
	e.key(t, "esc")
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, keyOf("c"))
	if cmd == nil {
		t.Fatal("entering did not start a read")
	}
	out := view(e.m)
	mustContain(t, "before the reply", out, "usage_service.go", "3 files / +44 -9", "synced 12s ago")
	if !e.m.pg.chg.changes.busy {
		t.Error("the read is not marked in flight")
	}
	e.m = pgDrain(t, e.m, cmd)
	if e.m.pg.chg.changes.busy {
		t.Error("still in flight after the reply")
	}
}

func TestPlaygroundChangesDoNotTouchTerminalOwnership(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	st := e.stream(t, "s1")
	attaches := len(e.fb.Sessions.Attaches())
	e.enter(t)
	e.key(t, "down", "enter", "n", "esc", "r", "esc")
	if len(st.Inputs()) != 0 || len(st.Resizes()) != 0 || st.Takes() != 0 || st.Writer() {
		t.Fatalf("Changes touched the PTY: inputs=%d resizes=%v takes=%d", len(st.Inputs()), st.Resizes(), st.Takes())
	}
	if got := len(e.fb.Sessions.Attaches()); got != attaches || st.Closed() {
		t.Fatalf("attachments changed: %d -> %d, closed=%v", attaches, got, st.Closed())
	}
	// While the agent has the keyboard, c is typed into it and Changes stays closed.
	e.key(t, "enter")
	st = e.stream(t, "s1")
	e.frames(t, st, status("running"))
	if !e.m.pgTerminalMode() {
		t.Fatal("not in control")
	}
	e.press(t, runeKey("c"))
	e.pump(t)
	if e.m.pg.chg.open {
		t.Fatal("Changes opened from Control")
	}
	if got := len(st.Inputs()); got != 1 || string(st.Inputs()[0]) != "c" {
		t.Fatalf("the key did not reach the agent: %q", st.Inputs())
	}
}

// --- linking ---

func (e *chgEnv) startLink(t *testing.T, number string) {
	t.Helper()
	e.key(t, "L")
	if e.m.modal == nil {
		t.Fatalf("L did not ask for the Issue:\n%s", view(e.m))
	}
	for _, r := range number {
		e.key(t, string(r))
	}
	e.key(t, "enter")
}

func (e *chgEnv) answerYes(t *testing.T) {
	t.Helper()
	e.key(t, "up", "enter")
}

func TestPlaygroundLinkFlowAsksAndConfirmsBeforeAnyWrite(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.m.pg.sel = "s2" // select the manual session
	e.m.pg.selIdx = 1
	e.enter(t)
	e.key(t, "L")
	mustContain(t, "ask", view(e.m), "Link to Issue")
	e.key(t, "q") // typed into the field: it neither quits nor leaves the dialog
	if e.m.modal == nil {
		t.Fatal("q left the dialog")
	}
	e.key(t, "esc")
	e.startLink(t, "x1") // not a number: asked again, nothing confirmed
	mustContain(t, "again", view(e.m), "enter a positive whole number")
	e.key(t, "esc")
	e.startLink(t, "151")
	out := view(e.m)
	mustContain(t, "confirm", out, "Link this session to Issue #151?", "Session: manual run", "Issue: #151 Broken usage refresh",
		"Links this session to the Issue for display only. It creates no plan, approval or attempt.")
	if len(e.pa.LinkRequests) != 0 {
		t.Fatal("LinkSession ran before the confirmation")
	}
	e.key(t, "enter") // default answer is No
	if len(e.pa.LinkRequests) != 0 || e.m.modal != nil {
		t.Fatalf("declining wrote or left the dialog open: %+v", e.pa.LinkRequests)
	}
	// Yes: one write, then the changes are read again and the rows use the link.
	changed := chgView()
	changed.Task = playgroundapi.TaskLink{Source: playgroundapi.TaskManual, IssueNumber: 151, IssueID: 7}
	e.pa.ChangesBySession["s2"] = changed
	reads := e.count("Changes")
	e.startLink(t, "151")
	e.answerYes(t)
	if len(e.pa.LinkRequests) != 1 || e.pa.LinkRequests[0] != (playgroundapi.LinkRequest{SessionID: "s2", IssueNumber: 151}) {
		t.Fatalf("link requests = %+v", e.pa.LinkRequests)
	}
	if e.count("Changes") != reads+1 || e.count("Links") != 2 {
		t.Fatalf("expected a re-read of changes and links: %v", e.pa.Calls)
	}
	mustContain(t, "linked", view(e.m), "Linked to Issue #151 (display only", "Task: Issue #151, linked manually. No approved plan.")
	if strings.Contains(view(e.m), "L links this session") {
		t.Error("still offers linking")
	}
	e.key(t, "esc")
	rowText := view(e.m)
	mustContain(t, "row", rowText, "#151 Broken usage refresh (linked manually)", "Linked manually: no approved plan")
	if strings.Contains(rowText, "approved plan is on the Issue") && e.m.pg.sel == "s2" {
		t.Errorf("a manual link claims an approved plan:\n%s", rowText)
	}
}

func TestPlaygroundLinkErrorCodesHaveFixedWording(t *testing.T) {
	tests := []struct {
		code playgroundapi.Code
		want string
	}{
		{playgroundapi.CodeHasTask, "This session already has a task from a launch or a link. Nothing was linked."},
		{playgroundapi.CodeGardenUnavailable, "Garden is not available, so the Issue could not be checked. Nothing was linked."},
		{playgroundapi.CodeIssueNotFound, "Issue #151 was not found in this Garden. Nothing was linked."},
		{playgroundapi.CodeStore, "The link store is unavailable. Nothing was linked."},
		{playgroundapi.CodeTimeout, "The link could not be saved (timed out). Nothing was linked."},
	}
	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			e := newChgEnv(t, 120, 35)
			e.m.pg.sel, e.m.pg.selIdx = "s2", 1
			e.enter(t)
			e.pa.LinkErr = &playgroundapi.Error{Code: tc.code, Message: "RAW BACKEND TEXT"}
			e.startLink(t, "151")
			e.answerYes(t)
			out := view(e.m)
			mustContain(t, "wording", out, tc.want)
			if strings.Contains(out, "RAW BACKEND TEXT") {
				t.Error("backend message shown to the user")
			}
			if len(e.pa.LinkRequests) != 1 || e.pa.LinkRequests[0].Replace || e.m.modal != nil {
				t.Fatalf("requests=%+v modal=%v", e.pa.LinkRequests, e.m.modal != nil)
			}
			if len(e.pa.LinksList) != 0 {
				t.Error("a failed link left a record")
			}
		})
	}
}

func TestPlaygroundLinkConflictNeedsAnExplicitReplace(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.m.pg.sel, e.m.pg.selIdx = "s2", 1
	e.pa.LinksList = []playgroundapi.Link{{SessionID: "s2", IssueNumber: 174, IssueID: 9, Origin: "manual"}}
	cmd := e.m.pgLinksRead()
	e.m = pgDrain(t, e.m, cmd)
	e.enter(t)
	e.startLink(t, "151")
	e.answerYes(t)
	out := view(e.m)
	mustContain(t, "replace", out, "Replace the link with Issue #151?", "already linked to Issue #174", "Confirming replaces that link.",
		"for display only. It creates no plan, approval or attempt.")
	if len(e.pa.LinkRequests) != 1 {
		t.Fatalf("requests = %+v", e.pa.LinkRequests)
	}
	e.key(t, "esc") // declining the replace writes nothing
	if len(e.pa.LinkRequests) != 1 || e.pa.LinksList[0].IssueNumber != 174 {
		t.Fatalf("declining replaced the link: %+v %+v", e.pa.LinkRequests, e.pa.LinksList)
	}
	e.startLink(t, "151")
	e.answerYes(t)
	e.answerYes(t)
	if len(e.pa.LinkRequests) != 3 || !e.pa.LinkRequests[2].Replace || e.pa.LinksList[0].IssueNumber != 151 {
		t.Fatalf("replace not sent: %+v %+v", e.pa.LinkRequests, e.pa.LinksList)
	}
}

func TestPlaygroundLinkNotOfferedWhenSessionHasATask(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	v := chgView()
	v.Task = playgroundapi.TaskLink{Source: playgroundapi.TaskLaunch, IssueNumber: 151, AttemptID: "att1"}
	e.pa.ChangesBySession["s1"] = v
	e.enter(t)
	mustContain(t, "launch", view(e.m), "Task: Issue #151, launched attempt (the approved plan is on the Issue).")
	e.key(t, "L")
	if e.m.modal != nil || len(e.pa.LinkRequests) != 0 {
		t.Fatal("a session with a task can be linked")
	}
	for _, h := range e.m.pgChangesHints() {
		if h.key == "L" {
			t.Error("hint offers L")
		}
	}
}

func TestPlaygroundRowsUseManualLinksAndNeverClaimAPlan(t *testing.T) {
	e := newChgEnv(t, 120, 35)
	e.pa.LinksList = []playgroundapi.Link{{SessionID: "s2", IssueNumber: 151, IssueID: 7, Origin: "manual"}}
	cmd := e.m.pgLinksRead()
	e.m = pgDrain(t, e.m, cmd)
	rows := map[string]pgRow{}
	for _, r := range e.m.playgroundRows() {
		rows[r.ID] = r
	}
	if r := rows["s2"]; !r.Manual || r.Title != "#151 Broken usage refresh (linked manually)" {
		t.Fatalf("s2 = %+v", r)
	}
	if r := rows["s1"]; r.Manual || r.Title != "#151 Broken usage refresh" {
		t.Fatalf("a launch link was changed: %+v", r)
	}
	e.m.pg.sel = "s2"
	mustContain(t, "manual detail", view(e.m), "Linked manually: no approved plan")
	if strings.Contains(view(e.m), "Plan: approved plan is on the Issue") {
		t.Error("manual link shows the approved plan line")
	}
	e.m.pg.sel = "s1"
	mustContain(t, "launch detail", view(e.m), "Plan: approved plan is on the Issue", "Attempt: att1")
	// A failed read keeps the last good links and says so.
	e.pa.LinksErr = &playgroundapi.Error{Code: playgroundapi.CodeStore}
	cmd = e.m.pgLinksRead()
	e.m = pgDrain(t, e.m, cmd)
	if !e.m.pg.links.stale() || !rowsKeepManual(e.m) {
		t.Fatal("a failed links read dropped the last good links")
	}
	mustContain(t, "stale links", view(e.m), "stale: manual links could not be read (link store unavailable)")
	// A session without a link is a neutral fact.
	e.pa.LinksErr = nil
	e.m.pg.sel = "s1"
	e.m.pg.recs[0].Session.Task = nil
	mustContain(t, "unlinked", view(e.m), "No Garden task (a neutral fact, not a failure)")
}

func rowsKeepManual(m Model) bool {
	for _, r := range m.playgroundRows() {
		if r.ID == "s2" {
			return r.Manual
		}
	}
	return false
}

func TestPlaygroundChangesRenderBounds(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		w, h := size[0], size[1]
		for _, ascii := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/ascii=%v", w, h, ascii), func(t *testing.T) {
				e := newChgEnv(t, w, h)
				if ascii {
					e.m.work.AgentGlyphs = "ascii"
				}
				v := chgView()
				v.Base.Note = "Base fell back to the repository default branch."
				for i := 0; i < 40; i++ {
					v.Files = append(v.Files, playgroundapi.FileChange{Path: fmt.Sprintf("internal/very/deep/package/path/file_with_a_long_name_%02d.go", i), Status: playgroundapi.FileModified, Additions: i, Deletions: 1, Counted: true, Uncommitted: true, Untracked: i%3 == 0})
				}
				v.Totals.Files = 43
				v.FilesTrunc = true
				e.pa.ChangesBySession["s1"] = v
				pv := prFound()
				pv.DetailErr = &playgroundapi.Error{Code: playgroundapi.CodeRateLimited}
				pv.PR.HeadMatchesLocal = false
				e.pa.PRBySession["s1"] = pv
				ck := checksMixed()
				ck.IsLocal, ck.HeadSHA = false, "9999999"
				ck.Truncated = true
				e.pa.ChecksBySession["s1"] = ck
				long := diffView()
				long.Hunks[0].Lines[2].Text = strings.Repeat("wide 界 line ", 40)
				long.Truncated, long.TruncReason = true, playgroundapi.TruncLines
				e.pa.DiffBySession["s1"]["usage_service.go"] = long
				check := func(label string) {
					t.Helper()
					lines := strings.Split(e.m.View(), "\n")
					if len(lines) != h {
						t.Errorf("%s: %d lines, want %d", label, len(lines), h)
					}
					for i, l := range lines {
						if got := ansi.StringWidth(l); got > w {
							t.Errorf("%s: line %d is %d cells > %d: %q", label, i, got, w, ansi.Strip(l))
						}
					}
					if ascii {
						for _, r := range ansi.Strip(e.m.View()) {
							if r > 0x7e && r != '界' {
								t.Errorf("%s: non-ASCII glyph %q in ASCII mode", label, r)
								break
							}
						}
					}
				}
				e.enter(t)
				check("files")
				e.pa.PRErr = &playgroundapi.Error{Code: playgroundapi.CodeRateLimited}
				e.pa.ChecksErr = &playgroundapi.Error{Code: playgroundapi.CodeOffline}
				e.pa.ChangesErr = &playgroundapi.Error{Code: playgroundapi.CodeTimeout}
				e.key(t, "r")
				check("stale")
				for i := 0; i < 45; i++ {
					e.key(t, "down")
				}
				check("scrolled files")
				if !strings.Contains(view(e.m), "file_with_a_long_name_39") && !strings.Contains(view(e.m), "file_with_a_long_n") {
					t.Error("the selected file is not on screen")
				}
				e.pa.ChangesErr = nil
				e.pa.PRErr, e.pa.ChecksErr = nil, nil
				e.key(t, "r")
				e.key(t, "enter")
				check("diff")
				for i := 0; i < 12; i++ {
					e.key(t, "down")
					check("diff scroll")
				}
				e.key(t, "n", "n", "p")
				check("hunks")
				e.key(t, "esc", "esc")
				check("list")
				if e.m.shellFocus = shellNav; true {
					check("nav")
				}
			})
		}
	}
}
