package websetup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

var home = filepath.FromSlash("/home/u")

func p(rel string) string { return filepath.Join(home, filepath.FromSlash(rel)) }

// fakeBackend records what the TUI asks for. Nothing it does touches disk.
type fakeBackend struct {
	mu          sync.Mutex
	checks      []checks.Check
	repos       map[string][]setup.Repo
	checkCalls  int
	plans       []setup.Plan
	opened      []string
	copied      []string
	fixes       []checks.Fix
	failApply   bool
	runningPort int
	liveRepos   []setup.LiveRepo
	liveCalls   int
}

func healthyChecks() []checks.Check {
	return []checks.Check{
		{ID: checks.IDGit, Title: "git", State: checks.OK, Detail: "2.43.0"},
		{ID: checks.IDGHInstalled, Title: "gh installed", State: checks.OK, Detail: "2.92.0"},
		{ID: checks.IDGHAuth, Title: "gh logged in", State: checks.OK, Detail: "github.com · octo"},
		{ID: checks.IDProjects, Title: "project folders", State: checks.OK, Detail: "2 folders · 15 repos found · 15 shown"},
		{ID: checks.IDPort, Title: "port 7001", State: checks.OK, Detail: "free"},
		{ID: checks.IDTunnelCloudflared, Title: "cloudflared", State: checks.Skip, Detail: "installed (2026.9.3) · not logged in (only a custom domain needs it) · only needed for live updates", Fix: &checks.Fix{Command: "cloudflared tunnel login", Inline: true}},
		{ID: checks.IDTunnelNgrok, Title: "ngrok", State: checks.Skip, Detail: "not installed · only needed for live updates", Fix: &checks.Fix{Command: "install ngrok: https://ngrok.com/download"}},
		{ID: checks.IDTunnelTailscale, Title: "tailscale", State: checks.Skip, Detail: "not installed · only needed for live updates", Fix: &checks.Fix{Command: "install Tailscale: https://tailscale.com/download"}},
	}
}

func loggedOut() []checks.Check {
	out := healthyChecks()
	out[2] = checks.Check{ID: checks.IDGHAuth, Title: "gh logged in", State: checks.Warn,
		Detail: "not logged in to github.com; without it you still get worktrees, branches, commits and processes",
		Fix:    &checks.Fix{Command: "gh auth login --hostname github.com", Inline: true}}
	return out
}

func newFake() *fakeBackend {
	repos := func(names ...string) []setup.Repo {
		var out []setup.Repo
		for _, n := range names {
			out = append(out, setup.Repo{ID: "id-" + n, Name: n})
		}
		return out
	}
	return &fakeBackend{
		checks: healthyChecks(),
		repos: map[string][]setup.Repo{
			p("bonsai"): repos("bonsai"),
			p("code"):   repos("api", "web", "cli", "docs", "infra", "site", "tools", "sdk", "app", "data", "ml", "ops", "auth", "pay"),
			p("dev"):    nil,
			p("work"):   repos("alpha", "beta", "gamma"),
			p("old"):    repos("legacy"),
		},
		liveRepos: []setup.LiveRepo{
			{FullName: "octo/bonsai", Local: "bonsai", Admin: true},
			{FullName: "octo/api", Local: "api", Admin: true},
			{FullName: "acme/web", Local: "web"},
		},
	}
}

func (f *fakeBackend) LiveRepositories(_ context.Context, _ []setup.Repo, configured []string) []setup.LiveRepo {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.liveCalls++
	out := append([]setup.LiveRepo{}, f.liveRepos...)
	for _, repo := range configured {
		found := false
		for _, r := range out {
			found = found || strings.EqualFold(r.FullName, repo)
		}
		if !found {
			out = append(out, setup.LiveRepo{FullName: repo, Err: "not in your project folders"})
		}
	}
	return out
}

func (f *fakeBackend) Checks(context.Context, config.WebConfig) []checks.Check {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkCalls++
	return append([]checks.Check{}, f.checks...)
}

