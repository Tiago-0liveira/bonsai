package websetup

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

func root(path string) config.ProjectRoot {
	return config.ProjectRoot{ID: config.PathID("root", path), Path: path}
}

func TestNewDraftFirstRun(t *testing.T) {
	d := NewDraft(config.DefaultWebConfig(), false, nil, []string{"/h/code", "/h/dev"}, "/h/bonsai")
	if d.Config.Interfaces != (config.WebInterfaces{Local: true}) {
		t.Fatalf("a brand-new user gets this computer only, got %+v", d.Config.Interfaces)
	}
	if len(d.Folders) != 3 || !d.Folders[0].ThisRepo || d.Folders[0].Path != "/h/bonsai" {
		t.Fatalf("folders = %+v", d.Folders)
	}
	for _, f := range d.Folders {
		if !f.Checked {
			t.Fatalf("first run preselects every suggestion: %+v", f)
		}
	}
	// A scan that finds nothing unchecks an untouched suggestion...
	d.SetScan(2, nil, "")
	if d.Folders[2].Checked {
		t.Fatal("empty suggestion stayed checked")
	}
	// ...but never this repo, and never a row the user toggled.
	d.SetScan(0, nil, "")
	if !d.Folders[0].Checked {
		t.Fatal("this repo got unchecked")
	}
	d.Toggle(1)
	d.Toggle(1)
	d.SetScan(1, nil, "")
	if !d.Folders[1].Checked {
		t.Fatal("scan overrode the user's choice")
	}
}

func TestNewDraftExistingUser(t *testing.T) {
	cfg := config.DefaultWebConfig() // hosted on
	d := NewDraft(cfg, true, []config.ProjectRoot{root("/h/code")}, []string{"/h/code", "/h/code/inner", "/h/dev"}, "/h/code/repo")
	if !d.Config.Interfaces.Hosted {
		t.Fatal("existing users keep their saved interfaces")
	}
	if len(d.Folders) != 2 || !d.Folders[0].Existing() || d.Folders[1].Path != "/h/dev" {
		t.Fatalf("roots first, covered suggestions and repo skipped: %+v", d.Folders)
	}
	if d.Folders[1].Checked {
		t.Fatal("suggestions are not preselected once roots exist")
	}
}

func TestBuildPlanFirstRun(t *testing.T) {
	before := NewDraft(config.DefaultWebConfig(), false, nil, []string{"/h/code"}, "/h/bonsai")
	after := before.Clone()
	after.SetScan(0, []Repo{{ID: "p1"}}, "")
	after.SetScan(1, []Repo{{ID: "p2"}, {ID: "p3"}, {ID: "p1"}}, "")
	p := BuildPlan(before, after, nil, true)
	if !p.Start || p.RestartAPI || p.Config.SetupVersion != config.WebSetupVersion {
		t.Fatalf("plan = %+v", p)
	}
	if !reflect.DeepEqual(p.AddRoots, []string{"/h/bonsai", "/h/code"}) || !reflect.DeepEqual(p.SelectRepos, []string{"p1", "p2", "p3"}) {
		t.Fatalf("roots %v repos %v", p.AddRoots, p.SelectRepos)
	}
	if before.Folders[0].Scanned {
		t.Fatal("Clone shares folders with the original")
	}
}

