package websetup

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// Every sentence the setup screens show about a choice lives here, so the
// copy rules (second person, present tense, no jargon in headlines, every
// problem with one fix) can be reviewed in one place.

// Choice is one option on an option screen, with its explanation panel.
type Choice struct {
	ID    string
	Label string // the list row
	Title string // the explanation panel's heading
	Lead  string // one or two sentences: what you get
	// Facts are the "You need / Setup / Delay / Exposes / Note" rows.
	Facts [][2]string
	// Pros are ✓ lines (used by the "Where to open" screen).
	Pros []string
}

const (
	OpenLocal  = "local"
	OpenHosted = "hosted"
)

// OpenChoices are the "Where do you want to open Bonsai?" options.
func OpenChoices(localURL, hostedURL string) []Choice {
	return []Choice{
		{
			ID:    OpenLocal,
			Label: "This computer (recommended)",
			Title: "This computer",
			Lead:  "Opens " + localURL,
			Pros: []string{
				"No browser permission prompt",
				"Always matches your bonsai version",
				"Works offline",
			},
		},
		{
			ID:    OpenHosted,
			Label: "Hosted app + this computer",
			Title: "Hosted app + this computer",
			Lead:  "Opens " + localURL + ". " + hostedURL + " also works while bonsai web runs.",
			Facts: [][2]string{
				{"Note", "Your browser asks to allow access to this computer the first time."},
				{"", "After a web release it may ask you to update bonsai."},
				{"Exposes", "nothing to the internet; pages from the hosted app may call this computer through your browser"},
			},
		},
	}
}

// Update choices. The IDs of the Live options are the tunnel presets of
// web.json (updates.live.tunnel).
const (
	UpdatesStandard         = "standard"
	UpdatesCloudflaredQuick = webtunnel.CloudflaredQuick
	UpdatesCloudflaredNamed = webtunnel.CloudflaredNamed
	UpdatesNgrok            = webtunnel.Ngrok
	UpdatesTailscale        = webtunnel.Tailscale
	UpdatesExternalURL      = webtunnel.ExternalURL
	UpdatesCustom           = webtunnel.Custom
)

// webhookExplained is the one place "webhook" is explained.
const webhookExplained = "GitHub notifies Bonsai the moment a PR, push or check changes (GitHub's change notifications, also called webhooks)."

const receiverOnly = "only Bonsai's change receiver; unsigned requests are rejected"

// UpdateChoices are the "How should GitHub changes reach you?" options.
// interval is the standard polling interval in words ("2 min").
func UpdateChoices(interval string) []Choice {
	live := func(id, label, title, need, setup, note string) Choice {
		c := Choice{
			ID: id, Label: "Live · " + label, Title: "Live · " + title,
			Lead: webhookExplained,
			Facts: [][2]string{
				{"You need", need},
				{"", "admin rights on the repos you pick"},
				{"Setup", setup},
				{"Delay", "~1 s"},
				{"Exposes", receiverOnly},
			},
		}
		if note != "" {
			c.Facts = append(c.Facts, [2]string{"Note", note})
		}
		return c
	}
	return []Choice{
		{
			ID: UpdatesStandard, Label: "Standard (recommended)", Title: "Standard",
			Lead: "Bonsai checks GitHub every ~" + interval + " while the app is open.",
			Facts: [][2]string{
				{"You need", "nothing"},
				{"Setup", "○○○ none"},
				{"Delay", "up to ~" + interval},
				{"Exposes", "nothing"},
			},
		},
		live(UpdatesCloudflaredQuick, "Cloudflare quick", "Cloudflare quick tunnel", "cloudflared", "●○○ one step, no account",
			"The public address changes each time bonsai web starts. Bonsai re-points your GitHub notifications automatically."),
		live(UpdatesCloudflaredNamed, "Cloudflare + domain", "Cloudflare tunnel with your domain", "cloudflared, a Cloudflare account and a domain", "●●○ a named tunnel", ""),
		live(UpdatesNgrok, "ngrok", "ngrok", "ngrok and a free ngrok account (static domain)", "●●○ an authtoken", ""),
		live(UpdatesTailscale, "Tailscale Funnel", "Tailscale Funnel", "Tailscale with Funnel enabled", "●●○ enable Funnel", ""),
		live(UpdatesExternalURL, "my own URL", "your own URL", "a reverse proxy you run", "●●● your proxy",
			"Bonsai starts no tunnel; your proxy forwards to the receiver."),
		live(UpdatesCustom, "custom command", "a custom command", "a command that prints a public URL", "●●● your command", ""),
	}
}