func (f *fakeBackend) ScanFolder(_ context.Context, path string) (string, []setup.Repo, error) {
	path = strings.Replace(path, "~", home, 1)
	path = filepath.FromSlash(path)
	repos, ok := f.repos[path]
	if !ok {
		return "", nil, errors.New("folder " + path + " does not exist")
	}
	return path, repos, nil
}

func (f *fakeBackend) FixCommand(fix checks.Fix) *exec.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fixes = append(f.fixes, fix)
	return exec.Command("true")
}

func (f *fakeBackend) Apply(_ context.Context, plan setup.Plan, progress func(Step)) Result {
	f.mu.Lock()
	f.plans = append(f.plans, plan)
	f.mu.Unlock()
	progress(Step{ID: "save", Label: "Saved settings", State: StepDone})
	if f.failApply {
		progress(Step{ID: "start", Label: "Start Bonsai", State: StepFailed, Detail: "port 7001 is used by another program (pid 4242, node)", Fix: "bonsai web --port 7011"})
		return Result{}
	}
	progress(Step{ID: "start", Label: "Started Bonsai", State: StepDone, Detail: "http://127.0.0.1:7001"})
	r := Result{OK: true, URL: "http://127.0.0.1:7001/app", Port: 7001}
	if plan.WaitLive {
		r.WebhookPort = plan.Config.Updates.Live.WebhookPort
		progress(Step{ID: "receiver", Label: "Change receiver ready", State: StepDone, Detail: "127.0.0.1:7002"})
		progress(Step{ID: "tunnel", Label: "Tunnel address", State: StepDone, Detail: "https://quiet-river.trycloudflare.com"})
		progress(Step{ID: "hooks", Label: "GitHub webhooks", State: StepDone, Detail: "1 added"})
		progress(Step{ID: "ping", Label: "Waiting for GitHub's ping", State: StepFailed, Detail: "octo/api needs admin", Fix: "ask an admin of octo/api, or drop it in: bonsai web setup → Updates"})
		r.Notes = append(r.Notes, "1 repository is not live yet; see bonsai web status.")
	}
	if plan.Config.OpenBrowser {
		r.Opened = true
		f.opened = append(f.opened, r.URL)
	}
	return r
}

func (f *fakeBackend) Open(url string) error { f.opened = append(f.opened, url); return nil }
func (f *fakeBackend) Copy(text string) error {
	f.copied = append(f.copied, text)
	return nil
}

func testInfo() Info {
	return Info{
		Home:             home,
		ConfigPath:       p(".config/bonsai/web.json"),
		StandardInterval: "2 min",
		HostedURL:        "https://app.bonsai.dev/app",
		DiscoveryDepth:   config.ProjectDiscoveryDepth,
	}
}

func asciiRenderer() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.Ascii)
	return r
}

func firstRunDraft() setup.Draft {
	return setup.NewDraft(config.DefaultWebConfig(), false, nil, []string{p("dev"), p("code")}, p("bonsai"))
}

func editDraft() setup.Draft {
	cfg := config.DefaultWebConfig()
	cfg.SetupVersion = config.WebSetupVersion
	roots := []config.ProjectRoot{
		{ID: config.PathID("root", p("code")), Path: p("code")},
		{ID: config.PathID("root", p("old")), Path: p("old")},
	}
	return setup.NewDraft(cfg, true, roots, []string{p("work")}, "")
}

// harness drives the model the way Bubble Tea does, running commands
// synchronously. It never runs ExecProcess commands; it records them.
type harness struct {
	t       *testing.T
	m       tea.Model
	quit    bool
	execs   int
	presses int
}

func newHarness(t *testing.T, f *fakeBackend, mode Mode, d setup.Draft, info Info) *harness {
	t.Helper()
	h := &harness{t: t, m: New(f, info, mode, d, asciiRenderer())}
	h.update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.run(h.m.Init())
	return h
}

func (h *harness) model() Model { return h.m.(Model) }

