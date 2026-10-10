// Package checks inspects what `bonsai web` needs on this computer: git, the
// GitHub CLI and its login, project folders, the API port and the tunnel
// tools that live updates will use. The setup TUI and `bonsai web doctor`
// render the same results.
//
// Every probe goes through Env, so tests never run real tools. Output from
// tools is reduced to versions, account names and states; nothing a tool
// prints is kept verbatim, and no probe ever asks for a token.
package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	OK   State = "ok"
	Warn State = "warn"
	Fail State = "fail"
	Skip State = "skip"
)

// Fix is one concrete thing the user can do. Inline fixes are interactive,
// user-level commands the setup TUI may run for them (it suspends itself);
// everything else is only shown and copied.
type Fix struct {
	Command string
	Inline  bool
}

type Check struct {
	ID     string
	Title  string
	State  State
	Detail string
	Fix    *Fix
}

// IDs of the checks, in the order Run returns them.
const (
	IDGit               = "git"
	IDGHInstalled       = "gh.installed"
	IDGHAuth            = "gh.auth"
	IDProjects          = "projects"
	IDPort              = "port.api"
	IDTunnelCloudflared = "tunnel.cloudflared"
	IDTunnelNgrok       = "tunnel.ngrok"
	IDTunnelTailscale   = "tunnel.tailscale"
	IDUpdatesLive       = "updates.live"
)

const (
	// Bonsai's overview status uses `git status --show-stash` (git 2.35).
	MinGitMajor = 2
	MinGitMinor = 35

	DefaultCommandTimeout = 5 * time.Second
)

// PortStatus describes who holds the API port.
type PortStatus struct {
	Free bool
	// Web is set when the running bonsai web stack itself holds the port.
	Web bool
	// Owner names a foreign holder when the OS allows it ("pid 4242, node").
	Owner string
	// Bonsai is set when the holder answers as a bonsai local API that is
	// not this stack (a dev stack, another user).
	Bonsai bool
	// Replaceable names a holder bonsai web stops and replaces when it
	// starts (an older per-repo `bonsai serve`).
	Replaceable string
}

// ProjectsSummary is what discovery finds under the configured roots.
type ProjectsSummary struct {
	Folders     int
	Unavailable []string // folders that cannot be read
	Found       int      // repositories discovered
	Shown       int      // of those, selected to show in Bonsai
}

// Env is every effect the checks need. SystemEnv fills in the real ones
// except Port and Projects, which belong to the caller.
type Env struct {
	GOOS     string
	HomeDir  string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Run executes a tool and returns its output and exit code. err is only
	// set when the tool could not run at all (or timed out).
	Run      func(ctx context.Context, name string, args ...string) (stdout, stderr string, code int, err error)
	Exists   func(path string) bool
	ReadFile func(path string) ([]byte, error)
	Port     func(port int) PortStatus
	Projects func(ctx context.Context) (ProjectsSummary, error)
}

// SystemEnv runs real tools with a per-command timeout.
func SystemEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{
		GOOS:     runtime.GOOS,
		HomeDir:  home,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Run:      runCommand,
		Exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		ReadFile: os.ReadFile,
	}
}

func runCommand(ctx context.Context, name string, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// Plain, predictable output: no colors, no pagers, no prompts.
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errOut.String(), 0, nil
	case ctx.Err() != nil:
		return out.String(), errOut.String(), -1, fmt.Errorf("%s did not answer within %s", name, DefaultCommandTimeout)
	case errors.As(err, &exit):
		return out.String(), errOut.String(), exit.ExitCode(), nil
	default:
		return out.String(), errOut.String(), -1, err
	}
}

type Options struct {
	APIPort     int
	UpdatesMode string // "standard" or "live"
}

// Run performs every check concurrently and returns them in a stable order.
func Run(ctx context.Context, env Env, opts Options) []Check {
	probes := []func() Check{
		func() Check { return checkGit(ctx, env) },
		func() Check { return checkGHInstalled(ctx, env) },
		func() Check { return checkGHAuth(ctx, env) },
		func() Check { return checkProjects(ctx, env) },
		func() Check { return checkPort(env, opts.APIPort) },
		func() Check { return checkCloudflared(ctx, env) },
		func() Check { return checkNgrok(ctx, env) },
		func() Check { return checkTailscale(ctx, env) },
	}
	out := make([]Check, len(probes))
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = probe()
		}()
	}
	wg.Wait()
	if opts.UpdatesMode == "live" {
		out = append(out, Check{
			ID:     IDUpdatesLive,
			Title:  "live updates",
			State:  Warn,
			Detail: "live updates arrive in a later version; Bonsai uses standard updates for now",
			Fix:    &Fix{Command: "bonsai web setup"},
		})
	}
	return out
}

