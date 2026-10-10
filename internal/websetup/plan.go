package websetup

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// Running describes the bonsai web stack that is up right now.
type Running struct {
	APIPort int
	Hosted  bool // the API allows the hosted app's origin
	// WebhookPort is the live-updates receiver's port, 0 without one.
	WebhookPort int
}

// What Apply does to one repository's GitHub webhook.
const (
	HookAdd     = "add"
	HookRepoint = "repoint"
	HookRemove  = "remove"
)

// HookChange is one GitHub webhook change the Review screen lists. Adds and
// re-points are made by the running API; removals by Apply itself.
type HookChange struct {
	Repo string // owner/name
	Kind string
}

// Plan is what Apply does, computed from the draft the user confirmed.
type Plan struct {
	// Config is web.json as it will be written; its SetupVersion is current.
	Config config.WebConfig
	// AddRoots are folders to add as project roots (canonical paths).
	AddRoots []string
	// RemoveRoots are project roots the user unchecked.
	RemoveRoots []config.ProjectRoot
	// SelectRepos are project IDs to show in Bonsai: every repository found
	// in a newly added folder. Existing selections are kept.
	SelectRepos []string
	// Start makes Apply start bonsai web (or reuse it, applying the new
	// settings). Set on the first run.
	Start bool
	// RestartAPI makes Apply restart the running API with the new settings.
	// Only a changed port or hosted-app access needs it; the API picks up
	// project roots and selections by itself.
	RestartAPI bool
	// RestartReason says why, for the Review screen.
	RestartReason string
	// RestartPort is the port the restarted API listens on: the new port
	// when the user changed it, otherwise the one it runs on now (which
	// may be a `bonsai web --port N` override).
	RestartPort int
	// Hooks are the GitHub webhook changes, in repository order.
	Hooks []HookChange
	// RotateSecret makes Apply write a new webhook secret before the API
	// restarts; the API then sends it to every hook.
	RotateSecret bool
	// TunnelStarts is set when Apply starts a tunnel or replaces it.
	TunnelStarts bool
	// NewAddress is set when the public URL GitHub posts to changes, so
	// re-pointed hooks get a new address (not only a new secret).
	NewAddress bool
	// WaitLive makes Apply follow live updates until every hook answered
	// its ping (or a timeout).
	WaitLive bool
}

// BuildPlan compares the draft the setup started from with the one the user
// confirmed. running is nil when bonsai web is not running.
func BuildPlan(before, after Draft, running *Running, firstRun bool) Plan {
	p := Plan{Config: after.Config, Start: firstRun}
	p.Config.SetupVersion = config.WebSetupVersion
	for _, f := range after.Folders {
		switch {
		case f.Checked && !f.Existing():
			p.AddRoots = append(p.AddRoots, f.Path)
			for _, r := range f.Repos {
				p.SelectRepos = append(p.SelectRepos, r.ID)
			}
		case !f.Checked && f.Existing():
			p.RemoveRoots = append(p.RemoveRoots, config.ProjectRoot{ID: f.RootID, Path: f.Path})
		}
	}
	p.SelectRepos = unique(p.SelectRepos)
	if running != nil && !firstRun {
		var reasons []string
		// A stack started with `bonsai web --port N` keeps running on N
		// until the user picks a port here.
		if after.Config.APIPort != before.Config.APIPort && after.Config.APIPort != running.APIPort {
			reasons = append(reasons, "port")
		}
		if after.Config.Interfaces.Hosted != running.Hosted {
			reasons = append(reasons, "hosted app access")
		}
		switch {
		case running.WebhookPort != webhookPort(after.Config):
			reasons = append(reasons, "live updates")
		case isLive(after.Config) && !slices.Equal(tunnelArgv(before.Config), tunnelArgv(after.Config)):
			reasons = append(reasons, "tunnel")
		}
		if after.RotateSecret && isLive(after.Config) {
			reasons = append(reasons, "webhook secret")
		}
		if len(reasons) > 0 {
			p.RestartAPI = true
			p.RestartReason = joinReasons(reasons) + " changed"
			p.RestartPort = running.APIPort
			if after.Config.APIPort != before.Config.APIPort {
				p.RestartPort = after.Config.APIPort
			}
		}
	}
	p.RotateSecret = after.RotateSecret
	// A new secret alone restarts only the API; anything else restarts the
	// whole stack, tunnel included.
	stackRestarts := firstRun || (p.RestartAPI && p.RestartReason != "webhook secret changed")
	planLive(&p, before.Config, after.Config, stackRestarts)
	return p
}

