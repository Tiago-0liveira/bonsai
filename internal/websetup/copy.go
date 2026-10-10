package websetup

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
	// Available is false for options a later version enables.
	Available bool
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
			Available: true,
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
			Available: true,
		},
	}
}

// Update choices. The IDs of the Live options are the tunnel presets the
// live-updates phase reads from web.json (updates.live.tunnel).
const (
	UpdatesStandard         = "standard"
	UpdatesCloudflaredQuick = "cloudflared-quick"
	UpdatesCloudflaredNamed = "cloudflared-named"
	UpdatesNgrok            = "ngrok"
	UpdatesTailscale        = "tailscale"
	UpdatesExternalURL      = "external-url"
	UpdatesCustom           = "custom"
)

// LaterVersion marks options that are visible but not selectable yet.
const LaterVersion = "available in a later version"

// webhookExplained is the one place "webhook" is explained.
const webhookExplained = "GitHub notifies Bonsai the moment a PR, push or check changes (GitHub's change notifications, also called webhooks)."

const receiverOnly = "only Bonsai's change receiver; unsigned requests are rejected"

// UpdateChoices are the "How should GitHub changes reach you?" options.
// interval is the standard polling interval in words ("2 min").
func UpdateChoices(interval string) []Choice {
	live := func(id, label, title, need, setup, exposes, note string) Choice {
		c := Choice{
			ID: id, Label: "Live · " + label, Title: "Live · " + title,
			Lead: webhookExplained,
			Facts: [][2]string{
				{"You need", need},
				{"", "admin rights on the repos you pick"},
				{"Setup", setup},
				{"Delay", "~1 s"},
				{"Exposes", exposes},
			},
		}
		if note != "" {
			c.Facts = append(c.Facts, [2]string{"Note", note})
		}
		c.Facts = append(c.Facts, [2]string{"Status", LaterVersion})
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
			Available: true,
		},
		live(UpdatesCloudflaredQuick, "Cloudflare quick", "Cloudflare quick tunnel", "cloudflared", "●○○ one step, no account", receiverOnly,
			"The public address changes each time bonsai web starts. Bonsai re-points your GitHub notifications automatically."),
		live(UpdatesCloudflaredNamed, "Cloudflare + domain", "Cloudflare tunnel with your domain", "cloudflared, a Cloudflare account and a domain", "●●○ a named tunnel", receiverOnly, ""),
		live(UpdatesNgrok, "ngrok", "ngrok", "ngrok and a free ngrok account (static domain)", "●●○ an authtoken", receiverOnly, ""),
		live(UpdatesTailscale, "Tailscale Funnel", "Tailscale Funnel", "Tailscale with Funnel enabled", "●●○ enable Funnel", receiverOnly, ""),
		live(UpdatesExternalURL, "my own URL", "your own URL", "a reverse proxy you run", "●●● your proxy", receiverOnly, ""),
		live(UpdatesCustom, "custom command", "a custom command", "a command that prints a public URL", "●●● your command", receiverOnly, ""),
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
	}
}

// Screen copy shared by the wizard and edit mode.
const (
	WelcomeLead     = "See every repo, worktree, pull request and CI run in your browser.\nEverything runs on this computer."
	WelcomeTime     = "Takes about a minute. Change anything later with:  bonsai web setup"
	GitHubLead      = "Bonsai uses your existing gh login. It never stores your token."
	GitHubWithout   = "Without it you still get worktrees, branches, commits and processes."
	ProjectsPickTip = "Repos in the folders you check show up in Bonsai. Pick single repos later in the browser: Settings → Projects."
	NothingExternal = "Nothing changes outside this computer."
)