func TestBuildPlanTargetedRestart(t *testing.T) {
	cfg := config.DefaultWebConfig()
	before := NewDraft(cfg, true, []config.ProjectRoot{root("/h/code"), root("/h/old")}, []string{"/h/work"}, "")
	running := &Running{APIPort: cfg.APIPort, Hosted: true}

	cases := []struct {
		name    string
		edit    func(*Draft)
		restart bool
		reason  string
	}{
		{"nothing", func(*Draft) {}, false, ""},
		{"roots only", func(d *Draft) { d.Toggle(1); d.Toggle(2) }, false, ""},
		{"auto-open only", func(d *Draft) { d.Config.OpenBrowser = false }, false, ""},
		{"port", func(d *Draft) { d.Config.APIPort = 7011 }, true, "port changed"},
		{"hosted", func(d *Draft) { d.Config.Interfaces.Hosted = false }, true, "hosted app access changed"},
		{"both", func(d *Draft) { d.Config.APIPort = 7011; d.Config.Interfaces.Hosted = false }, true, "port and hosted app access changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := before.Clone()
			tc.edit(&after)
			p := BuildPlan(before, after, running, false)
			if p.RestartAPI != tc.restart || p.RestartReason != tc.reason || p.Start {
				t.Fatalf("plan = %+v", p)
			}
			if BuildPlan(before, after, nil, false).RestartAPI {
				t.Fatal("nothing restarts when bonsai web is not running")
			}
		})
	}

	// Running on a --port override: leaving the port alone restarts nothing.
	override := &Running{APIPort: 17931, Hosted: true}
	if p := BuildPlan(before, before.Clone(), override, false); p.RestartAPI {
		t.Fatalf("untouched port restarted a --port stack: %+v", p)
	}
	hostedOnly := before.Clone()
	hostedOnly.Config.Interfaces.Hosted = false
	if p := BuildPlan(before, hostedOnly, override, false); !p.RestartAPI || p.RestartPort != 17931 {
		t.Fatalf("a hosted-only change must restart on the running port: %+v", p)
	}
	newPort := before.Clone()
	newPort.Config.APIPort = 7011
	if p := BuildPlan(before, newPort, override, false); p.RestartPort != 7011 {
		t.Fatalf("a port change restarts on the new port: %+v", p)
	}

	after := before.Clone()
	after.Toggle(1) // remove /h/old
	after.Toggle(2) // add /h/work
	p := BuildPlan(before, after, running, false)
	if !reflect.DeepEqual(p.AddRoots, []string{"/h/work"}) || len(p.RemoveRoots) != 1 || p.RemoveRoots[0].ID != config.PathID("root", "/h/old") || !p.ProjectsChanged() {
		t.Fatalf("plan = %+v", p)
	}
}