// planLive fills in the live-updates part of the plan: which hooks are
// added, re-pointed or removed, and whether a tunnel starts.
func planLive(p *Plan, before, after config.WebConfig, stackRestarts bool) {
	was, now := LiveRepositories(before), LiveRepositories(after)
	firstRun := p.Start
	// The public URL changes with the tunnel, and a quick tunnel gets a new
	// one whenever it restarts; a rotated secret is sent again.
	p.NewAddress = !sameAddress(before, after) || (stackRestarts && after.Updates.Live.Tunnel == webtunnel.CloudflaredQuick)
	for _, repo := range now {
		switch {
		case !containsFold(was, repo):
			p.Hooks = append(p.Hooks, HookChange{Repo: repo, Kind: HookAdd})
		case p.NewAddress || p.RotateSecret:
			p.Hooks = append(p.Hooks, HookChange{Repo: repo, Kind: HookRepoint})
		}
	}
	for _, repo := range was {
		if !containsFold(now, repo) {
			p.Hooks = append(p.Hooks, HookChange{Repo: repo, Kind: HookRemove})
		}
	}
	if !isLive(after) {
		return
	}
	argv := tunnelArgv(after)
	p.TunnelStarts = argv != nil && (firstRun || !slices.Equal(tunnelArgv(before), argv))
	p.WaitLive = len(now) > 0 && (firstRun || p.RestartAPI || len(p.HookRepos(HookAdd))+len(p.HookRepos(HookRepoint)) > 0)
}

// LiveRepositories are the repositories live updates cover with cfg: none
// while updates are standard.
func LiveRepositories(cfg config.WebConfig) []string {
	if !isLive(cfg) {
		return nil
	}
	return cfg.Updates.Live.Repositories
}

func isLive(cfg config.WebConfig) bool { return cfg.Updates.Mode == config.WebUpdatesLive }

func webhookPort(cfg config.WebConfig) int {
	if !isLive(cfg) {
		return 0
	}
	return cfg.Updates.Live.WebhookPort
}

// tunnelArgv is the tunnel cfg runs, nil for none (or an invalid one).
func tunnelArgv(cfg config.WebConfig) []string {
	if !isLive(cfg) {
		return nil
	}
	argv, err := webtunnel.Argv(cfg.Updates.Live.TunnelOptions())
	if err != nil {
		return nil
	}
	return argv
}