func (h *harness) update(msg tea.Msg) {
	var cmd tea.Cmd
	h.m, cmd = h.m.Update(msg)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.QuitMsg:
			h.quit = true
		default:
			if strings.Contains(fmt.Sprintf("%T", msg), "exec") {
				h.execs++
				continue
			}
			var next tea.Cmd
			h.m, next = h.m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func (h *harness) keys(keys ...string) {
	for _, k := range keys {
		h.presses++
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		h.update(msg)
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) screen() screen { return h.model().current() }

func TestFirstRunDefaultsKeyBudget(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "enter", "enter", "enter")
	if h.presses > 6 {
		t.Fatalf("took %d key presses", h.presses)
	}
	if len(f.plans) != 1 {
		t.Fatalf("apply calls = %d, screen = %d", len(f.plans), h.screen())
	}
	plan := f.plans[0]
	if !plan.Start || plan.Config.SetupVersion != config.WebSetupVersion || plan.Config.Interfaces != (config.WebInterfaces{Local: true}) || !plan.Config.OpenBrowser || plan.Config.Updates.Mode != config.WebUpdatesStandard {
		t.Fatalf("plan = %+v", plan)
	}
	if strings.Join(plan.AddRoots, ",") != p("bonsai")+","+p("code") {
		t.Fatalf("roots = %v (the empty ~/dev suggestion must be dropped)", plan.AddRoots)
	}
	if len(plan.SelectRepos) != 15 {
		t.Fatalf("selected %d repos", len(plan.SelectRepos))
	}
	if len(f.opened) != 1 || f.opened[0] != "http://127.0.0.1:7001/app" {
		t.Fatalf("browser opened = %v", f.opened)
	}
	if h.screen() != screenDone || !h.model().Applied() {
		t.Fatalf("screen = %d applied = %v", h.screen(), h.model().Applied())
	}
	h.keys("enter")
	if !h.quit {
		t.Fatal("enter on Done should close the setup")
	}
}

func TestFirstRunDefaultsShortcut(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("d")
	if h.screen() != screenReview {
		t.Fatalf("d should jump to Review, got %d", h.screen())
	}
	h.keys("enter")
	if len(f.plans) != 1 || h.screen() != screenDone {
		t.Fatalf("plans = %d screen = %d", len(f.plans), h.screen())
	}
}

func TestNothingIsWrittenBeforeReview(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "space", "down", "space", "enter", "down", "space", "enter", "enter", "down", "enter", "?", "esc", "up", "enter")
	if h.screen() != screenReview {
		t.Fatalf("screen = %d", h.screen())
	}
	h.keys("esc", "esc", "ctrl+c")
	if !h.model().confirming {
		t.Fatal("ctrl+c with changes should ask first")
	}
	h.keys("n")
	if h.model().confirming || h.quit {
		t.Fatal("n should keep editing")
	}
	h.keys("ctrl+c", "y")
	if !h.quit {
		t.Fatal("y should quit")
	}
	if len(f.plans) != 0 || h.model().Applied() {
		t.Fatalf("Apply ran before Review: %d", len(f.plans))
	}
}

func TestCtrlCWithoutChangesQuitsAtOnce(t *testing.T) {
	h := newHarness(t, newFake(), Wizard, firstRunDraft(), testInfo())
	h.keys("ctrl+c")
	if !h.quit || h.model().confirming {
		t.Fatal("nothing to lose: quit without asking")
	}
}

