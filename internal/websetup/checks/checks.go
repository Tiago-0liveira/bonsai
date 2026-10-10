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

	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
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
	IDTunnelCustom      = "tunnel.custom"
	IDUpdatesLive       = "updates.live"
	IDLiveHooks         = "updates.hooks"
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
	// Live reads what live updates achieved (only asked in live mode).
	Live func(ctx context.Context) (LiveSummary, error)
	// LeftoverHooks looks for Bonsai webhooks nothing uses any more; nil
	// when there is nowhere to look.
	LeftoverHooks func(ctx context.Context) ([]LeftoverHook, error)
}

// LiveSummary is the state of live updates as the running API recorded it.
type LiveSummary struct {
	// Running is set while bonsai web runs with its change receiver.
	Running     bool
	PublicURL   string
	TunnelError string
	Repos       []LiveRepo
}

// LiveRepo is one live repository's hook state (config.WebLiveState*, or ""
// before the API set it up).
type LiveRepo struct {
	Name  string
	State string
	Error string
}

// LeftoverHook is a Bonsai webhook of this computer on a repository that no
// longer uses live updates.
type LeftoverHook struct {
	Repo   string
	HookID int64
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
	// Tunnel is the live-updates tunnel preset and TunnelProgram the program
	// it runs ("" for none); both only count while UpdatesMode is live.
	Tunnel        string
	TunnelProgram string
}

func (o Options) live() bool { return o.UpdatesMode == "live" }

// needs reports whether the chosen tunnel runs tool.
func (o Options) needs(tool string) bool { return o.live() && o.TunnelProgram == tool }