// CompareMatrix is the `?` table on the Updates screen.
func CompareMatrix(interval string) [][]string {
	return [][]string{
		{"", "Setup", "Account", "Stable URL", "Delay"},
		{"Standard", "none", "—", "—", "~" + interval},
		{"Cloudflare quick", "●○○", "none", "no (auto)", "~1 s"},
		{"Cloudflare+domain", "●●○", "Cloudflare+domain", "yes", "~1 s"},
		{"ngrok", "●●○", "ngrok (free)", "yes", "~1 s"},
		{"Tailscale Funnel", "●●○", "Tailscale", "yes", "~1 s"},
		{"My own URL", "●●●", "your proxy", "yes", "~1 s"},
		{"Custom command", "●●●", "yours", "yours", "~1 s"},
	}
}

// TunnelText names a live-updates tunnel preset in words.
func TunnelText(preset string) string {
	switch preset {
	case UpdatesCloudflaredQuick:
		return "Cloudflare quick tunnel"
	case UpdatesCloudflaredNamed:
		return "Cloudflare tunnel"
	case UpdatesNgrok:
		return "ngrok"
	case UpdatesTailscale:
		return "Tailscale Funnel"
	case UpdatesExternalURL:
		return "your own URL"
	case UpdatesCustom:
		return "custom command"
	}
	return preset
}

// PublicHostText is where GitHub's notifications go, as the Review screen
// tells repository admins: the host when it is known in advance, otherwise
// what kind of address the tunnel gets.
func PublicHostText(l config.WebLiveUpdates) string {
	if l.PublicURL != "" {
		if public, err := webtunnel.NormalizePublicURL(l.PublicURL); err == nil {
			if u, err := url.Parse(public); err == nil {
				return u.Hostname()
			}
		}
	}
	switch l.Tunnel {
	case UpdatesCloudflaredQuick:
		return "a trycloudflare.com address"
	case UpdatesNgrok:
		return "an ngrok address"
	case UpdatesTailscale:
		return "your ts.net address"
	}
	return "your tunnel's address"
}

// Live settings each tunnel asks for on the screen after Updates.
const (
	FieldTunnelName = "tunnel_name"
	FieldPublicURL  = "public_url"
	FieldCommand    = "command"
	FieldURLPattern = "url_pattern"
)

// LiveField is one text input of the tunnel settings screen.
type LiveField struct {
	ID          string
	Label       string
	Placeholder string
	Help        string
	Optional    bool
}

// LiveFields are the inputs preset needs; none for the quick tunnel and
// Tailscale. port is the receiver's port, for the help text.
func LiveFields(preset string, port int) []LiveField {
	receiver := "http://127.0.0.1:" + strconv.Itoa(port) + webhooks.LivePath
	switch preset {
	case UpdatesCloudflaredNamed:
		return []LiveField{
			{ID: FieldTunnelName, Label: "Tunnel name", Placeholder: "bonsai",
				Help: "The tunnel from: cloudflared tunnel create <name>. Bonsai points it at the receiver (127.0.0.1:" + strconv.Itoa(port) + "); ingress rules in your own cloudflared config take precedence."},
			{ID: FieldPublicURL, Label: "Public URL", Placeholder: "https://hooks.example.com",
				Help: "The hostname routed to the tunnel: cloudflared tunnel route dns <name> <hostname>."},
		}
	case UpdatesNgrok:
		return []LiveField{
			{ID: FieldPublicURL, Label: "Static domain", Placeholder: "your-name.ngrok-free.app", Optional: true,
				Help: "Optional. Your free static domain (ngrok dashboard → Domains) keeps the address stable; without it the address changes on every start and Bonsai re-points your notifications."},
		}
	case UpdatesExternalURL:
		return []LiveField{
			{ID: FieldPublicURL, Label: "Public URL", Placeholder: "https://hooks.example.com",
				Help: "Your proxy forwards https://<host>" + webhooks.LivePath + " to " + receiver + " and nothing else."},
		}
	case UpdatesCustom:
		return []LiveField{
			{ID: FieldCommand, Label: "Command", Placeholder: "mytunnel http 127.0.0.1:{port}",
				Help: "{port} becomes the receiver's port. Arguments are split on spaces; no shell runs it."},
			{ID: FieldURLPattern, Label: "URL pattern", Placeholder: `https://[a-z0-9-]+\.example\.dev`,
				Help: "A regular expression that finds the public URL in the command's output."},
		}
	}
	return nil
}