func TestEscGoesBackOnEveryScreen(t *testing.T) {
	h := newHarness(t, newFake(), Wizard, firstRunDraft(), testInfo())
	order := []screen{screenWelcome, screenProjects, screenOpenIn, screenGitHub, screenUpdates, screenReview}
	for i := 1; i < len(order); i++ {
		h.keys("enter")
		if h.screen() != order[i] {
			t.Fatalf("enter #%d: screen %d, want %d", i, h.screen(), order[i])
		}
	}
	for i := len(order) - 2; i >= 0; i-- {
		h.keys("esc")
		if h.screen() != order[i] {
			t.Fatalf("esc: screen %d, want %d", h.screen(), order[i])
		}
	}

	e := newHarness(t, newFake(), Edit, editDraft(), testInfo())
	for i, target := range []screen{screenProjects, screenOpenIn, screenGitHub, screenUpdates, screenAdvanced} {
		e.model().cursor[screenDashboard] = i
		e.keys("enter")
		if e.screen() != target {
			t.Fatalf("dashboard row %d opened %d", i, e.screen())
		}
		e.keys("esc")
		if e.screen() != screenDashboard {
			t.Fatalf("esc from %d went to %d", target, e.screen())
		}
	}
	e.keys("d")
	e.keys("esc")
	if e.screen() != screenDashboard {
		t.Fatal("esc from doctor")
	}
}

func liveEditDraft(repos ...string) setup.Draft {
	d := editDraft()
	d.Config.Updates.Mode = config.WebUpdatesLive
	d.Config.Updates.Live.Repositories = repos
	return setup.NewDraft(d.Config, true, []config.ProjectRoot{
		{ID: config.PathID("root", p("code")), Path: p("code")},
		{ID: config.PathID("root", p("old")), Path: p("old")},
	}, []string{p("work")}, "")
}

func TestLiveUpdatesWizard(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "enter", "down")
	if !strings.Contains(h.model().View(), "installed (2026.9.3)") {
		t.Fatal("the Cloudflare panel shows the detected cloudflared")
	}
	h.keys("enter")
	if h.screen() != screenLiveRepos || f.liveCalls != 1 {
		t.Fatalf("a quick tunnel needs no settings: screen %d, calls %d", h.screen(), f.liveCalls)
	}
	if step := h.model().current().step(); step != 5 {
		t.Fatalf("the picker counts as step 5, got %d", step)
	}
	h.keys("enter")
	if h.screen() != screenLiveRepos || !strings.Contains(h.model().flash, "Pick at least one") {
		t.Fatal("live updates need a repository")
	}
	h.keys("down", "down", "space")
	if !strings.Contains(h.model().flash, "admin rights on acme/web") {
		t.Fatalf("flash = %q", h.model().flash)
	}
	h.keys("up", "up", "space", "enter")
	if h.screen() != screenReview {
		t.Fatalf("screen = %d", h.screen())
	}
	review := h.model().View()
	for _, want := range []string{"Live · Cloudflare quick tunnel · 1 repo", "Outside this computer", "Starts a Cloudflare quick tunnel", "Adds a GitHub webhook to octo/bonsai"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	h.keys("enter")
	if len(f.plans) != 1 {
		t.Fatal("apply did not run")
	}
	plan := f.plans[0]
	live := plan.Config.Updates.Live
	if plan.Config.Updates.Mode != config.WebUpdatesLive || live.Tunnel != setup.UpdatesCloudflaredQuick || strings.Join(live.Repositories, ",") != "octo/bonsai" || !plan.WaitLive || !plan.TunnelStarts {
		t.Fatalf("plan = %+v", plan)
	}
	if h.screen() != screenDone || !strings.Contains(h.model().View(), "1 repository is not live yet") {
		t.Fatalf("done:\n%s", h.model().View())
	}
}

