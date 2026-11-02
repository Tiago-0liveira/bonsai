package git

import (
	"fmt"
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
				runGit(t, dir, "remote", "add", "origin", "https://example.invalid/repo")
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
	for _, tc := range []struct{ start, want string }{
		{"", "main"}, {"HEAD", "main"}, {"main", "main"},
		{"refs/heads/main", "main"}, {"origin/parent", "parent"},
		{"refs/remotes/upstream/team/parent", "team/parent"},
		{"HEAD~1", ""}, {"tag", ""}, {"sha", ""},
	} {
		t.Run("base="+tc.start, func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			runGit(t, dir, "commit", "--allow-empty", "-m", "second")
			for _, remote := range []string{"origin", "upstream"} {
				runGit(t, dir, "remote", "add", remote, "https://example.invalid/repo")
			}
			runGit(t, dir, "update-ref", "refs/remotes/origin/parent", "HEAD")
			runGit(t, dir, "update-ref", "refs/remotes/upstream/team/parent", "HEAD")
			runGit(t, dir, "tag", "tag")
			start := tc.start
			if start == "sha" {
				start, _ = BranchTip(dir, "main")
			}
			if err := AddWorktreeNewBranch(dir, filepath.Join(t.TempDir(), "child"), "child", start); err != nil {
				t.Fatal(err)
			}
			got, err := run(dir, "config", "--local", "--get", "branch.child.bonsaiBase")
			if got != tc.want || (tc.want != "" && err != nil) {
				t.Fatalf("recorded %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBaseMetricsRefSafety(t *testing.T) {
	for _, tc := range []struct {
		name, base, want, source string
		remote, tag              bool
	}{
		{"deleted branch with tag", "parent", "main", "default", false, true},
		{"remote beats tag", "parent", "parent", "pr", true, true},
		{"local beats tag", "child", "child", "pr", false, true},
		{"revision expression", "child~1", "main", "default", false, false},
		{"option", "--all", "main", "default", false, false},
		{"option-like branch", "-parent", "-parent", "pr", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			runGit(t, dir, "checkout", "-b", "parent")
			runGit(t, dir, "commit", "--allow-empty", "-m", "parent")
			runGit(t, dir, "checkout", "-b", "child")
			runGit(t, dir, "commit", "--allow-empty", "-m", "child")
			if tc.tag {
				runGit(t, dir, "tag", tc.base, "main")
			}
			if tc.remote {
				runGit(t, dir, "remote", "add", "upstream", "https://example.invalid/repo")
				runGit(t, dir, "update-ref", "refs/remotes/upstream/parent", "refs/heads/parent")
			}
			if tc.base == "-parent" {
				runGit(t, dir, "update-ref", "refs/heads/-parent", "refs/heads/parent")
			}
			runGit(t, dir, "branch", "-D", "parent")
			got, err := BaseMetrics(dir, "child", "main", tc.base)
			wantAhead := 2
			if tc.want == "parent" || tc.want == "-parent" {
				wantAhead = 1
			}
			if tc.want == "child" {
				wantAhead = 0
			}
			if err != nil || got.Base != tc.want || got.BaseSource != tc.source || got.Ahead != wantAhead {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestBaseNormalization(t *testing.T) {
	for _, prefix := range []string{"", "refs/heads/", "upstream/", "refs/remotes/upstream/"} {
		for _, source := range []string{"pr", "recorded", "guess"} {
			t.Run(prefix+source, func(t *testing.T) {
				dir := isolatedBaseRepo(t)
				runGit(t, dir, "remote", "add", "upstream", "https://example.invalid/repo")
				runGit(t, dir, "branch", "team/parent")
				runGit(t, dir, "update-ref", "refs/remotes/upstream/team/parent", "main")
				runGit(t, dir, "checkout", "-b", "child")
				runGit(t, dir, "commit", "--allow-empty", "-m", "child")
				pr := ""
				if source == "pr" {
					pr = prefix + "team/parent"
				}
				if source == "recorded" {
					runGit(t, dir, "config", "--local", "branch.child.bonsaiBase", prefix+"team/parent")
				}
				got, err := BaseMetrics(dir, "child", prefix+"team/parent", pr)
				if err != nil || got.Base != "team/parent" || got.BaseSource != source {
					t.Fatalf("got %+v, %v", got, err)
				}
			})
		}
	}
}

func TestBaseGuessCrissCross(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			dir := isolatedBaseRepo(t)
			root, _ := BranchTip(dir, "main")
			tree, _ := run(dir, "rev-parse", "HEAD^{tree}")
			commit := func(message string, parents ...string) string {
				args := []string{"commit-tree", tree, "-m", message}
				for _, parent := range parents {
					args = append(args, "-p", parent)
				}
				sha, err := run(dir, args...)
				if err != nil {
					t.Fatal(err)
				}
				return sha
			}
			a := commit("a2", commit("a1", root))
			b := commit("b2", commit("b1", root))
			if reverse {
				a, b = b, a
			}
			x := commit("merge x", a, b)
			y := commit("merge y", b, a)
			for branch, sha := range map[string]string{"a": a, "b": b, "parent": y, "child": commit("child", x)} {
				runGit(t, dir, "update-ref", "refs/heads/"+branch, sha)
			}
			got, err := ResolveBase(dir, "child", "a", "")
			if err != nil || got.Name != "parent" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestBaseGuessEvictsOneEntry(t *testing.T) {
	dir := isolatedBaseRepo(t)
	runGit(t, dir, "checkout", "-b", "child")
	runGit(t, dir, "commit", "--allow-empty", "-m", "child")
	baseGuesses.Lock()
	old := baseGuesses.entries
	baseGuesses.entries = map[string]baseGuess{}
	for i := range 256 {
		baseGuesses.entries[fmt.Sprint(i)] = baseGuess{used: uint64(i)}
	}
	baseGuesses.Unlock()
	t.Cleanup(func() { baseGuesses.Lock(); baseGuesses.entries = old; baseGuesses.Unlock() })
	if _, err := ResolveBase(dir, "child", "main", ""); err != nil {
		t.Fatal(err)
	}
	baseGuesses.Lock()
	defer baseGuesses.Unlock()
	if len(baseGuesses.entries) != 256 {
		t.Fatalf("entries = %d", len(baseGuesses.entries))
	}
	if _, ok := baseGuesses.entries["0"]; ok {
		t.Fatal("oldest entry retained")
	}
	if _, ok := baseGuesses.entries["255"]; !ok {
		t.Fatal("recent entry evicted")
	}
}
