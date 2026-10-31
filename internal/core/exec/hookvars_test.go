package exec

import (
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCmdDelayedExpansion(t *testing.T) {
	vars := map[string]string{"branch": "main", "pr_number": "123", "0_a": "value"}
	tests := []struct {
		name, command string
		style         refStyle
		wantDelayed   bool
	}{
		{"literal bang", "echo done!", refCmd, false},
		{"git format", "git log --format=%h!", refCmd, false},
		{"paired bangs", "echo !literal!", refCmd, false},
		{"unknown placeholder", "echo {unknown}!", refCmd, false},
		{"substitution", "echo {branch}", refCmd, true},
		{"quoted substitution", `echo "{pr_number}"`, refCmd, true},
		{"placeholder grammar", "echo {0_a}", refCmd, true},
		{"mixed bangs", "echo {branch} done!", refCmd, true},
		{"empty name", "echo !BONSAI_!", refCmd, false},
		{"invalid name", "echo !BONSAI_A-B!", refCmd, false},
		{"incomplete reference", "echo !BONSAI_BRANCH", refCmd, false},
		{"posix", "echo {branch} !BONSAI_BRANCH!", refPOSIX, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := expandVarRefs(tt.command, vars, tt.style)
			cmd := &osexec.Cmd{Args: []string{"cmd.exe", "/c", command}}
			enableDelayedExpansion(cmd, tt.style)
			want := []string{"cmd.exe", "/c", command}
			if tt.wantDelayed {
				want = []string{"cmd.exe", "/v:on", "/c", command}
			}
			if !reflect.DeepEqual(cmd.Args, want) {
				t.Fatalf("args = %q, want %q", cmd.Args, want)
			}
		})
	}
}

func skipNoPOSIXShell(t *testing.T) {
	t.Helper()
	if hookRefStyle() != refPOSIX {
		t.Skip("posix shell syntax")
	}
	if _, err := osexec.LookPath("sh"); err != nil && runtime.GOOS != "windows" {
		t.Skip("no sh")
	}
}

func TestExpandVarRefsQuoting(t *testing.T) {
	vars := map[string]string{"branch": "x", "pr_number": ""}
	tests := []struct{ in, want string }{
		{`echo {branch}`, `echo ${BONSAI_BRANCH}`},
		{`echo "{branch}"`, `echo "${BONSAI_BRANCH}"`},
		{`echo '{branch}'`, `echo ''"${BONSAI_BRANCH}"''`},
		{`echo "it's {branch}"`, `echo "it's ${BONSAI_BRANCH}"`},
		{`echo \'{branch}`, `echo \'${BONSAI_BRANCH}`},
		{`echo {unknown} {pr_number}`, `echo {unknown} ${BONSAI_PR_NUMBER}`},
	}
	for _, tt := range tests {
		if got := expandVarRefs(tt.in, vars, refPOSIX); got != tt.want {
			t.Errorf("expandVarRefs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := expandVarRefs(`echo {branch}`, vars, refCmd); got != `echo !BONSAI_BRANCH!` {
		t.Errorf("cmd style = %q", got)
	}
}

func TestRunHooksValuesAreNeverShellSource(t *testing.T) {
	skipNoPOSIXShell(t)
	values := []string{
		`feat-$(touch${IFS}injected)`,
		"feat-`touch injected`",
		`a"b'c`,
		`a;touch injected`,
		`a|touch injected`,
		`a && touch injected`,
		`dir with spaces/$HOME`,
	}
	for _, v := range values {
		for _, tpl := range []string{`printf '%s' "{branch}" > out`, `printf '%s' '{branch}' > out`} {
			dir := t.TempDir()
			if err := RunHooks(dir, []string{tpl}, map[string]string{"branch": v}); err != nil {
				t.Fatalf("%q with %q: %v", tpl, v, err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "out"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != v {
				t.Errorf("%q with %q wrote %q", tpl, v, got)
			}
			if _, err := os.Stat(filepath.Join(dir, "injected")); err == nil {
				t.Errorf("%q with %q executed the value", tpl, v)
			}
		}
	}
}

func TestInlineVarRefsIsSelfContained(t *testing.T) {
	skipNoPOSIXShell(t)
	dir := t.TempDir()
	vars := map[string]string{"branch": `it's $(touch injected) "x"`, "repo": "r"}
	cmd := osexec.CommandContext(context.Background(), "sh", "-c", InlineVarRefs(`printf '%s' "{branch}" '{repo}' > out`, vars))
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "out"))
	if want := vars["branch"] + "r"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); err == nil {
		t.Error("value was executed")
	}
	if got := InlineVarRefs("echo plain", vars); got != "echo plain" || strings.Contains(got, "export") {
		t.Errorf("command without placeholders changed: %q", got)
	}
}