func TestLiveToolMissing(t *testing.T) {
	h := newHarness(t, newFake(), Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "enter", "down", "down", "down", "enter")
	m := h.model()
	if m.current() != screenUpdates || m.draft.Config.Updates.Mode != config.WebUpdatesStandard {
		t.Fatalf("screen %d mode %s", m.current(), m.draft.Config.Updates.Mode)
	}
	if !strings.Contains(m.flash, "ngrok is not installed") || !strings.Contains(m.flash, "https://ngrok.com/download") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestLiveTunnelSettings(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "enter", "down", "down", "enter")
	if h.screen() != screenLiveTunnel || len(h.model().liveIn) != 2 {
		t.Fatalf("screen %d", h.screen())
	}
	h.typeText("bonsai")
	h.keys("enter")
	h.typeText("http://hooks.example.com")
	h.keys("enter")
	if m := h.model(); m.current() != screenLiveTunnel || !strings.Contains(m.inputErr, "https://") {
		t.Fatalf("plain http must be refused: %q", m.inputErr)
	}
	for range "http://hooks.example.com" {
		h.keys("backspace")
	}
	h.typeText("hooks.example.com")
	h.keys("enter")
	if h.screen() != screenLiveRepos {
		t.Fatalf("screen %d err %q", h.screen(), h.model().inputErr)
	}
	live := h.model().draft.Config.Updates.Live
	if live.TunnelName != "bonsai" || live.PublicURL != "https://hooks.example.com" {
		t.Fatalf("live = %+v", live)
	}
	h.keys("space", "enter")
	h.resize(160, 40)
	if !strings.Contains(h.model().View(), "Their admins see a webhook to hooks.example.com") {
		t.Fatalf("review:\n%s", h.model().View())
	}
}

func TestCustomTunnelCannotTargetTheAPI(t *testing.T) {
	h := newHarness(t, newFake(), Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "enter", "down", "down", "down", "down", "down", "down", "enter")
	if h.screen() != screenLiveTunnel {
		t.Fatalf("screen %d", h.screen())
	}
	h.typeText("mytunnel {port} --metrics 127.0.0.1:7001")
	h.keys("enter")
	h.typeText(`https://\S+`)
	h.keys("enter")
	if m := h.model(); m.current() != screenLiveTunnel || !strings.Contains(m.inputErr, "7001") {
		t.Fatalf("screen %d err %q", m.current(), m.inputErr)
	}
}

func TestLiveOffDeletesHooksAfterReview(t *testing.T) {
	f := newFake()
	info := testInfo()
	info.Running = &setup.Running{APIPort: 7001, Hosted: true, WebhookPort: 7002}
	h := newHarness(t, f, Edit, liveEditDraft("octo/bonsai", "octo/api"), info)
	h.model().cursor[screenDashboard] = 3
	h.keys("enter")
	if h.model().cursor[screenUpdates] != 1 {
		t.Fatal("the cursor starts on the chosen tunnel")
	}
	h.keys("up", "enter")
	if h.screen() != screenDashboard {
		t.Fatalf("screen %d", h.screen())
	}
	h.keys("a")
	review := h.model().View()
	for _, want := range []string{"live · Cloudflare quick tunnel → standard", "Deletes Bonsai's webhook from octo/bonsai, octo/api.", "Restarts the Bonsai API (live updates changed)."} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	h.keys("enter")
	if len(f.plans) != 1 || len(f.plans[0].HookRepos(setup.HookRemove)) != 2 || !f.plans[0].RestartAPI {
		t.Fatalf("plan = %+v", f.plans)
	}
}

func TestLiveReposKeepConfigured(t *testing.T) {
	f := newFake()
	info := testInfo()
	info.Running = &setup.Running{APIPort: 7001, Hosted: true, WebhookPort: 7002}
	h := newHarness(t, f, Edit, liveEditDraft("octo/bonsai", "gone/repo"), info)
	h.model().cursor[screenDashboard] = 3
	h.keys("enter", "enter")
	m := h.model()
	if m.current() != screenLiveRepos || len(m.liveRepos) != 4 || !m.livePicked["gone/repo"] || !m.livePicked["octo/bonsai"] {
		t.Fatalf("screen %d repos %+v picked %v", m.current(), m.liveRepos, m.livePicked)
	}
	// Unpick the repository no folder has any more, add octo/api.
	h.keys("down", "space", "down", "down", "space")
	if m := h.model(); m.livePicked["gone/repo"] || !m.livePicked["octo/api"] {
		t.Fatalf("picked %v: a configured repository can always be unpicked", m.livePicked)
	}
	h.keys("enter")
	if h.screen() != screenDashboard {
		t.Fatalf("edit mode returns to the dashboard: %d", h.screen())
	}
	h.keys("a")
	review := h.model().View()
	for _, want := range []string{"Live repos", "+ octo/api", "− gone/repo", "Adds a GitHub webhook to octo/api.", "Deletes Bonsai's webhook from gone/repo."} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	if strings.Contains(review, "Restarts") {
		t.Fatalf("picking repositories restarts nothing:\n%s", review)
	}
}

