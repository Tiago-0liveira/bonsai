package claude

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const testPlanFile = "/home/u/.bonsai/garden/plans/local-1/7/plan-a/plan.md"

// withoutSessionID drops the random --session-id pair so argv can be compared exactly.
func withoutSessionID(t *testing.T, args []string) []string {
	t.Helper()
	i := slices.Index(args, "--session-id")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("no --session-id in %v", args)
	}
	return slices.Delete(slices.Clone(args), i, i+2)
}

func TestReadOnlyArgsWithoutPlanFileAreUnchanged(t *testing.T) {
	args, _, err := launchArgs(Settings{}, nil, agents.LaunchOptions{ReadOnly: true, Prompt: "plan it"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--restricted", "--tools", "Read,Grep,Glob", "--strict-mcp-config", "--permission-mode", "plan", "--", "plan it"}
	if got := withoutSessionID(t, args); !slices.Equal(got, want) {
		t.Fatalf("args = %q\nwant %q", got, want)
	}
}

func TestPlanFileLaunchArgsAndSettingsExactly(t *testing.T) {
	hooks := &agents.ActivityRequest{HookCommand: []string{"/bin/bonsai", "__agent-hook", "claude"}, SpoolEnv: "SPOOL", SpoolPath: "/run/x/activity.jsonl"}
	policy := `"permissions":{"allow":["Edit(//home/u/.bonsai/garden/plans/local-1/7/plan-a/plan.md)"],"deny":["Bash","WebFetch","WebSearch"]}`
	cases := []struct {
		name     string
		activity *agents.ActivityRequest
		settings func(t *testing.T) string
	}{
		{"permissions only", nil, func(*testing.T) string { return `{` + policy + `}` }},
		{"merged with activity hooks", hooks, func(t *testing.T) string {
			h, err := json.Marshal(activityHooks(hooks))
			if err != nil {
				t.Fatal(err)
			}
			return `{"hooks":` + string(h) + `,` + policy + `}`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, _, err := launchArgs(Settings{PermissionMode: "bypassPermissions"}, nil,
				agents.LaunchOptions{ReadOnly: true, PlanFile: testPlanFile, Prompt: "plan it"}, tc.activity)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"--restricted", "--tools", "Read,Grep,Glob,Write", "--strict-mcp-config", "--permission-mode", "dontAsk", "--setting-sources", "",
				"--add-dir", "/home/u/.bonsai/garden/plans/local-1/7/plan-a", "--settings", tc.settings(t), "--", "plan it"}
			if got := withoutSessionID(t, args); !slices.Equal(got, want) {
				t.Fatalf("args = %q\nwant %q", got, want)
			}
			if n := countOf(args, "--settings"); n != 1 {
				t.Fatalf("--settings passed %d times, want exactly one", n)
			}
			for _, bad := range []string{"bypassPermissions", "--dangerously-skip-permissions", "--allowedTools", "Edit", "Bash"} {
				if slices.Contains(args, bad) {
					t.Fatalf("args carry %s", bad)
				}
			}
		})
	}
}

func countOf(args []string, flag string) int {
	n := 0
	for _, a := range args {
		if a == flag {
			n++
		}
	}
	return n
}

func TestPlanFileRefusesPathsThatWidenTheRule(t *testing.T) {
	bad := []string{
		"plan.md", "/tmp/plan.md/", "/tmp/../etc/plan.md", "/tmp/a/other.md", "/tmp/a/plan.md.bak", "/tmp/*/plan.md", "/tmp/a?/plan.md",
		"/tmp/[ab]/plan.md", "/tmp/{a,b}/plan.md", "/tmp/(x)/plan.md", "/tmp/!x/plan.md", `/tmp/a\b/plan.md`, "/tmp/a\nb/plan.md", "/tmp//a/plan.md",
		"/tmp/a b\"/plan.md", `C:\tmp\plan.md`, "//server/share/plan.md",
	}
	for _, p := range bad {
		if _, _, err := launchArgs(Settings{}, nil, agents.LaunchOptions{ReadOnly: true, PlanFile: p}, nil); !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Errorf("launchArgs(%q) err = %v, want ErrInvalidLaunch", p, err)
		}
	}
	e := newTestEnv(t)
	account := e.addLogin("planner")
	if err := e.provider.ValidateLaunch(account, agents.LaunchOptions{PlanFile: testPlanFile}); err == nil {
		t.Fatal("a plan file without read-only was accepted")
	}
	if err := e.provider.ValidateLaunch(account, agents.LaunchOptions{ReadOnly: true, PlanFile: testPlanFile}); err != nil {
		t.Fatalf("valid plan file refused: %v", err)
	}
	if _, err := e.provider.ResolveLaunch(account, agents.LaunchOptions{PlanFile: testPlanFile}); err == nil {
		t.Fatal("ResolveLaunch accepted a plan file")
	}
}

func TestPlanFileVerdictAndPrepare(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("planner")
	if s := agents.ReadOnlyOf(e.provider); !s.Supported || !s.PlanFile {
		t.Fatalf("verdict = %+v", s)
	}
	prepared, _ := e.prepare(account, agents.LaunchOptions{ReadOnly: true, PlanFile: testPlanFile, Prompt: "p"})
	if !containsRun(prepared.Args, []string{"--tools", "Read,Grep,Glob,Write"}) || !containsRun(prepared.Args, []string{"--permission-mode", "dontAsk"}) {
		t.Fatalf("args = %v", prepared.Args)
	}
}

func TestPlanFileRefusesExplicitArgsThatWidenIt(t *testing.T) {
	for _, flag := range []string{"--permission-mode", "--dangerously-skip-permissions", "--settings", "--add-dir", "--tools", "--allowedTools", "--allowed-tools", "--setting-sources"} {
		_, _, err := launchArgs(Settings{}, []string{flag, "x"}, agents.LaunchOptions{ReadOnly: true, PlanFile: testPlanFile}, nil)
		if !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Errorf("%s: err = %v, want ErrInvalidLaunch", flag, err)
		}
	}
}

func TestPlanFileArgsIgnoreEverySettingsFileLayer(t *testing.T) {
	args, _, err := launchArgs(Settings{}, nil, agents.LaunchOptions{ReadOnly: true, PlanFile: testPlanFile}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRun(args, []string{"--restricted"}) || !containsRun(args, []string{"--setting-sources", ""}) {
		t.Fatalf("settings file layers are not switched off: %q", args)
	}
	i := slices.Index(args, "--settings")
	var parsed struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(args[i+1]), &parsed); err != nil || len(parsed.Permissions.Allow) != 1 {
		t.Fatalf("settings = %q, %v", args[i+1], err)
	}
}