// CheckLiveField explains what is wrong with the text typed into f, or
// returns "".
func CheckLiveField(f LiveField, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if f.Optional {
			return ""
		}
		return f.Label + " is required."
	}
	switch f.ID {
	case FieldTunnelName:
		if !webtunnel.ValidTunnelName(value) {
			return "Use letters, digits, '.', '_' or '-' for the tunnel name (at most 64)."
		}
	case FieldPublicURL:
		if _, err := webtunnel.NormalizePublicURL(value); err != nil {
			return f.Label + " must be an https:// address with no path, for example https://hooks.example.com."
		}
	case FieldCommand:
		if !strings.Contains(value, webtunnel.PortPlaceholder) {
			return "The command must contain " + webtunnel.PortPlaceholder + ", where the receiver's port goes."
		}
	case FieldURLPattern:
		if _, err := regexp.Compile(value); err != nil {
			return "The URL pattern is not a valid regular expression: " + err.Error()
		}
	}
	return ""
}

// LiveFieldValue is the current text of field id.
func LiveFieldValue(l config.WebLiveUpdates, id string) string {
	switch id {
	case FieldTunnelName:
		return l.TunnelName
	case FieldPublicURL:
		return l.PublicURL
	case FieldCommand:
		return strings.Join(l.Command, " ")
	case FieldURLPattern:
		return l.URLPattern
	}
	return ""
}

// SetLiveField stores the text typed into field id.
func SetLiveField(l *config.WebLiveUpdates, id, value string) {
	value = strings.TrimSpace(value)
	switch id {
	case FieldTunnelName:
		l.TunnelName = value
	case FieldPublicURL:
		l.PublicURL = value
		if public, err := webtunnel.NormalizePublicURL(value); err == nil {
			l.PublicURL = public
		}
	case FieldCommand:
		l.Command = strings.Fields(value)
	case FieldURLPattern:
		l.URLPattern = value
	}
}

// LiveRepo is one row of the live repositories picker.
type LiveRepo struct {
	FullName string // owner/name on GitHub
	Local    string // the local repository it was found in, "" when none
	Admin    bool
	// Err says why the repository cannot be checked or picked.
	Err string
}

// Live repositories picker copy.
const (
	LiveReposLead = "Pick the repositories that get live updates. Bonsai adds one webhook (GitHub's change notifications) to each; GitHub only lets admins do that."
	NeedsAdmin    = "needs admin, stays on Standard"
)

// Screen copy shared by the wizard and edit mode.
const (
	WelcomeLead     = "See every repo, worktree, pull request and CI run in your browser.\nEverything runs on this computer."
	WelcomeTime     = "Takes about a minute. Change anything later with:  bonsai web setup"
	GitHubLead      = "Bonsai uses your existing gh login. It never stores your token."
	GitHubWithout   = "Without it you still get worktrees, branches, commits and processes."
	ProjectsPickTip = "Repos in the folders you check show up in Bonsai. Pick single repos later in the browser: Settings → Projects."
	NothingExternal = "Nothing changes outside this computer."
)