func TestAdvancedWebhookPortAndSecret(t *testing.T) {
	f := newFake()
	info := testInfo()
	info.Running = &setup.Running{APIPort: 7001, Hosted: true}
	h := newHarness(t, f, Edit, editDraft(), info)
	h.model().cursor[screenDashboard] = 4
	h.keys("enter", "down", "down", "space")
	if !strings.Contains(h.model().flash, "turn them on in Updates first") || h.model().draft.RotateSecret {
		t.Fatal("standard updates have no secret to rotate")
	}

	info.Running.WebhookPort = 7002
	h = newHarness(t, f, Edit, liveEditDraft("octo/bonsai"), info)
	h.model().cursor[screenDashboard] = 4
	h.keys("enter", "down")
	for range "7002" {
		h.keys("backspace")
	}
	h.typeText("7001")
	h.keys("enter")
	if m := h.model(); m.current() != screenAdvanced || !strings.Contains(m.inputErr, "webhook_port") {
		t.Fatalf("same port as the API: %q", m.inputErr)
	}
	for range "7001" {
		h.keys("backspace")
	}
	h.typeText("7012")
	h.keys("down", "space", "enter")
	m := h.model()
	if m.current() != screenDashboard || m.draft.Config.Updates.Live.WebhookPort != 7012 || !m.draft.RotateSecret || !m.dirty() {
		t.Fatalf("screen %d draft %+v", m.current(), m.draft.Config.Updates.Live)
	}
	h.keys("a")
	review := h.model().View()
	for _, want := range []string{"Webhook port", "7002 → 7012", "new webhook secret", "Restarts the Bonsai API (live updates and webhook secret changed).", "at the new address and secret"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	h.keys("enter")
	if plan := f.plans[len(f.plans)-1]; !plan.RotateSecret || !plan.RestartAPI {
		t.Fatalf("plan = %+v", plan)
	}
	if h.model().draft.RotateSecret {
		t.Fatal("a rotation is applied once")
	}
}

func TestInlineFixRunsAndRechecks(t *testing.T) {
	f := newFake()
	f.checks = loggedOut()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter")
	if h.screen() != screenGitHub || h.model().cursor[screenGitHub] != 1 {
		t.Fatalf("cursor should sit on the logged-out row: screen %d cursor %d", h.screen(), h.model().cursor[screenGitHub])
	}
	if !strings.Contains(h.model().View(), "enter fix") {
		t.Fatal("footer should offer the fix")
	}
	before := f.checkCalls
	h.keys("enter")
	if len(f.fixes) != 1 || f.fixes[0].Command != "gh auth login --hostname github.com" || h.execs != 1 {
		t.Fatalf("fixes = %+v execs = %d", f.fixes, h.execs)
	}
	if h.screen() != screenGitHub {
		t.Fatal("running a fix must not leave the screen")
	}
	f.checks = healthyChecks()
	h.update(fixDoneMsg{})
	if f.checkCalls != before+1 {
		t.Fatalf("checks did not re-run after the fix: %d → %d", before, f.checkCalls)
	}
	if c, _ := checks.Find(h.model().checks, checks.IDGHAuth); c.State != checks.OK {
		t.Fatalf("auth = %+v", c)
	}
	h.keys("enter")
	if h.screen() != screenUpdates {
		t.Fatal("with everything fixed, enter moves on")
	}
}

func TestCopyAndRecheck(t *testing.T) {
	f := newFake()
	f.checks = loggedOut()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "enter", "enter", "c")
	if len(f.copied) != 1 || f.copied[0] != "gh auth login --hostname github.com" {
		t.Fatalf("copied = %v", f.copied)
	}
	before := f.checkCalls
	h.keys("r")
	if f.checkCalls != before+1 {
		t.Fatal("r should re-run the checks")
	}
	h.keys("s")
	if h.screen() != screenUpdates {
		t.Fatal("s skips GitHub")
	}
}