func TestDiff(t *testing.T) {
	home := filepath.FromSlash("/h")
	code, old, work := filepath.Join(home, "code"), filepath.Join(home, "old"), filepath.Join(home, "work")
	before := NewDraft(config.DefaultWebConfig(), true, []config.ProjectRoot{root(code), root(old)}, []string{work}, "")
	after := before.Clone()
	after.Toggle(1)
	after.Toggle(2)
	after.Config.APIPort = 7011
	after.Config.Interfaces.Hosted = false
	after.Config.OpenBrowser = false
	got := Diff(before, after, home)
	sep := string(filepath.Separator)
	want := []Change{
		{Label: "Folders", To: "+ ~" + sep + "work  − ~" + sep + "old"},
		{Label: "Open in", From: "this computer + hosted app", To: "this computer"},
		{Label: "Auto-open", From: "on", To: "off"},
		{Label: "Port", From: "7001", To: "7011"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff =\n%+v\nwant\n%+v", got, want)
	}
	if len(Diff(before, before.Clone(), home)) != 0 {
		t.Fatal("identical drafts differ")
	}
}

func TestSummaries(t *testing.T) {
	cfg := config.DefaultWebConfig()
	if got := OpenInSummary(cfg); got != "this computer + hosted app · http://127.0.0.1:7001 · auto-open on" {
		t.Fatal(got)
	}
	cfg.Interfaces = config.WebInterfaces{Hosted: true}
	if got := OpenInSummary(cfg); got != "hosted app only · auto-open on" {
		t.Fatal(got)
	}
	if got := UpdatesSummary(cfg, "2 min"); got != "Standard · checks GitHub every ~2 min" {
		t.Fatal(got)
	}
	home := filepath.FromSlash("/h")
	d := Draft{Folders: []Folder{
		{Path: filepath.Join(home, "code"), Checked: true, Scanned: true, Repos: []Repo{{ID: "a"}, {ID: "b"}}},
		{Path: filepath.Join(home, "bonsai"), Checked: true, Scanned: true, Repos: []Repo{{ID: "a"}}},
		{Path: filepath.Join(home, "dev"), Checked: false},
	}}
	sep := string(filepath.Separator)
	if got := ProjectsSummary(d, home); got != "~"+sep+"bonsai, ~"+sep+"code · 2 repos" {
		t.Fatal(got)
	}
	if got := TildePath(home, filepath.FromSlash("/elsewhere")); got != filepath.FromSlash("/elsewhere") {
		t.Fatal(got)
	}
}

func liveConfig(tunnel string, repos ...string) config.WebConfig {
	cfg := config.DefaultWebConfig()
	cfg.SetupVersion = config.WebSetupVersion
	cfg.Updates.Mode = config.WebUpdatesLive
	cfg.Updates.Live.Tunnel = tunnel
	cfg.Updates.Live.Repositories = repos
	return cfg
}

func hooks(p Plan) string {
	var out []string
	for _, h := range p.Hooks {
		out = append(out, h.Kind+" "+h.Repo)
	}
	return strings.Join(out, ", ")
}

func TestBuildPlanLive(t *testing.T) {
	standard := config.DefaultWebConfig()
	standard.SetupVersion = config.WebSetupVersion
	draft := func(cfg config.WebConfig) Draft { return Draft{Config: cfg} }
	idle := &Running{APIPort: 7001, Hosted: true}
	live := &Running{APIPort: 7001, Hosted: true, WebhookPort: 7002}

	// Turning live on adds hooks and starts the tunnel with a restart.
	p := BuildPlan(draft(standard), draft(liveConfig("cloudflared-quick", "o/a", "o/b")), idle, false)
	if hooks(p) != "add o/a, add o/b" || !p.TunnelStarts || !p.RestartAPI || p.RestartReason != "live updates changed" || !p.WaitLive {
		t.Fatalf("on: %+v", p)
	}
	if got := ExternalChanges(p, "abc123"); len(got) != 2 || !strings.Contains(got[0], "Starts a Cloudflare quick tunnel that forwards only Bonsai's change receiver (127.0.0.1:7002)") ||
		!strings.Contains(got[1], "Adds a GitHub webhook to o/a, o/b. Their admins see a webhook to a trycloudflare.com address marked ?bonsai=abc123.") {
		t.Fatalf("external = %q", got)
	}

	// Adding and dropping a repository: no restart, only hook changes.
	p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a", "o/b")), draft(liveConfig("cloudflared-quick", "O/A", "o/c")), live, false)
	if hooks(p) != "add o/c, remove o/b" || p.RestartAPI || p.TunnelStarts || !p.WaitLive {
		t.Fatalf("repos: %+v", p)
	}

	// A new tunnel re-points the hooks that stay.
	named := liveConfig("cloudflared-named", "o/a")
	named.Updates.Live.TunnelName, named.Updates.Live.PublicURL = "bonsai", "https://hooks.example.com"
	p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a")), draft(named), live, false)
	if hooks(p) != "repoint o/a" || !p.TunnelStarts || p.RestartReason != "tunnel changed" {
		t.Fatalf("tunnel: %+v", p)
	}
	if got := ExternalChanges(p, ""); !strings.Contains(got[len(got)-1], "Points the webhook of o/a at the new address.") {
		t.Fatalf("external = %q", got)
	}

	// A quick tunnel restarted for another reason gets a new address too.
	hosted := draft(liveConfig("cloudflared-quick", "o/a"))
	hosted.Config.Interfaces.Hosted = false
	if p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a")), hosted, live, false); hooks(p) != "repoint o/a" || p.TunnelStarts {
		t.Fatalf("restart: %+v", p)
	}

	// Rotating the secret restarts the API and re-sends it.
	rotate := draft(named)
	rotate.RotateSecret = true
	p = BuildPlan(draft(named), rotate, live, false)
	if hooks(p) != "repoint o/a" || !p.RotateSecret || p.RestartReason != "webhook secret changed" || p.TunnelStarts {
		t.Fatalf("rotate: %+v", p)
	}
	if got := ExternalChanges(p, ""); len(got) != 1 || got[0] != "Sends the new secret to the webhook of o/a." || p.NewAddress {
		t.Fatalf("external = %q", got)
	}
	// With a quick tunnel, a new secret alone keeps the tunnel (and its
	// address); a new port restarts it too.
	quickRotate := draft(liveConfig("cloudflared-quick", "o/a"))
	quickRotate.RotateSecret = true
	if p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a")), quickRotate, live, false); p.NewAddress {
		t.Fatalf("quick rotate: %+v", p)
	}
	quickRotate.Config.APIPort = 7011
	if p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a")), quickRotate, live, false); !p.NewAddress {
		t.Fatalf("quick rotate and port: %+v", p)
	}
	if got := ExternalChanges(p, ""); got[len(got)-1] != "Points the webhook of o/a at the new address and secret." {
		t.Fatalf("external = %q", got)
	}

	// Turning live off deletes every hook and stops the receiver.
	p = BuildPlan(draft(liveConfig("cloudflared-quick", "o/a", "o/b")), draft(standard), live, false)
	if hooks(p) != "remove o/a, remove o/b" || !p.RestartAPI || p.TunnelStarts || p.WaitLive {
		t.Fatalf("off: %+v", p)
	}
	if got := ExternalChanges(p, ""); len(got) != 1 || got[0] != "Deletes Bonsai's webhook from o/a, o/b." {
		t.Fatalf("external = %q", got)
	}

	// Your own URL starts no tunnel.
	external := liveConfig("external-url", "o/a")
	external.Updates.Live.PublicURL = "https://hooks.example.com"
	p = BuildPlan(draft(standard), draft(external), nil, true)
	if p.TunnelStarts || hooks(p) != "add o/a" || !p.WaitLive {
		t.Fatalf("external: %+v", p)
	}
	if got := ExternalChanges(p, ""); !strings.Contains(got[0], "GitHub posts to hooks.example.com; your proxy must forward it to 127.0.0.1:7002.") {
		t.Fatalf("external = %q", got)
	}

	if got := ExternalChanges(BuildPlan(draft(standard), draft(standard), idle, false), ""); len(got) != 0 {
		t.Fatalf("standard = %q", got)
	}
}

