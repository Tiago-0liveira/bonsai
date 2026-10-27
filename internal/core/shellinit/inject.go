package shellinit

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// all: keeps the dotfiles (zsh/.zshenv) in the embedded tree.
//
//go:embed all:shims
var shimFS embed.FS

// Shell identifies a shell family for prompt injection.
type Shell string

const (
	ShellUnknown Shell = ""
	ShellBash    Shell = "bash"
	ShellZsh     Shell = "zsh"
	ShellFish    Shell = "fish"
	ShellSh      Shell = "sh" // sh, dash, ksh, mksh: all read $ENV
	ShellNu      Shell = "nu"
	ShellPwsh    Shell = "pwsh"
	ShellCmd     Shell = "cmd"
)

// Detect maps a shell path to its family from the basename, ignoring a
// trailing ".exe" and either path separator.
func Detect(shellPath string) Shell {
	name := shellPath
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	switch name {
	case "bash":
		return ShellBash
	case "zsh":
		return ShellZsh
	case "fish":
		return ShellFish
	case "sh", "dash", "ksh", "mksh":
		return ShellSh
	case "nu":
		return ShellNu
	case "pwsh", "powershell":
		return ShellPwsh
	case "cmd":
		return ShellCmd
	}
	return ShellUnknown
}

// Plan is what must be added to a shell launch to inject the prompt prefix.
type Plan struct {
	Args []string // appended after the shell program
	Env  []string // KEY=VALUE entries appended to the child environment
}

// BuildPlan returns the launch additions for shellPath. shimDir is the
// directory from EnsureShims; baseEnv is the environment the child would
// otherwise get (it supplies the user's ZDOTDIR/ENV and the prompt prefix).
// An unknown shell yields an empty plan.
func BuildPlan(shellPath, shimDir string, baseEnv []string) Plan {
	switch Detect(shellPath) {
	case ShellBash:
		return Plan{Args: []string{"--rcfile", filepath.Join(shimDir, "rcfile.bash")}}
	case ShellZsh:
		return Plan{Env: []string{
			// zsh finds its startup files through ZDOTDIR; the shims put the
			// real value back, and keep the original here until they do.
			"ZDOTDIR=" + filepath.Join(shimDir, "zsh"),
			envOrigZdotdir + "=" + lookupEnv(baseEnv, "ZDOTDIR"),
		}}
	case ShellFish:
		return Plan{Args: []string{"-C", "source " + fishQuote(filepath.Join(shimDir, "hook.fish"))}}
	case ShellSh:
		return Plan{Env: []string{
			"ENV=" + filepath.Join(shimDir, "env.sh"),
			envOrigEnv + "=" + lookupEnv(baseEnv, "ENV"),
		}}
	case ShellNu:
		return Plan{Args: []string{"-e", "source " + nuQuote(filepath.Join(shimDir, "hook.nu"))}}
	case ShellPwsh:
		// The profile already loads before -Command runs, so only wrap prompt.
		return Plan{Args: []string{"-NoExit", "-Command", ". " + psQuote(filepath.Join(shimDir, "hook.ps1"))}}
	case ShellCmd:
		// cmd has no prompt hook; set a static prompt (best effort) on top of
		// the user's PROMPT. "$" is the escape character in cmd prompt
		// strings, so only our prefix is escaped; PROMPT is already prompt
		// syntax and must stay verbatim.
		p := strings.ReplaceAll(lookupEnv(baseEnv, EnvPromptPrefix), "$", "$$")
		base := lookupEnv(baseEnv, "PROMPT")
		if base == "" {
			base = "$P$G"
		}
		// `prompt` sets PROMPT in the cmd process, so a nested bonsai inherits
		// the parent's prefix; drop it (one token: glyphs hold no whitespace)
		// instead of stacking "B2 B1 ".
		if n := DepthFrom(lookupEnv(baseEnv, EnvDepth)); n > 1 {
			if tok, rest, ok := strings.Cut(base, " "); ok && strings.HasSuffix(tok, strconv.Itoa(n-1)) {
				base = rest
			}
		}
		return Plan{Args: []string{"/K", "prompt " + p + base}}
	}
	return Plan{}
}

// fishQuote single-quotes s for fish, where backslash and quote are escaped.
func fishQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`) + "'"
}

// psQuote single-quotes s for PowerShell, where a quote is doubled and
// backslashes are literal (Windows paths).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// nuQuote double-quotes s for nu. Its single-quoted strings are raw and cannot
// hold a quote, so a path like /home/O'Connor would end the string early.
func nuQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func lookupEnv(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
			v = val // last entry wins, matching os/exec
		}
	}
	return v
}

// EnsureShims writes the embedded shims under
// <cacheDir>/bonsai/shell-integration/<version>/ and returns that directory.
// Files are rewritten only when missing or different, atomically, so a shell
// starting at the same time never reads a half-written script.
func EnsureShims(cacheDir, version string) (string, error) {
	if cacheDir == "" {
		return "", fmt.Errorf("shell integration: no cache dir")
	}
	if version == "" {
		version = "dev"
	}
	version = strings.NewReplacer("/", "_", `\`, "_", ":", "_").Replace(version)
	root := filepath.Join(cacheDir, "bonsai", "shell-integration", version)
	err := fs.WalkDir(shimFS, "shims", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "shims")))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		want, err := shimFS.ReadFile(p)
		if err != nil {
			return err
		}
		if have, err := os.ReadFile(dst); err == nil && string(have) == string(want) {
			return nil
		}
		return writeAtomic(dst, want)
	})
	if err != nil {
		return "", fmt.Errorf("write shell integration: %w", err)
	}
	return root, nil
}

func writeAtomic(dst string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".shim-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// Snippet returns the eval-able hook for `bonsai shell-init <shell>`.
func Snippet(shell string) (string, error) {
	file := ""
	switch shell {
	case "bash":
		file = "hook.bash"
	case "zsh":
		file = "hook.zsh"
	case "fish":
		file = "hook.fish"
	case "nu":
		file = "hook.nu"
	case "pwsh":
		file = "hook.ps1"
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(SnippetShells, ", "))
	}
	b, err := shimFS.ReadFile(path.Join("shims", file))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SnippetShells lists the shells Snippet supports.
var SnippetShells = []string{"bash", "zsh", "fish", "nu", "pwsh"}