func TestAddFolder(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("enter", "a")
	h.typeText("~/nope")
	h.keys("enter")
	m := h.model()
	if !m.adding || !strings.Contains(m.inputErr, "does not exist") {
		t.Fatalf("adding=%v err=%q", m.adding, m.inputErr)
	}
	for range "~/nope" {
		h.keys("backspace")
	}
	h.typeText("~/work")
	h.keys("enter")
	m = h.model()
	if m.adding {
		t.Fatalf("still adding: %q", m.inputErr)
	}
	last := m.draft.Folders[len(m.draft.Folders)-1]
	if last.Path != p("work") || !last.Checked || len(last.Repos) != 3 {
		t.Fatalf("added = %+v", last)
	}
}

func TestEditModeTargetedApply(t *testing.T) {
	f := newFake()
	info := testInfo()
	info.Running = &setup.Running{APIPort: 7001, Hosted: true}
	h := newHarness(t, f, Edit, editDraft(), info)

	h.keys("a")
	if h.screen() != screenDashboard || !strings.Contains(h.model().flash, "Nothing to apply") {
		t.Fatal("a without changes should explain, not open Review")
	}

	// Projects: drop ~/old, add ~/work.
	h.keys("enter", "down", "space", "down", "space", "enter")
	// Open in: this computer only.
	h.keys("down", "enter", "up", "enter")
	// Advanced: port 7011.
	h.keys("down", "down", "down", "enter")
	for range "7001" {
		h.keys("backspace")
	}
	h.typeText("7011")
	h.keys("enter")
	if h.screen() != screenDashboard {
		t.Fatalf("screen = %d", h.screen())
	}
	view := h.model().View()
	if !strings.Contains(view, "Changes not applied yet") || !strings.Contains(view, "edited") {
		t.Fatalf("dashboard should flag pending edits:\n%s", view)
	}
	h.keys("a")
	review := h.model().View()
	for _, want := range []string{"Port", "7001 → 7011", "this computer + hosted app → this computer", "Restarts the Bonsai API (port and hosted app access changed).", "use the new address: http://127.0.0.1:7011/app"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review is missing %q:\n%s", want, review)
		}
	}
	h.keys("enter")
	if len(f.plans) != 1 {
		t.Fatal("apply did not run")
	}
	plan := f.plans[0]
	if !plan.RestartAPI || plan.Start || plan.Config.APIPort != 7011 || plan.Config.Interfaces.Hosted {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.AddRoots) != 1 || plan.AddRoots[0] != p("work") || len(plan.RemoveRoots) != 1 || plan.RemoveRoots[0].Path != p("old") {
		t.Fatalf("roots: add %v remove %v", plan.AddRoots, plan.RemoveRoots)
	}
	if h.screen() != screenDone {
		t.Fatalf("screen = %d", h.screen())
	}
	h.keys("enter")
	if h.screen() != screenDashboard || h.model().dirty() {
		t.Fatal("after apply the dashboard is clean")
	}
	if r := h.model().info.Running; r == nil || r.APIPort != 7001 || r.Hosted {
		t.Fatalf("running should follow the port the fake reported: %+v", r)
	}

	// A second apply in the same session starts from what is on disk now:
	// nothing applied before is applied again.
	h.model().cursor[screenDashboard] = 1
	h.keys("enter", "down", "enter", "a")
	h.keys("enter")
	if len(f.plans) != 2 {
		t.Fatal("second apply did not run")
	}
	second := f.plans[1]
	if len(second.AddRoots) != 0 || len(second.RemoveRoots) != 0 || len(second.SelectRepos) != 0 {
		t.Fatalf("second apply repeated the folder changes: %+v", second)
	}
	// Unchecking the folder added by the first apply removes it now.
	h.keys("enter")
	h.model().cursor[screenDashboard] = 0
	h.keys("enter")
	h.model().cursor[screenProjects] = 2
	h.keys("space", "enter", "a", "enter")
	third := f.plans[2]
	if len(third.RemoveRoots) != 1 || third.RemoveRoots[0].Path != p("work") || third.RemoveRoots[0].ID != config.PathID("root", p("work")) {
		t.Fatalf("third plan = %+v", third)
	}
}