// sameAddress reports whether the public URL GitHub posts to stays the same.
func sameAddress(before, after config.WebConfig) bool {
	b, a := before.Updates.Live, after.Updates.Live
	return isLive(before) == isLive(after) && b.Tunnel == a.Tunnel && b.TunnelName == a.TunnelName &&
		b.PublicURL == a.PublicURL && slices.Equal(b.Command, a.Command) && b.URLPattern == a.URLPattern
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// joinReasons is "a", "a and b" or "a, b and c".
func joinReasons(reasons []string) string {
	if len(reasons) <= 2 {
		return strings.Join(reasons, " and ")
	}
	return strings.Join(reasons[:len(reasons)-1], ", ") + " and " + reasons[len(reasons)-1]
}

// HookRepos lists the repositories of the hook changes of kind.
func (p Plan) HookRepos(kind string) []string {
	var out []string
	for _, h := range p.Hooks {
		if h.Kind == kind {
			out = append(out, h.Repo)
		}
	}
	return out
}

func unique(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ProjectsChanged reports whether the plan touches project roots.
func (p Plan) ProjectsChanged() bool {
	return len(p.AddRoots) > 0 || len(p.RemoveRoots) > 0 || len(p.SelectRepos) > 0
}

// Change is one line of the Review screen's diff.
type Change struct {
	Label, From, To string
}

// Diff lists what differs between two drafts, in screen order.
func Diff(before, after Draft, home string) []Change {
	var out []Change
	var added, removed []string
	for _, f := range after.Folders {
		switch {
		case f.Checked && !f.Existing():
			added = append(added, "+ "+TildePath(home, f.Path))
		case !f.Checked && f.Existing():
			removed = append(removed, "− "+TildePath(home, f.Path))
		}
	}
	if len(added)+len(removed) > 0 {
		out = append(out, Change{Label: "Folders", To: strings.Join(append(added, removed...), "  ")})
	}
	b, a := before.Config, after.Config
	if b.Interfaces != a.Interfaces {
		out = append(out, Change{Label: "Open in", From: InterfacesText(b.Interfaces), To: InterfacesText(a.Interfaces)})
	}
	if b.OpenBrowser != a.OpenBrowser {
		out = append(out, Change{Label: "Auto-open", From: onOff(b.OpenBrowser), To: onOff(a.OpenBrowser)})
	}
	if b.APIPort != a.APIPort {
		out = append(out, Change{Label: "Port", From: strconv.Itoa(b.APIPort), To: strconv.Itoa(a.APIPort)})
	}
	if UpdatesText(b) != UpdatesText(a) {
		out = append(out, Change{Label: "Updates", From: UpdatesText(b), To: UpdatesText(a)})
	}
	if isLive(a) {
		var repos []string
		for _, r := range a.Updates.Live.Repositories {
			if !containsFold(LiveRepositories(b), r) {
				repos = append(repos, "+ "+r)
			}
		}
		for _, r := range LiveRepositories(b) {
			if !containsFold(a.Updates.Live.Repositories, r) {
				repos = append(repos, "− "+r)
			}
		}
		if len(repos) > 0 {
			out = append(out, Change{Label: "Live repos", To: strings.Join(repos, "  ")})
		}
		if b.Updates.Live.WebhookPort != a.Updates.Live.WebhookPort {
			out = append(out, Change{Label: "Webhook port", From: strconv.Itoa(b.Updates.Live.WebhookPort), To: strconv.Itoa(a.Updates.Live.WebhookPort)})
		}
	}
	if after.RotateSecret {
		out = append(out, Change{Label: "Secret", To: "new webhook secret"})
	}
	return out
}

// UpdatesText names the update mode: "standard" or "live · <tunnel>".
func UpdatesText(cfg config.WebConfig) string {
	if !isLive(cfg) {
		return "standard"
	}
	return "live · " + TunnelText(cfg.Updates.Live.Tunnel)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// InterfacesText names where Bonsai opens.
func InterfacesText(i config.WebInterfaces) string {
	switch {
	case i.Local && i.Hosted:
		return "this computer + hosted app"
	case i.Hosted:
		return "hosted app only"
	default:
		return "this computer"
	}
}

// OpenInSummary is the Review and dashboard line for "Open in".
func OpenInSummary(cfg config.WebConfig) string {
	text := InterfacesText(cfg.Interfaces)
	if cfg.Interfaces.Local {
		text += " · http://127.0.0.1:" + strconv.Itoa(cfg.APIPort)
	}
	return text + " · auto-open " + onOff(cfg.OpenBrowser)
}

// UpdatesSummary is the Review and dashboard line for "Updates". interval is
// the standard polling interval in words ("2 min").
func UpdatesSummary(cfg config.WebConfig, interval string) string {
	if !isLive(cfg) {
		return "Standard · checks GitHub every ~" + interval
	}
	repos := "no repos picked"
	switch n := len(cfg.Updates.Live.Repositories); n {
	case 0:
	case 1:
		repos = "1 repo"
	default:
		repos = strconv.Itoa(n) + " repos"
	}
	return "Live · " + TunnelText(cfg.Updates.Live.Tunnel) + " · " + repos
}

// ExternalChanges are the Review lines for everything Apply changes outside
// this computer, in the order it happens; empty when nothing does.
// installID marks this machine's hooks ("" before the first live start).
func ExternalChanges(p Plan, installID string) []string {
	var out []string
	live := p.Config.Updates.Live
	if p.TunnelStarts {
		out = append(out, "Starts a "+TunnelText(live.Tunnel)+" that forwards only Bonsai's change receiver (127.0.0.1:"+strconv.Itoa(live.WebhookPort)+") to the internet.")
	}
	if isLive(p.Config) && live.Tunnel == webtunnel.ExternalURL && len(p.Hooks) > 0 {
		out = append(out, "GitHub posts to "+PublicHostText(live)+"; your proxy must forward it to 127.0.0.1:"+strconv.Itoa(live.WebhookPort)+".")
	}
	if repos := p.HookRepos(HookAdd); len(repos) > 0 {
		marker := "?" + "bonsai=<this computer's id>"
		if installID != "" {
			marker = "?bonsai=" + installID
		}
		out = append(out, "Adds a GitHub webhook to "+strings.Join(repos, ", ")+". Their admins see a webhook to "+PublicHostText(live)+" marked "+marker+".")
	}
	if repos := p.HookRepos(HookRepoint); len(repos) > 0 {
		switch {
		case p.NewAddress && p.RotateSecret:
			out = append(out, "Points the webhook of "+strings.Join(repos, ", ")+" at the new address and secret.")
		case p.NewAddress:
			out = append(out, "Points the webhook of "+strings.Join(repos, ", ")+" at the new address.")
		default:
			out = append(out, "Sends the new secret to the webhook of "+strings.Join(repos, ", ")+".")
		}
	}
	if repos := p.HookRepos(HookRemove); len(repos) > 0 {
		out = append(out, "Deletes Bonsai's webhook from "+strings.Join(repos, ", ")+".")
	}
	return out
}