func TestDiffAndSummaryLive(t *testing.T) {
	before := Draft{Config: liveConfig("cloudflared-quick", "o/a", "o/b")}
	after := Draft{Config: liveConfig("ngrok", "o/a", "o/c"), RotateSecret: true}
	after.Config.Updates.Live.WebhookPort = 7012
	var got []string
	for _, c := range Diff(before, after, "") {
		got = append(got, c.Label+": "+c.From+" → "+c.To)
	}
	want := []string{
		"Updates: live · Cloudflare quick tunnel → live · ngrok",
		"Live repos:  → + o/c  − o/b",
		"Webhook port: 7002 → 7012",
		"Secret:  → new webhook secret",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff = %q", got)
	}
	if s := UpdatesSummary(after.Config, "2 min"); s != "Live · ngrok · 2 repos" {
		t.Fatalf("summary = %q", s)
	}
	if s := UpdatesSummary(liveConfig("tailscale"), "2 min"); s != "Live · Tailscale Funnel · no repos picked" {
		t.Fatalf("summary = %q", s)
	}
}

func TestLiveFields(t *testing.T) {
	if len(LiveFields("cloudflared-quick", 7002)) != 0 || len(LiveFields("tailscale", 7002)) != 0 {
		t.Fatal("quick tunnel and Tailscale ask nothing")
	}
	live := config.DefaultWebConfig().Updates.Live
	live.Tunnel = "custom"
	for _, f := range LiveFields("custom", 7002) {
		switch f.ID {
		case FieldCommand:
			SetLiveField(&live, f.ID, "  mytunnel http  127.0.0.1:{port} ")
		case FieldURLPattern:
			SetLiveField(&live, f.ID, `https://\S+`)
		}
	}
	if !reflect.DeepEqual(live.Command, []string{"mytunnel", "http", "127.0.0.1:{port}"}) || LiveFieldValue(live, FieldCommand) != "mytunnel http 127.0.0.1:{port}" {
		t.Fatalf("command = %q", live.Command)
	}
	if err := webtunnel.ValidateOptions(live.TunnelOptions()); err != nil {
		t.Fatal(err)
	}
	SetLiveField(&live, FieldPublicURL, "Hooks.Example.com")
	if live.PublicURL != "https://hooks.example.com" {
		t.Fatalf("public URL = %q", live.PublicURL)
	}
	SetLiveField(&live, FieldPublicURL, "http://plain")
	if live.PublicURL != "http://plain" {
		t.Fatal("an invalid URL is kept as typed, for the error to name it")
	}
}