// Find returns the check with id.
func Find(checks []Check, id string) (Check, bool) {
	for _, c := range checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

// Counts reports how many checks failed and how many warned.
func Counts(checks []Check) (failed, warned int) {
	for _, c := range checks {
		switch c.State {
		case Fail:
			failed++
		case Warn:
			warned++
		}
	}
	return failed, warned
}

// Failed reports whether any check failed (doctor exits 1).
func Failed(checks []Check) bool {
	failed, _ := Counts(checks)
	return failed > 0
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// toolVersion runs `name args...` and returns the first dotted version in its
// output, or "" when there is none.
func toolVersion(ctx context.Context, env Env, name string, args ...string) (string, error) {
	stdout, stderr, code, err := env.Run(ctx, name, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s %s exited with code %d", name, strings.Join(args, " "), code)
	}
	firstLine := strings.SplitN(strings.TrimSpace(stdout+"\n"+stderr), "\n", 2)[0]
	return versionPattern.FindString(firstLine), nil
}

func installed(env Env, name string) bool {
	_, err := env.LookPath(name)
	return err == nil
}

func gitInstallFix(goos string) *Fix {
	switch goos {
	case "darwin":
		return &Fix{Command: "xcode-select --install"}
	case "windows":
		return &Fix{Command: "winget install --id Git.Git"}
	}
	return &Fix{Command: "install git 2.35 or newer: https://git-scm.com/downloads/linux"}
}

func checkGit(ctx context.Context, env Env) Check {
	c := Check{ID: IDGit, Title: "git"}
	if !installed(env, "git") {
		c.State, c.Detail, c.Fix = Fail, "git is not installed; Bonsai needs it for everything", gitInstallFix(env.GOOS)
		return c
	}
	version, err := toolVersion(ctx, env, "git", "--version")
	if err != nil || version == "" {
		c.State, c.Detail, c.Fix = Fail, "git is installed but does not report its version", gitInstallFix(env.GOOS)
		return c
	}
	parts := versionPattern.FindStringSubmatch(version)
	major, _ := strconv.Atoi(parts[1])
	minor, _ := strconv.Atoi(parts[2])
	if major < MinGitMajor || (major == MinGitMajor && minor < MinGitMinor) {
		c.State = Fail
		c.Detail = fmt.Sprintf("%s is too old; Bonsai needs %d.%d or newer", version, MinGitMajor, MinGitMinor)
		c.Fix = gitInstallFix(env.GOOS)
		return c
	}
	c.State, c.Detail = OK, version
	return c
}

func ghInstallFix(goos string) *Fix {
	switch goos {
	case "darwin":
		return &Fix{Command: "brew install gh"}
	case "windows":
		return &Fix{Command: "winget install --id GitHub.cli"}
	}
	return &Fix{Command: "install gh: https://github.com/cli/cli#installation"}
}

const withoutGitHub = "without it you still get worktrees, branches, commits and processes"

func checkGHInstalled(ctx context.Context, env Env) Check {
	c := Check{ID: IDGHInstalled, Title: "gh installed"}
	if !installed(env, "gh") {
		c.State, c.Detail, c.Fix = Warn, "GitHub CLI not found; "+withoutGitHub, ghInstallFix(env.GOOS)
		return c
	}
	version, err := toolVersion(ctx, env, "gh", "--version")
	if err != nil {
		c.State, c.Detail, c.Fix = Warn, "gh is installed but does not run: "+err.Error(), ghInstallFix(env.GOOS)
		return c
	}
	c.State, c.Detail = OK, version
	return c
}

// GitHubHost is the host Bonsai checks the gh login for.
func GitHubHost(env Env) string {
	if host := strings.TrimSpace(env.Getenv("GH_HOST")); host != "" {
		return host
	}
	return "github.com"
}

var ghAccountPattern = regexp.MustCompile(`(?:account|as) ([A-Za-z0-9][A-Za-z0-9-]*)`)

func checkGHAuth(ctx context.Context, env Env) Check {
	host := GitHubHost(env)
	c := Check{ID: IDGHAuth, Title: "gh logged in"}
	if !installed(env, "gh") {
		c.State, c.Detail = Skip, "needs the GitHub CLI"
		return c
	}
	login := &Fix{Command: "gh auth login --hostname " + host, Inline: true}
	// --json has no token field; --show-token is never used.
	stdout, stderr, code, err := env.Run(ctx, "gh", "auth", "status", "--active", "--hostname", host, "--json", "hosts")
	if err != nil {
		c.State, c.Detail, c.Fix = Warn, err.Error(), login
		return c
	}
	if code == 0 && strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		var parsed struct {
			Hosts map[string][]struct {
				State  string `json:"state"`
				Active bool   `json:"active"`
				Login  string `json:"login"`
			} `json:"hosts"`
		}
		if json.Unmarshal([]byte(stdout), &parsed) == nil {
			for _, account := range parsed.Hosts[host] {
				if !account.Active && len(parsed.Hosts[host]) > 1 {
					continue
				}
				if account.State == "success" {
					c.State, c.Detail = OK, host+" · "+account.Login
					return c
				}
				c.State = Warn
				c.Detail = fmt.Sprintf("the %s login for %s no longer works; %s", host, account.Login, withoutGitHub)
				c.Fix = login
				return c
			}
			c.State, c.Detail, c.Fix = Warn, "not logged in to "+host+"; "+withoutGitHub, login
			return c
		}
	}
	// Older gh without `auth status --json`: the exit code is the answer.
	stdout, stderr, code, err = env.Run(ctx, "gh", "auth", "status", "--hostname", host)
	if err == nil && code == 0 {
		c.State, c.Detail = OK, host
		if m := ghAccountPattern.FindStringSubmatch(stdout + "\n" + stderr); m != nil {
			c.Detail = host + " · " + m[1]
		}
		return c
	}
	c.State, c.Detail, c.Fix = Warn, "not logged in to "+host+"; "+withoutGitHub, login
	return c
}

func checkProjects(ctx context.Context, env Env) Check {
	c := Check{ID: IDProjects, Title: "project folders"}
	setup := &Fix{Command: "bonsai web setup"}
	if env.Projects == nil {
		c.State, c.Detail = Skip, "not checked"
		return c
	}
	summary, err := env.Projects(ctx)
	if err != nil {
		c.State, c.Detail, c.Fix = Warn, "cannot read your project folders: "+err.Error(), setup
		return c
	}
	if summary.Folders == 0 {
		c.State, c.Detail, c.Fix = Warn, "no project folders yet, so Bonsai has no repositories to show", setup
		return c
	}
	c.Detail = fmt.Sprintf("%s · %s found · %d shown", plural(summary.Folders, "folder"), plural(summary.Found, "repo"), summary.Shown)
	switch {
	case len(summary.Unavailable) > 0:
		c.State = Warn
		c.Detail = fmt.Sprintf("cannot read %s; %s", strings.Join(summary.Unavailable, ", "), c.Detail)
		c.Fix = setup
	case summary.Found == 0:
		c.State, c.Fix = Warn, setup
		c.Detail = "no repositories found in your project folders"
	case summary.Shown == 0:
		c.State = Warn
		c.Detail += "; pick repositories in the browser (Settings → Projects) or with bonsai web setup"
		c.Fix = setup
	default:
		c.State = OK
	}
	return c
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func checkPort(env Env, port int) Check {
	c := Check{ID: IDPort, Title: "port " + strconv.Itoa(port)}
	if env.Port == nil {
		c.State, c.Detail = Skip, "not checked"
		return c
	}
	status := env.Port(port)
	switch {
	case status.Free:
		c.State, c.Detail = OK, "free"
	case status.Web:
		c.State, c.Detail = OK, "bonsai web is running here"
	case status.Replaceable != "":
		c.State, c.Detail = OK, "used by "+status.Replaceable+"; bonsai web replaces it when it starts"
	default:
		holder := "another program"
		if status.Bonsai {
			holder = "another bonsai local API"
		}
		if status.Owner != "" {
			holder += " (" + status.Owner + ")"
		}
		c.State = Fail
		c.Detail = fmt.Sprintf("port %d is used by %s", port, holder)
		next := port + 10
		if next > 65535 {
			next = port - 10
		}
		c.Fix = &Fix{Command: fmt.Sprintf("bonsai web --port %d   or pick another port in: bonsai web setup → Advanced", next)}
	}
	return c
}

const forLive = "only needed for live updates (coming in a later version)"

// TunnelInstallFix is the install command for a tunnel tool on goos, nil for
// a tool it does not know.
func TunnelInstallFix(goos, tool string) *Fix {
	if fix := tunnelInstallFix(goos, tool); fix.Command != "" {
		return fix
	}
	return nil
}

func tunnelInstallFix(goos, tool string) *Fix {
	type commands struct{ darwin, windows, other string }
	known := map[string]commands{
		"cloudflared": {"brew install cloudflared", "winget install --id Cloudflare.cloudflared", "install cloudflared: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"},
		"ngrok":       {"brew install ngrok", "winget install --id ngrok.ngrok", "install ngrok: https://ngrok.com/download"},
		"tailscale":   {"brew install --cask tailscale", "winget install --id tailscale.tailscale", "install Tailscale: https://tailscale.com/download"},
	}[tool]
	switch goos {
	case "darwin":
		return &Fix{Command: known.darwin}
	case "windows":
		return &Fix{Command: known.windows}
	}
	return &Fix{Command: known.other}
}

// tunnelCheck is shared by the tunnel tools: detection and version now, the
// login state as detail. Live updates are not available yet, so these never
// warn or fail.
func tunnelCheck(ctx context.Context, env Env, id, tool string, versionArgs []string, login func() (string, *Fix)) Check {
	c := Check{ID: id, Title: tool, State: Skip}
	if !installed(env, tool) {
		c.Detail, c.Fix = "not installed · "+forLive, tunnelInstallFix(env.GOOS, tool)
		return c
	}
	version, err := toolVersion(ctx, env, tool, versionArgs...)
	if err != nil || version == "" {
		version = "version unknown"
	}
	c.Detail = "installed (" + version + ")"
	if login != nil {
		state, fix := login()
		if state != "" {
			c.Detail += " · " + state
		}
		c.Fix = fix
	}
	c.Detail += " · " + forLive
	return c
}

func checkCloudflared(ctx context.Context, env Env) Check {
	return tunnelCheck(ctx, env, IDTunnelCloudflared, "cloudflared", []string{"--version"}, func() (string, *Fix) {
		// A quick tunnel needs no account; only named tunnels need the
		// origin certificate written by `cloudflared tunnel login`.
		cert := env.Getenv("TUNNEL_ORIGIN_CERT")
		if cert == "" && env.HomeDir != "" {
			cert = filepath.Join(env.HomeDir, ".cloudflared", "cert.pem")
		}
		if cert != "" && env.Exists(cert) {
			return "logged in to Cloudflare", nil
		}
		return "not logged in (only a custom domain needs it)", &Fix{Command: "cloudflared tunnel login", Inline: true}
	})
}

var (
	ngrokConfigPattern    = regexp.MustCompile(`(?m)configuration file at (.+)$`)
	ngrokAuthtokenPattern = regexp.MustCompile(`(?m)^\s*authtoken:\s*\S`)
)

func checkNgrok(ctx context.Context, env Env) Check {
	return tunnelCheck(ctx, env, IDTunnelNgrok, "ngrok", []string{"version"}, func() (string, *Fix) {
		addToken := &Fix{Command: "ngrok config add-authtoken <your-token>   (from https://dashboard.ngrok.com/get-started/your-authtoken)"}
		if env.Getenv("NGROK_AUTHTOKEN") != "" {
			return "authtoken set", nil
		}
		stdout, stderr, code, err := env.Run(ctx, "ngrok", "config", "check")
		if err != nil || code != 0 {
			return "no authtoken", addToken
		}
		m := ngrokConfigPattern.FindStringSubmatch(stdout + "\n" + stderr)
		if m == nil {
			return "no authtoken", addToken
		}
		// Only the presence of the key is checked; the value never leaves
		// this function.
		raw, err := env.ReadFile(strings.TrimSpace(m[1]))
		if err != nil || !ngrokAuthtokenPattern.Match(raw) {
			return "no authtoken", addToken
		}
		return "authtoken set", nil
	})
}

func checkTailscale(ctx context.Context, env Env) Check {
	return tunnelCheck(ctx, env, IDTunnelTailscale, "tailscale", []string{"version"}, func() (string, *Fix) {
		connect := &Fix{Command: "tailscale up"}
		stdout, _, _, err := env.Run(ctx, "tailscale", "status", "--json")
		if err != nil {
			return "not connected", connect
		}
		var status struct {
			BackendState string `json:"BackendState"`
		}
		if json.Unmarshal([]byte(stdout), &status) != nil || status.BackendState != "Running" {
			return "not connected", connect
		}
		return "connected", nil
	})
}