// Run performs every check concurrently and returns them in a stable order.
func Run(ctx context.Context, env Env, opts Options) []Check {
	probes := []func() Check{
		func() Check { return checkGit(ctx, env) },
		func() Check { return checkGHInstalled(ctx, env) },
		func() Check { return checkGHAuth(ctx, env) },
		func() Check { return checkProjects(ctx, env) },
		func() Check { return checkPort(env, opts.APIPort) },
		func() Check { return checkCloudflared(ctx, env, opts) },
		func() Check { return checkNgrok(ctx, env, opts) },
		func() Check { return checkTailscale(ctx, env, opts) },
	}
	if program := opts.TunnelProgram; opts.live() && program != "" && !knownTunnelTool(program) {
		probes = append(probes, func() Check { return checkCustomTunnel(env, program) })
	}
	if opts.live() {
		probes = append(probes, func() Check { return checkLive(ctx, env) })
	}
	if env.LeftoverHooks != nil {
		probes = append(probes, func() Check { return checkLeftoverHooks(ctx, env) })
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

func knownTunnelTool(name string) bool {
	switch name {
	case "cloudflared", "ngrok", "tailscale":
		return true
	}
	return false
}

// unused says why a tunnel tool does not matter right now.
func unused(opts Options) string {
	if opts.live() {
		return "not used by your tunnel"
	}
	return "only needed for live updates"
}

// TunnelInstallFix is the install command for a tunnel tool on goos, nil for
// a tool it does not know.
func TunnelInstallFix(goos, tool string) *Fix {
	if !knownTunnelTool(tool) {
		return nil
	}
	return tunnelInstallFix(goos, tool)
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

// account is a tunnel tool's login state. ready is false when the chosen
// tunnel cannot run until fix is done; then the check has severity.
type account struct {
	text     string
	fix      *Fix
	ready    bool
	severity State
}

// tunnelCheck is shared by the tunnel tools: detection, version and login
// state. A tool the chosen tunnel runs is ok, warn or fail; any other tool
// is only described (skip).
func tunnelCheck(ctx context.Context, env Env, opts Options, id, tool string, versionArgs []string, login func() account) Check {
	c := Check{ID: id, Title: tool, State: Skip}
	needed := opts.needs(tool)
	if !installed(env, tool) {
		c.Fix = tunnelInstallFix(env.GOOS, tool)
		if needed {
			c.State, c.Detail = Fail, "not installed · your live updates tunnel needs it"
			return c
		}
		c.Detail = "not installed · " + unused(opts)
		return c
	}
	version, err := toolVersion(ctx, env, tool, versionArgs...)
	if err != nil || version == "" {
		version = "version unknown"
	}
	c.Detail = "installed (" + version + ")"
	ready := true
	severity := Warn
	if login != nil {
		a := login()
		if a.text != "" {
			c.Detail += " · " + a.text
		}
		c.Fix, ready, severity = a.fix, a.ready, a.severity
	}
	switch {
	case !needed:
		c.Detail += " · " + unused(opts)
	case ready:
		c.State = OK
	default:
		c.State = severity
	}
	return c
}

func checkCloudflared(ctx context.Context, env Env, opts Options) Check {
	return tunnelCheck(ctx, env, opts, IDTunnelCloudflared, "cloudflared", []string{"--version"}, func() account {
		// A quick tunnel needs no account; only named tunnels need the
		// origin certificate written by `cloudflared tunnel login`.
		cert := env.Getenv("TUNNEL_ORIGIN_CERT")
		if cert == "" && env.HomeDir != "" {
			cert = filepath.Join(env.HomeDir, ".cloudflared", "cert.pem")
		}
		if cert != "" && env.Exists(cert) {
			return account{text: "logged in to Cloudflare", ready: true}
		}
		login := &Fix{Command: "cloudflared tunnel login", Inline: true}
		if opts.Tunnel == "cloudflared-named" {
			// The tunnel's credentials file may exist without the cert, so
			// this only warns.
			return account{text: "not logged in; your named tunnel needs a Cloudflare login", fix: login, severity: Warn}
		}
		return account{text: "not logged in (only a custom domain needs it)", fix: login, ready: true}
	})
}

var (
	ngrokConfigPattern    = regexp.MustCompile(`(?m)configuration file at (.+)$`)
	ngrokAuthtokenPattern = regexp.MustCompile(`(?m)^\s*authtoken:\s*\S`)
)

func checkNgrok(ctx context.Context, env Env, opts Options) Check {
	return tunnelCheck(ctx, env, opts, IDTunnelNgrok, "ngrok", []string{"version"}, func() account {
		missing := account{
			text:     "no authtoken",
			fix:      &Fix{Command: "ngrok config add-authtoken <your-token>   (from https://dashboard.ngrok.com/get-started/your-authtoken)"},
			severity: Fail,
		}
		if env.Getenv("NGROK_AUTHTOKEN") != "" {
			return account{text: "authtoken set", ready: true}
		}
		stdout, stderr, code, err := env.Run(ctx, "ngrok", "config", "check")
		if err != nil || code != 0 {
			return missing
		}
		m := ngrokConfigPattern.FindStringSubmatch(stdout + "\n" + stderr)
		if m == nil {
			return missing
		}
		// Only the presence of the key is checked; the value never leaves
		// this function.
		raw, err := env.ReadFile(strings.TrimSpace(m[1]))
		if err != nil || !ngrokAuthtokenPattern.Match(raw) {
			return missing
		}
		return account{text: "authtoken set", ready: true}
	})
}

func checkTailscale(ctx context.Context, env Env, opts Options) Check {
	return tunnelCheck(ctx, env, opts, IDTunnelTailscale, "tailscale", []string{"version"}, func() account {
		notConnected := account{text: "not connected", fix: &Fix{Command: "tailscale up"}, severity: Fail}
		stdout, _, _, err := env.Run(ctx, "tailscale", "status", "--json")
		if err != nil {
			return notConnected
		}
		var status struct {
			BackendState string `json:"BackendState"`
		}
		if json.Unmarshal([]byte(stdout), &status) != nil || status.BackendState != "Running" {
			return notConnected
		}
		return account{text: "connected", ready: true}
	})
}

// checkCustomTunnel looks for the program of a custom tunnel command.
func checkCustomTunnel(env Env, program string) Check {
	c := Check{ID: IDTunnelCustom, Title: "tunnel command"}
	if !installed(env, program) {
		c.State, c.Detail = Fail, program+" is not found on your PATH; your custom tunnel runs it"
		c.Fix = &Fix{Command: "install " + program + ", or change the command in: bonsai web setup → Updates"}
		return c
	}
	c.State, c.Detail = OK, program+" found"
	return c
}

// checkLive is what live updates achieved: the tunnel's public address and
// each repository's hook.
func checkLive(ctx context.Context, env Env) Check {
	c := Check{ID: IDUpdatesLive, Title: "live updates"}
	if env.Live == nil {
		c.State, c.Detail = Skip, "not checked"
		return c
	}
	s, err := env.Live(ctx)
	switch {
	case err != nil:
		c.State, c.Detail, c.Fix = Warn, "cannot read the live updates state: "+err.Error(), &Fix{Command: "bonsai web status"}
		return c
	case len(s.Repos) == 0:
		c.State, c.Detail, c.Fix = Warn, "no repositories picked, so every repository stays on standard updates", &Fix{Command: "bonsai web setup"}
		return c
	case !s.Running:
		c.State, c.Detail = Skip, plural(len(s.Repos), "repo")+" picked · starts with bonsai web"
		return c
	case s.TunnelError != "":
		c.State, c.Detail, c.Fix = Warn, "no public address: "+s.TunnelError, &Fix{Command: "bonsai web logs tunnel"}
		return c
	case s.PublicURL == "":
		c.State, c.Detail, c.Fix = Warn, "waiting for the tunnel's public address", &Fix{Command: "bonsai web logs tunnel"}
		return c
	}
	live := 0
	var problem *LiveRepo
	for i, r := range s.Repos {
		if r.State == "live" {
			live++
		} else if problem == nil {
			problem = &s.Repos[i]
		}
	}
	if problem == nil {
		c.State, c.Detail = OK, plural(live, "repo")+" live · "+s.PublicURL
		return c
	}
	c.State = Warn
	c.Detail = fmt.Sprintf("%d of %s live · %s %s", live, plural(len(s.Repos), "repo"), problem.Name, liveStateText(*problem))
	c.Fix = liveFix(*problem)
	return c
}

func liveStateText(r LiveRepo) string {
	switch r.State {
	case "needs_admin":
		return "needs admin"
	case "scope_missing":
		return "cannot be managed with your gh login"
	case "waiting_for_ping":
		return "is waiting for GitHub's ping"
	case "failing":
		if r.Error != "" {
			return "is failing (" + r.Error + ")"
		}
		return "is failing"
	case "":
		return "is not set up yet"
	}
	return r.State
}

func liveFix(r LiveRepo) *Fix {
	switch r.State {
	case "needs_admin":
		return &Fix{Command: "ask an admin of " + r.Name + ", or drop it in: bonsai web setup → Updates"}
	case "scope_missing":
		return &Fix{Command: app.ScopeFix("github.com"), Inline: true}
	case "failing":
		return &Fix{Command: "bonsai web logs tunnel"}
	}
	return &Fix{Command: "bonsai web status"}
}

// checkLeftoverHooks lists Bonsai webhooks of this computer on repositories
// live updates no longer cover. Doctor never deletes them; the fix does.
func checkLeftoverHooks(ctx context.Context, env Env) Check {
	c := Check{ID: IDLiveHooks, Title: "Bonsai webhooks"}
	hooks, err := env.LeftoverHooks(ctx)
	if err != nil {
		c.State, c.Detail = Skip, "could not look for leftover webhooks: "+err.Error()
		return c
	}
	if len(hooks) == 0 {
		c.State, c.Detail = OK, "none left behind"
		return c
	}
	repos := make([]string, 0, len(hooks))
	commands := make([]string, 0, len(hooks))
	for _, h := range hooks {
		repos = append(repos, h.Repo)
		commands = append(commands, fmt.Sprintf("gh api -X DELETE repos/%s/hooks/%d", h.Repo, h.HookID))
	}
	c.State = Warn
	verb := "have"
	if len(hooks) == 1 {
		verb = "has"
	}
	c.Detail = strings.Join(repos, ", ") + " still " + verb + " a Bonsai webhook that live updates no longer use"
	c.Fix = &Fix{Command: strings.Join(commands, " && ")}
	return c
}
