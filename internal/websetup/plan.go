package websetup

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

// Running describes the bonsai web stack that is up right now.
type Running struct {
	APIPort int
	Hosted  bool // the API allows the hosted app's origin
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
		if len(reasons) > 0 {
			p.RestartAPI = true
			p.RestartReason = strings.Join(reasons, " and ") + " changed"
		}
	}
	return p
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
	if b.Updates.Mode != a.Updates.Mode {
		out = append(out, Change{Label: "Updates", From: b.Updates.Mode, To: a.Updates.Mode})
	}
	return out
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
	if cfg.Updates.Mode == config.WebUpdatesLive {
		return "Live (available in a later version; standard for now)"
	}
	return "Standard · checks GitHub every ~" + interval
}
