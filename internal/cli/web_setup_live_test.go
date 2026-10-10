package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"github.com/Tiago-0liveira/bonsai/internal/livehooks"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// fakeHooks is a GitHub with a few repositories and their hooks.
type fakeHooks struct {
	mu      sync.Mutex
	admin   map[string]bool
	errs    map[string]error
	hooks   map[string][]app.Hook
	deleted []string
}

func (f *fakeHooks) RepoAdmin(_ context.Context, repo string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[repo]; err != nil {
		return false, err
	}
	return f.admin[repo], nil
}

func (f *fakeHooks) ListHooks(_ context.Context, repo string) ([]app.Hook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[repo]; err != nil {
		return nil, err
	}
	return append([]app.Hook{}, f.hooks[repo]...), nil
}

func (f *fakeHooks) GetHook(_ context.Context, repo string, id int64) (app.Hook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hooks[repo] {
		if h.ID == id {
			return h, nil
		}
	}
	return app.Hook{}, domain.E("not_found", "Not Found")
}

func (f *fakeHooks) CreateHook(context.Context, string, app.HookSpec) (app.Hook, error) {
	panic("setup never creates hooks")
}

func (f *fakeHooks) UpdateHook(context.Context, string, int64, app.HookSpec) (app.Hook, error) {
	panic("setup never updates hooks")
}

func (f *fakeHooks) DeleteHook(_ context.Context, repo string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, repo)
	return nil
}

func (f *fakeHooks) PingHook(context.Context, string, int64) error { return nil }

func bonsaiHook(id int64, installID string) app.Hook {
	return app.Hook{ID: id, Active: true, Config: app.HookConfig{URL: livehooks.HookURL("https://x.trycloudflare.com", installID)}}
}

// liveSetup is an offline setup backend with a fake GitHub and its own
// web-state.json.
func liveSetup(t *testing.T, gh *fakeHooks) (*setupBackend, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	w := &webCLI{
		out: &out, errOut: &out,
		home:       filepath.Join(dir, "home"),
		configPath: filepath.Join(dir, "web.json"),
		statePath:  filepath.Join(dir, "web-state.json"),
		client:     client.ForUserHome(filepath.Join(dir, "home")),
		hooks:      &hookClient{api: gh},
	}
	b := w.newSetupBackend(filepath.Join(dir, "project-roots.json"), config.DefaultWebConfig(), false, webStartOptions{noOpen: true})
	b.wait = liveWait{poll: 5 * time.Millisecond, tunnel: 300 * time.Millisecond, hooks: 300 * time.Millisecond, ping: 300 * time.Millisecond}
	return b, &out
}

func repoWithRemotes(t *testing.T, path string, remotes ...string) setup.Repo {
	t.Helper()
	gitInit(t, path)
	for i := 0; i+1 < len(remotes); i += 2 {
		if out, err := exec.Command("git", "-C", path, "remote", "add", remotes[i], remotes[i+1]).CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v: %s", err, out)
		}
	}
	return setup.Repo{ID: path, Name: filepath.Base(path), Path: path}
}

