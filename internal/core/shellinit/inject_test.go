package shellinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := map[string]Shell{
		"/bin/bash": ShellBash, "/usr/bin/zsh": ShellZsh, "fish": ShellFish,
		"/bin/sh": ShellSh, "/bin/dash": ShellSh, "ksh": ShellSh, "mksh": ShellSh,
		"/usr/bin/nu": ShellNu, "pwsh": ShellPwsh, "powershell": ShellPwsh,
		`C:\Windows\System32\cmd.exe`: ShellCmd, `C:\Program Files\PowerShell\7\pwsh.EXE`: ShellPwsh,
		"/bin/tcsh": ShellUnknown, "": ShellUnknown,
	}
	for in, want := range tests {
		if got := Detect(in); got != want {
			t.Errorf("Detect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildPlan(t *testing.T) {
	d := filepath.Join("shim", "dir")
	env := []string{"ZDOTDIR=/real", "ENV=/real/env", "BONSAI_PROMPT_PREFIX=B1 $"}
	tests := []struct {
		shell string
		env   []string
		want  Plan
	}{
		{"/bin/bash", nil, Plan{Args: []string{"--rcfile", filepath.Join(d, "rcfile.bash")}}},
		{"zsh", env, Plan{Env: []string{"ZDOTDIR=" + filepath.Join(d, "zsh"), "BONSAI_ORIG_ZDOTDIR=/real"}}},
		{"zsh", nil, Plan{Env: []string{"ZDOTDIR=" + filepath.Join(d, "zsh"), "BONSAI_ORIG_ZDOTDIR="}}},
		{"fish", nil, Plan{Args: []string{"-C", "source '" + strings.ReplaceAll(filepath.Join(d, "hook.fish"), `\`, `\\`) + "'"}}},
		{"dash", env, Plan{Env: []string{"ENV=" + filepath.Join(d, "env.sh"), "BONSAI_ORIG_ENV=/real/env"}}},
		{"nu", nil, Plan{Args: []string{"-e", `source "` + strings.ReplaceAll(filepath.Join(d, "hook.nu"), `\`, `\\`) + `"`}}},
		{"pwsh", nil, Plan{Args: []string{"-NoExit", "-Command", ". '" + filepath.Join(d, "hook.ps1") + "'"}}},
		{"cmd.exe", env, Plan{Args: []string{"/K", "prompt B1 $$$P$G"}}},
		{"tcsh", env, Plan{}},
	}
	for _, tc := range tests {
		if got := BuildPlan(tc.shell, d, tc.env); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("BuildPlan(%q) = %#v, want %#v", tc.shell, got, tc.want)
		}
	}
}

// An apostrophe in the cache path must not end nu's string early: the path is
// double-quoted, with embedded double quotes escaped.
func TestBuildPlanNuQuoting(t *testing.T) {
	got := BuildPlan("nu", `/home/O'Connor/"x"`, nil)
	if len(got.Args) != 2 {
		t.Fatalf("args = %#v", got.Args)
	}
	arg := got.Args[1]
	if !strings.HasPrefix(arg, `source "`) || !strings.HasSuffix(arg, `hook.nu"`) ||
		!strings.Contains(arg, `O'Connor`) || !strings.Contains(arg, `\"x\"`) {
		t.Errorf("source arg = %q", arg)
	}
}

func TestEnsureShims(t *testing.T) {
	cache := t.TempDir()
	dir, err := EnsureShims(cache, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cache, "bonsai", "shell-integration", "v1.2.3"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	for _, f := range []string{"rcfile.bash", "hook.bash", "hook.zsh", "hook.fish", "env.sh", "hook.nu", "hook.ps1", "zsh/.zshenv", "zsh/.zshrc", "zsh/.zprofile"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	// Outdated content is replaced; matching content is left alone.
	hook := filepath.Join(dir, "hook.bash")
	if err := os.WriteFile(hook, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureShims(cache, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	want, _ := shimFS.ReadFile("shims/hook.bash")
	if got, _ := os.ReadFile(hook); string(got) != string(want) {
		t.Error("stale shim not rewritten")
	}
	if _, err := EnsureShims("", "x"); err == nil {
		t.Error("empty cache dir: want error")
	}
	if d, err := EnsureShims(cache, ""); err != nil || filepath.Base(d) != "dev" {
		t.Errorf("empty version = %q, %v; want dev", d, err)
	}
}

func TestSnippet(t *testing.T) {
	for _, sh := range SnippetShells {
		s, err := Snippet(sh)
		if err != nil || !strings.Contains(s, "__BONSAI_PROMPT_INSTALLED") {
			t.Errorf("Snippet(%q) = %q, %v", sh, s, err)
		}
	}
	if _, err := Snippet("tcsh"); err == nil || !strings.Contains(err.Error(), "bash, zsh, fish, nu, pwsh") {
		t.Errorf("unknown shell error = %v", err)
	}
}

// shellEnv is a hermetic environment: HOME and ZDOTDIR live under home.
func shellEnv(home string, extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TERM=dumb",
		"BONSAI_DEPTH=1", "BONSAI_PROMPT_PREFIX=🪴1 "}
	return append(env, extra...)
}

func runShell(t *testing.T, name string, args []string, env []string, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell startup fixtures require Unix paths")
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	cmd := exec.Command(bin, append(args, script)...)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	// stderr carries job-control and distro rc noise, never the prompt value.
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, out)
	}
	// Distro rc files may print banners; the value sits between markers.
	_, rest, _ := strings.Cut(string(out), "<<")
	val, _, _ := strings.Cut(rest, ">>")
	return val
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBashInjection(t *testing.T) {
	shims, err := EnsureShims(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".bashrc"), "PS1='user$ '\n")
	bin, _ := exec.LookPath("bash")
	plan := BuildPlan(bin, shims, nil)
	args := append(plan.Args, "-i", "-c")

	// Run the prompt command by hand: it only fires before an interactive prompt.
	got := runShell(t, "bash", args, shellEnv(home), `eval "$PROMPT_COMMAND"; printf "<<%s>>" "$PS1"`)
	if got != "🪴1 user$ " {
		t.Errorf("PS1 = %q", got)
	}
	// Prompt runs repeatedly (starship style) without stacking the prefix.
	got = runShell(t, "bash", args, shellEnv(home), `eval "$PROMPT_COMMAND"; eval "$PROMPT_COMMAND"; printf "<<%s>>" "$PS1"`)
	if got != "🪴1 user$ " {
		t.Errorf("repeated PS1 = %q", got)
	}
	// Nesting: the child's env carries depth 2.
	got = runShell(t, "bash", args, shellEnv(home, "BONSAI_DEPTH=2", "BONSAI_PROMPT_PREFIX=🪴2 "), `eval "$PROMPT_COMMAND"; printf "<<%s>>" "$PS1"`)
	if got != "🪴2 user$ " {
		t.Errorf("nested PS1 = %q", got)
	}
	// The opt-in snippet in .bashrc plus the shim must not double the prefix.
	snippet, _ := Snippet("bash")
	writeFile(t, filepath.Join(home, ".bashrc"), "PS1='user$ '\n"+snippet)
	got = runShell(t, "bash", args, shellEnv(home), `eval "$PROMPT_COMMAND"; printf "<<%s>>" "$PS1"`)
	if got != "🪴1 user$ " {
		t.Errorf("guarded PS1 = %q", got)
	}
	// The guard is not exported, so a child shell installs its own hook.
	got = runShell(t, "bash", []string{"-c"}, shellEnv(home), `. `+shims+`/hook.bash; bash -c 'echo "<<${__BONSAI_PROMPT_INSTALLED-unset}>>"'`)
	if strings.TrimSpace(got) != "unset" {
		t.Errorf("guard leaked to child: %q", got)
	}
}

func TestZshInjection(t *testing.T) {
	shims, err := EnsureShims(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".zshrc"), "PROMPT='user%# '\n")
	bin, _ := exec.LookPath("zsh")
	script := `for f in $precmd_functions; do $f; done; print -rn -- "<<$PROMPT|${ZDOTDIR-unset}|$(env | grep -c BONSAI_ORIG_ZDOTDIR)>>"`

	plan := BuildPlan(bin, shims, nil)
	got := runShell(t, "zsh", []string{"-i", "-c"}, append(shellEnv(home), plan.Env...), script)
	if got != "🪴1 user%# |unset|0" {
		t.Errorf("got %q", got)
	}

	// A custom ZDOTDIR is honored and handed back to children.
	zd := filepath.Join(home, "zd")
	if err := os.Mkdir(zd, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(zd, ".zshrc"), "PROMPT='zd%# '\n")
	base := shellEnv(home, "ZDOTDIR="+zd)
	plan = BuildPlan(bin, shims, base)
	got = runShell(t, "zsh", []string{"-i", "-c"}, append(base, plan.Env...), script)
	if got != "🪴1 zd%# |"+zd+"|0" {
		t.Errorf("custom ZDOTDIR got %q", got)
	}

	// Nesting and the opt-in snippet together.
	snippet, _ := Snippet("zsh")
	writeFile(t, filepath.Join(zd, ".zshrc"), "PROMPT='zd%# '\n"+snippet)
	base = shellEnv(home, "ZDOTDIR="+zd, "BONSAI_DEPTH=2", "BONSAI_PROMPT_PREFIX=🪴2 ")
	plan = BuildPlan(bin, shims, base)
	got = runShell(t, "zsh", []string{"-i", "-c"}, append(base, plan.Env...), script)
	if got != "🪴2 zd%# |"+zd+"|0" {
		t.Errorf("nested+guard got %q", got)
	}
}

// A .zshrc that declares variables (typeset -U path) must keep them: the shim
// sources it at top level, not inside a function that would scope them away.
func TestZshRcDeclarationsSurvive(t *testing.T) {
	shims, err := EnsureShims(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".zshrc"), "typeset -U path\npath=(/custom/tools $path)\ntypeset -g MARK=kept\n")
	bin, _ := exec.LookPath("zsh")
	plan := BuildPlan(bin, shims, nil)
	got := runShell(t, "zsh", []string{"-i", "-c"}, append(shellEnv(home), plan.Env...), `print -rn -- "<<${path[1]}|${(t)path}|${MARK-unset}>>"`)
	if got != "/custom/tools|array-tied-unique-special|kept" {
		t.Errorf("got %q", got)
	}
}

// A ZDOTDIR the user's own .zshenv assigns (not inherited) must steer the
// shim to that directory's .zshrc, as it does in a plain zsh start.
func TestZshEnvMovesZdotdir(t *testing.T) {
	shims, err := EnsureShims(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	alt := filepath.Join(home, "alt")
	if err := os.Mkdir(alt, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".zshenv"), "ZDOTDIR="+alt+"\n")
	writeFile(t, filepath.Join(alt, ".zshrc"), "PROMPT='alt%# '\n")
	bin, _ := exec.LookPath("zsh")
	plan := BuildPlan(bin, shims, nil)
	got := runShell(t, "zsh", []string{"-i", "-c"}, append(shellEnv(home), plan.Env...), `for f in $precmd_functions; do $f; done; print -rn -- "<<$PROMPT|${ZDOTDIR-unset}>>"`)
	if got != "🪴1 alt%# |"+alt {
		t.Errorf("got %q", got)
	}
}

func TestShInjection(t *testing.T) {
	shims, err := EnsureShims(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sh", "dash"} {
		t.Run(name, func(t *testing.T) {
			bin, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s not installed", name)
			}
			home := t.TempDir()
			userEnv := filepath.Join(home, "env")
			writeFile(t, userEnv, "PS1='user$ '\n")
			base := shellEnv(home, "ENV="+userEnv)
			plan := BuildPlan(bin, shims, base)
			got := runShell(t, name, []string{"-i", "-c"}, append(base, plan.Env...), `printf "<<%s>>" "$PS1|$ENV"`)
			if got != "🪴1 user$ |"+userEnv {
				t.Errorf("got %q", got)
			}
		})
	}
}
