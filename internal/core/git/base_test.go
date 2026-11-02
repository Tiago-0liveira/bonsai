package git

import (
	"path/filepath"
	"testing"
)

func isolatedBaseRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "missing"))
	return gitInit(t)
}

func TestResolveBase(t *testing.T) {
	for _, tc := range []struct {
		name, pr, recorded, want, source string
		tie, contained                   bool
	}{
		{name: "stack", want: "feat/parent", source: "guess"},
		{name: "PR wins", pr: "main", recorded: "feat/parent", want: "main", source: "pr"},
		{name: "recorded wins", recorded: "main", want: "main", source: "recorded"},
		{name: "default wins tie", want: "main", source: "guess", tie: true},
		{name: "containing branches excluded", want: "main", source: "default", contained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			runGit(t, dir, "checkout", "-b", "feat/parent")
			if !tc.tie {
				runGit(t, dir, "commit", "--allow-empty", "-m", "parent")
			}
			runGit(t, dir, "checkout", "-b", "child")
			runGit(t, dir, "commit", "--allow-empty", "-m", "child")
			runGit(t, dir, "branch", "descendant")
			if tc.contained {
				runGit(t, dir, "branch", "-f", "main", "child")
				runGit(t, dir, "branch", "-f", "feat/parent", "child")
			}
			if tc.recorded != "" {
				runGit(t, dir, "config", "--local", "branch.child.bonsaiBase", tc.recorded)
			}
			for range 2 {
				got, err := ResolveBase(dir, "child", "main", tc.pr)
				if err != nil || got.Name != tc.want || got.Source != tc.source {
					t.Fatalf("got %+v, %v; want %s/%s", got, err, tc.want, tc.source)
				}
			}
		})
	}
}

func TestBaseMetrics(t *testing.T) {
	for _, tc := range []struct {
		name, pr, want, source string
		remote                 bool
		ahead, behind          int
	}{
		{"local", "feat/parent", "feat/parent", "pr", false, 1, 1},
		{"remote only", "feat/parent", "feat/parent", "pr", true, 1, 1},
		{"missing base", "gone", "main", "default", false, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			runGit(t, dir, "checkout", "-b", "feat/parent")
			runGit(t, dir, "commit", "--allow-empty", "-m", "parent")
			runGit(t, dir, "checkout", "-b", "child")
			runGit(t, dir, "commit", "--allow-empty", "-m", "child")
			runGit(t, dir, "checkout", "feat/parent")
			runGit(t, dir, "commit", "--allow-empty", "-m", "base moved")
			runGit(t, dir, "checkout", "child")
			if tc.remote {
				runGit(t, dir, "update-ref", "refs/remotes/origin/feat/parent", "feat/parent")
				runGit(t, dir, "branch", "-D", "feat/parent")
			}
			got, err := BaseMetrics(dir, "child", "main", tc.pr)
			if err != nil || got.Base != tc.want || got.BaseSource != tc.source || got.Ahead != tc.ahead || got.Behind != tc.behind {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestBaseGuessInvalidation(t *testing.T) {
	dir := isolatedBaseRepo(t)
	runGit(t, dir, "checkout", "-b", "parent")
	runGit(t, dir, "commit", "--allow-empty", "-m", "parent")
	runGit(t, dir, "checkout", "-b", "child")
	runGit(t, dir, "commit", "--allow-empty", "-m", "child")
	first, err := ResolveBase(dir, "child", "main", "")
	if err != nil || first.Name != "parent" {
		t.Fatalf("first %+v: %v", first, err)
	}
	runGit(t, dir, "branch", "-f", "parent", "main")
	second, err := ResolveBase(dir, "child", "main", "")
	if err != nil || second.Name != "main" {
		t.Fatalf("moved candidate %+v: %v", second, err)
	}
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "branch", "-f", "child", "main")
	third, err := ResolveBase(dir, "child", "main", "")
	if err != nil || third.Source != "default" {
		t.Fatalf("moved HEAD %+v: %v", third, err)
	}
}

func TestCreateRecordsBase(t *testing.T) {
	for _, base := range []string{"", "HEAD", "main"} {
		t.Run("base="+base, func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			if err := AddWorktreeNewBranch(dir, filepath.Join(t.TempDir(), "child"), "child", base); err != nil {
				t.Fatal(err)
			}
			got, err := ResolveBase(dir, "child", "main", "")
			if err != nil || got.Name != "main" || got.Source != "recorded" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}