func TestLiveRepositoriesListsGitHubRemotesWithAdminRights(t *testing.T) {
	gh := &fakeHooks{
		admin: map[string]bool{"octo/bonsai": true},
		errs:  map[string]error{"gone/repo": domain.E("not_found", "Not Found")},
	}
	b, _ := liveSetup(t, gh)
	base := t.TempDir()
	repos := []setup.Repo{
		repoWithRemotes(t, filepath.Join(base, "bonsai"), "upstream", "https://github.com/acme/bonsai.git", "origin", "git@github.com:octo/bonsai.git"),
		repoWithRemotes(t, filepath.Join(base, "web"), "upstream", "https://github.com/acme/web"),
		repoWithRemotes(t, filepath.Join(base, "lab"), "origin", "https://gitlab.com/octo/lab.git"),
	}
	got := b.LiveRepositories(context.Background(), repos, []string{"OCTO/bonsai", "gone/repo"})
	var rows []string
	for _, r := range got {
		row := r.FullName + " " + r.Local
		if r.Admin {
			row += " admin"
		}
		if r.Err != "" {
			row += " err=" + r.Err
		}
		rows = append(rows, row)
	}
	want := []string{
		"acme/web web",
		"gone/repo  err=gone/repo was not found, or your gh login cannot see it",
		"octo/bonsai bonsai admin",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestSetupDeletesOnlyConfirmedHooks(t *testing.T) {
	gh := &fakeHooks{hooks: map[string][]app.Hook{
		"octo/a": {bonsaiHook(5, "me"), {ID: 6, Config: app.HookConfig{URL: "https://ci.example.com/hook"}}},
		"octo/b": {bonsaiHook(8, "another-machine")},
	}, errs: map[string]error{"octo/c": domain.E("unauthorized", "Bad credentials")}}
	b, _ := liveSetup(t, gh)
	if _, err := config.UpdateWebState(b.w.statePath, func(s *config.WebState) error {
		s.Live.InstallID = "me"
		s.Live.Repositories = map[string]config.WebLiveRepository{
			"Octo/A": {HookID: 5, State: config.WebLiveStateLive},
			"octo/b": {State: config.WebLiveStateLive},
			"octo/c": {HookID: 9, State: config.WebLiveStateLive},
			"octo/d": {HookID: 11, State: config.WebLiveStateLive},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var steps []websetupui.Step
	notes := b.removeHooks(context.Background(), []string{"octo/a", "octo/b", "octo/c"}, func(s websetupui.Step) { steps = append(steps, s) })
	if strings.Join(gh.deleted, ",") != "octo/a" {
		t.Fatalf("deleted %v: only this computer's hook may go", gh.deleted)
	}
	last := map[string]websetupui.Step{}
	for _, s := range steps {
		last[s.ID] = s
	}
	if s := last["unhook-octo/a"]; s.State != websetupui.StepDone {
		t.Fatalf("a = %+v", s)
	}
	if s := last["unhook-octo/b"]; s.State != websetupui.StepSkipped {
		t.Fatalf("b = %+v", s)
	}
	if s := last["unhook-octo/c"]; s.State != websetupui.StepFailed || !strings.Contains(s.Fix, "github.com/octo/c/settings/hooks") {
		t.Fatalf("c = %+v", s)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "bonsai web doctor") {
		t.Fatalf("notes = %q", notes)
	}
	state, _ := config.ReadWebState(b.w.statePath)
	if _, ok := state.Live.Repositories["Octo/A"]; ok {
		t.Fatal("a deleted hook keeps its entry")
	}
	if _, ok := state.Live.Repositories["octo/c"]; !ok {
		t.Fatal("a hook that could not be deleted must stay recorded for doctor")
	}
	if _, ok := state.Live.Repositories["octo/d"]; !ok {
		t.Fatal("an unrelated entry was dropped")
	}
}

func TestDoctorFindsLeftoverHooks(t *testing.T) {
	gh := &fakeHooks{hooks: map[string][]app.Hook{
		"octo/old":  {bonsaiHook(41, "me")},
		"octo/gone": {},
		"octo/live": {bonsaiHook(3, "me")},
	}}
	b, _ := liveSetup(t, gh)
	cfg := config.DefaultWebConfig()
	cfg.Updates.Mode = config.WebUpdatesLive
	cfg.Updates.Live.Repositories = []string{"octo/live"}
	state := config.WebLiveState{InstallID: "me", Repositories: map[string]config.WebLiveRepository{
		"octo/old":  {HookID: 41},
		"octo/gone": {HookID: 40},
		"octo/live": {HookID: 3},
		"octo/none": {},
	}}
	if got := leftoverCandidates(cfg, state); strings.Join(got, ",") != "octo/gone,octo/old" {
		t.Fatalf("candidates = %v", got)
	}
	hooks, err := b.w.leftoverHooks(context.Background(), cfg, state)
	if err != nil || len(hooks) != 1 || hooks[0] != (checks.LeftoverHook{Repo: "octo/old", HookID: 41}) {
		t.Fatalf("hooks %+v err %v", hooks, err)
	}
	cfg.Updates.Mode = config.WebUpdatesStandard
	if got := leftoverCandidates(cfg, state); len(got) != 3 {
		t.Fatalf("live off: every remembered hook is a leftover, got %v", got)
	}
}

func TestFollowLiveReportsTheAPIProgress(t *testing.T) {
	b, _ := liveSetup(t, &fakeHooks{})
	cfg := config.DefaultWebConfig()
	cfg.Updates.Mode = config.WebUpdatesLive
	cfg.Updates.Live.Repositories = []string{"octo/a", "octo/b"}
	since := time.Now().Add(-time.Minute)
	// The API restarted after Apply began: what its predecessor wrote in
	// between (below, the old address) must not count.
	group := &procstore.ServeGroup{WebhookPort: 7002, Processes: []procstore.ServeProcess{{Name: "api", StartedAt: time.Now()}, {Name: "tunnel"}}}
	write := func(fn func(*config.WebLiveState)) {
		if _, err := config.UpdateWebState(b.w.statePath, func(s *config.WebState) error {
			if s.Live.Repositories == nil {
				s.Live.Repositories = map[string]config.WebLiveRepository{}
			}
			fn(&s.Live)
			s.Live.UpdatedAt = time.Now()
			return nil
		}); err != nil {
			t.Error(err)
		}
	}
	// What the previous API wrote on its way out must not count.
	if _, err := config.UpdateWebState(b.w.statePath, func(s *config.WebState) error {
		s.Live.PublicURL = "https://old.trycloudflare.com"
		s.Live.UpdatedAt = since.Add(time.Second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		write(func(s *config.WebLiveState) { s.PublicURL = "https://new.trycloudflare.com" })
		time.Sleep(20 * time.Millisecond)
		now := time.Now()
		write(func(s *config.WebLiveState) {
			s.Repositories["octo/a"] = config.WebLiveRepository{HookID: 1, State: config.WebLiveStateWaiting, CheckedAt: now}
			s.Repositories["octo/b"] = config.WebLiveRepository{State: config.WebLiveStateNeedsAdmin, LastError: "you are not an admin of octo/b, so it stays on standard updates", CheckedAt: now}
		})
		time.Sleep(20 * time.Millisecond)
		write(func(s *config.WebLiveState) {
			r := s.Repositories["octo/a"]
			r.State = config.WebLiveStateLive
			s.Repositories["octo/a"] = r
		})
	}()
	last := map[string]websetupui.Step{}
	notes := b.followLive(context.Background(), cfg, group, since, func(s websetupui.Step) { last[s.ID] = s })
	if s := last["receiver"]; s.State != websetupui.StepDone || s.Detail != "127.0.0.1:7002" {
		t.Fatalf("receiver = %+v", s)
	}
	if s := last["tunnel"]; s.State != websetupui.StepDone || s.Detail != "https://new.trycloudflare.com" {
		t.Fatalf("tunnel = %+v", s)
	}
	if s := last["hooks"]; s.State != websetupui.StepFailed || !strings.Contains(s.Detail, "not an admin of octo/b") || !strings.Contains(s.Fix, "ask an admin of octo/b") {
		t.Fatalf("hooks = %+v", s)
	}
	if s := last["ping"]; s.State != websetupui.StepDone || s.Detail != "1 repo live" {
		t.Fatalf("ping = %+v", s)
	}
	if len(notes) != 1 || notes[0] != "1 repository is not live yet; see bonsai web status." {
		t.Fatalf("notes = %q", notes)
	}

	// No tunnel process: say why at once instead of waiting.
	group.Processes = group.Processes[:1]
	cfg.Updates.Live.Tunnel = "ngrok"
	last = map[string]websetupui.Step{}
	b.followLive(context.Background(), cfg, group, time.Now(), func(s websetupui.Step) { last[s.ID] = s })
	if s := last["tunnel"]; s.State != websetupui.StepFailed || !strings.Contains(s.Detail, "ngrok is not installed") || s.Fix == "" {
		t.Fatalf("tunnel = %+v", s)
	}
}

func TestSetupRotatesTheSecretBeforeStarting(t *testing.T) {
	// The secret lives under the user config directory: point every OS's
	// variant of it at a temporary home.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	b, _ := liveSetup(t, &fakeHooks{})
	path, err := config.WebWebhookSecretPath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := config.EnsureWebWebhookSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	plan := setup.Plan{Config: config.DefaultWebConfig(), RotateSecret: true}
	plan.Config.SetupVersion = config.WebSetupVersion
	var steps []websetupui.Step
	if result := b.Apply(context.Background(), plan, func(s websetupui.Step) { steps = append(steps, s) }); !result.OK {
		t.Fatalf("steps %+v", steps)
	}
	after, err := config.ReadWebWebhookSecret(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatalf("secret not rotated: %v", err)
	}
	for _, s := range steps {
		if strings.Contains(s.Label+s.Detail, string(after)) {
			t.Fatal("the secret reached the progress lines")
		}
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("secret outside the test home: %s", path)
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("secret file mode: %v %v", info, err)
	}
}