func TestReviewWaitsForFolderScans(t *testing.T) {
	f := newFake()
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.model().draft.Folders[1].Scanned = false // ~/dev still scanning
	h.model().draft.Folders[1].Checked = true
	h.keys("d", "enter")
	if len(f.plans) != 0 || h.screen() != screenReview || !strings.Contains(h.model().flash, "Still looking for repositories") {
		t.Fatalf("applied before the scans finished: plans=%d screen=%d flash=%q", len(f.plans), h.screen(), h.model().flash)
	}
}

func TestEditModeRootsOnlyDoesNotRestart(t *testing.T) {
	f := newFake()
	info := testInfo()
	info.Running = &setup.Running{APIPort: 7001, Hosted: true}
	h := newHarness(t, f, Edit, editDraft(), info)
	h.keys("enter", "down", "down", "space", "enter", "a")
	if !strings.Contains(h.model().View(), "Bonsai picks up project changes within 30 s") {
		t.Fatalf("review:\n%s", h.model().View())
	}
	h.keys("enter")
	if len(f.plans) != 1 || f.plans[0].RestartAPI {
		t.Fatalf("plan = %+v", f.plans)
	}
}

func TestAdvancedRejectsBadPorts(t *testing.T) {
	h := newHarness(t, newFake(), Edit, editDraft(), testInfo())
	h.model().cursor[screenDashboard] = 4
	h.keys("enter")
	for range "7001" {
		h.keys("backspace")
	}
	h.typeText("7002")
	h.keys("enter")
	m := h.model()
	if m.current() != screenAdvanced || !strings.Contains(m.inputErr, "webhook_port") {
		t.Fatalf("screen %d err %q", m.current(), m.inputErr)
	}
	h.keys("esc")
	if h.model().draft.Config.APIPort != 7001 {
		t.Fatal("esc must discard the typed port")
	}
}

func TestApplyFailureStaysOnApply(t *testing.T) {
	f := newFake()
	f.failApply = true
	h := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
	h.keys("d", "enter")
	m := h.model()
	if m.current() != screenApply || m.Applied() || m.applying {
		t.Fatalf("screen %d applied %v applying %v", m.current(), m.Applied(), m.applying)
	}
	view := m.View()
	if !strings.Contains(view, "✗") || !strings.Contains(view, "fix  bonsai web --port 7011") {
		t.Fatalf("failure view:\n%s", view)
	}
	h.keys("esc")
	if h.screen() != screenReview {
		t.Fatal("esc returns to Review")
	}
}

func TestStateIsNotColorOnly(t *testing.T) {
	f := newFake()
	f.checks = loggedOut()
	f.checks[4] = checks.Check{ID: checks.IDPort, Title: "port 7001", State: checks.Fail, Detail: "port 7001 is used by another program", Fix: &checks.Fix{Command: "bonsai web --port 7011"}}
	h := newHarness(t, f, Edit, editDraft(), testInfo())
	h.keys("d")
	view := h.model().View()
	for _, glyph := range []string{"✓", "!", "✗", "–"} {
		if !strings.Contains(view, glyph) {
			t.Fatalf("doctor view lacks %q:\n%s", glyph, view)
		}
	}
	if !strings.Contains(view, "1 problem, 1 warning") {
		t.Fatalf("summary missing:\n%s", view)
	}
}
