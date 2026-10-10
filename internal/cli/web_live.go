package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// liveServe is the live-updates part of the bonsai web serve spec: the
// receiver's port and the tunnel sidecar (none for standard updates, for an
// external proxy, or when the tunnel program is not installed).
type liveServe struct {
	webhookPort int
	sidecars    []procstore.ServeSidecar
	// warning explains a missing tunnel program; the receiver still runs.
	warning string
}

func (l liveServe) warn(out io.Writer) {
	if l.warning != "" {
		fmt.Fprintf(out, "! %s\n", l.warning)
	}
}

func (l liveServe) tunnel() []string {
	if len(l.sidecars) == 0 {
		return nil
	}
	return l.sidecars[0].Command
}

// oldDaemonRefusesLive matches the refusal of a web daemon from a bonsai
// version without live updates (same daemon protocol, older allow-rule).
func oldDaemonRefusesLive(err error) bool {
	return err != nil && strings.Contains(err.Error(), "cannot supervise development services")
}

// liveServe builds the live-updates part of the spec for an API on apiPort,
// creating the webhook secret on first use.
func (w *webCLI) liveServe(cfg config.WebConfig, apiPort int) (liveServe, *webFailure) {
	if cfg.Updates.Mode != config.WebUpdatesLive {
		return liveServe{}, nil
	}
	live := cfg.Updates.Live
	fix := "change updates.live in " + w.configPath + ", or switch to standard updates with bonsai web setup"
	if live.WebhookPort == apiPort {
		return liveServe{}, &webFailure{
			detail: fmt.Sprintf("port %d is both the API port and the live updates webhook port", apiPort),
			fix:    "use another --port, or change updates.live.webhook_port in " + w.configPath,
		}
	}
	secretPath, err := config.WebWebhookSecretPath()
	if err == nil {
		_, err = config.EnsureWebWebhookSecret(secretPath)
	}
	if err != nil {
		return liveServe{}, &webFailure{detail: "live updates webhook secret: " + err.Error(), fix: fix}
	}
	argv, err := webtunnel.Argv(live.TunnelOptions())
	if err == nil && argv != nil {
		err = webtunnel.ValidateArgv(argv, live.WebhookPort, apiPort)
	}
	if err != nil {
		return liveServe{}, &webFailure{detail: "live updates tunnel: " + err.Error(), fix: fix}
	}
	out := liveServe{webhookPort: live.WebhookPort}
	if argv == nil {
		return out, nil // external-url: the user's own proxy forwards to the receiver
	}
	lookPath := w.lookPath
	if lookPath == nil {
		lookPath = execLookPath
	}
	if _, err := lookPath(argv[0]); err != nil {
		install := "install " + argv[0]
		if fix := checks.TunnelInstallFix(runtime.GOOS, argv[0]); fix != nil && fix.Command != "" {
			install = fix.Command
		}
		out.warning = fmt.Sprintf("%s is not installed, so live updates have no tunnel yet.\n  fix   %s      then run bonsai web again", argv[0], install)
		return out, nil
	}
	out.sidecars = []procstore.ServeSidecar{{Name: webtunnel.SidecarName, Command: argv}}
	return out, nil
}

// webhookPortFailure is portFailure for the receiver's port.
func (w *webCLI) webhookPortFailure(f webFailure, port int) webFailure {
	f.process = "hook"
	f.detail = "live updates webhook " + f.detail
	f.fix = fmt.Sprintf("set updates.live.webhook_port to a free port (for example %d) in %s", port+10, w.configPath)
	return f
}

// liveUpdatesText is the "updates" line for live mode.
func liveUpdatesText(cfg config.WebConfig, group *procstore.ServeGroup, state config.WebLiveState) string {
	live := cfg.Updates.Live
	parts := []string{"live", live.Tunnel}
	if n := len(live.Repositories); n > 0 {
		healthy := 0
		for _, repo := range live.Repositories {
			if _, r, ok := state.Repository(repo); ok && r.State == config.WebLiveStateLive {
				healthy++
			}
		}
		parts = append(parts, fmt.Sprintf("%d %s (%d live)", n, plural(n, "repo", "repos"), healthy))
	} else {
		parts = append(parts, "no repos yet")
	}
	if group != nil && group.WebhookPort != 0 {
		if live.Tunnel != webtunnel.ExternalURL {
			parts = append(parts, "tunnel "+tunnelState(group))
		}
		parts = append(parts, "receiver 127.0.0.1:"+strconv.Itoa(group.WebhookPort))
	}
	return strings.Join(parts, " · ")
}

func tunnelState(group *procstore.ServeGroup) string {
	for _, p := range group.Processes {
		if p.Name == webtunnel.SidecarName {
			return p.State
		}
	}
	return "not running"
}

func hasProcess(group *procstore.ServeGroup, name string) bool {
	for _, p := range group.Processes {
		if p.Name == name {
			return true
		}
	}
	return false
}

// errNoTunnel explains why a running bonsai web has no tunnel process.
func (w *webCLI) errNoTunnel() error {
	cfg, _, err := config.ReadWebConfig(w.configPath)
	switch {
	case err != nil || cfg.Updates.Mode != config.WebUpdatesLive:
		return errors.New("there is no tunnel: live updates are off. Turn them on with: bonsai web setup")
	case cfg.Updates.Live.Tunnel == webtunnel.ExternalURL:
		return errors.New("there is no tunnel: live updates use your own proxy (external-url)")
	}
	return errors.New("there is no tunnel: it did not start with bonsai web. Run bonsai web again to see why")
}

// liveState reads what the API's live controller recorded (empty when there
// is nothing yet or the file cannot be read).
func (w *webCLI) liveState(cfg config.WebConfig) config.WebLiveState {
	if cfg.Updates.Mode != config.WebUpdatesLive || w.statePath == "" {
		return config.WebLiveState{}
	}
	state, err := config.ReadWebState(w.statePath)
	if err != nil {
		return config.WebLiveState{}
	}
	return state.Live
}

// livePublicText is the "public" line: the URL GitHub posts to, or why there
// is none yet.
func livePublicText(state config.WebLiveState) string {
	switch {
	case state.TunnelError != "":
		return "unknown: " + state.TunnelError
	case state.PublicURL != "":
		return state.PublicURL
	}
	return "waiting for the tunnel"
}

// printLiveRepositories is the live part of bonsai web status: each live
// repository's hook state and its last ping or delivery.
func (w *webCLI) printLiveRepositories(cfg config.WebConfig, state config.WebLiveState) error {
	repos := cfg.Updates.Live.Repositories
	fmt.Fprintln(w.out)
	if len(repos) == 0 {
		fmt.Fprintln(w.out, "No repositories use live updates yet. Choose them with: bonsai web setup")
		return nil
	}
	tw := tabwriter.NewWriter(w.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tLIVE\tLAST EVENT\tDETAIL")
	now := time.Now()
	for _, repo := range repos {
		_, r, _ := state.Repository(repo)
		status := r.State
		if status == "" {
			status = "pending"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", repo, status, lastLiveEvent(r, now), r.LastError)
	}
	return tw.Flush()
}

func lastLiveEvent(r config.WebLiveRepository, now time.Time) string {
	kind, at := "delivery", r.LastDeliveryAt
	if r.LastPingAt.After(at) {
		kind, at = "ping", r.LastPingAt
	}
	if at.IsZero() {
		return "none yet"
	}
	return kind + " " + humanAgo(now.Sub(at))
}

func humanAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " days ago"
}
