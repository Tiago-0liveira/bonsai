package cli

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/livehooks"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// How long Apply follows live updates once bonsai web runs with them. A
// timeout is not a failure: the API keeps going, and bonsai web status
// shows where it got.
type liveWait struct {
	poll, tunnel, hooks, ping time.Duration
}

var defaultLiveWait = liveWait{poll: 500 * time.Millisecond, tunnel: 60 * time.Second, hooks: 60 * time.Second, ping: 90 * time.Second}

const liveGitHubTimeout = 20 * time.Second

// hookClient is created once per bonsai invocation, on first use.
type hookClient struct {
	once sync.Once
	api  livehooks.API
}

// hookAPI is the GitHub client for repository webhooks: the gh login's
// token, in process (setup and doctor only read, except for the deletions
// the user confirmed in Review).
func (w *webCLI) hookAPI() livehooks.API {
	if w.hooks == nil {
		return ghcli.NewShared().Service("")
	}
	w.hooks.once.Do(func() {
		if w.hooks.api == nil {
			w.hooks.api = ghcli.NewShared().Service("")
		}
	})
	return w.hooks.api
}

// checksOptions are the checks that matter for cfg: the chosen tunnel's
// program counts only in live mode.
func checksOptions(cfg config.WebConfig) checks.Options {
	opts := checks.Options{APIPort: cfg.APIPort, UpdatesMode: cfg.Updates.Mode}
	if cfg.Updates.Mode == config.WebUpdatesLive {
		opts.Tunnel = cfg.Updates.Live.Tunnel
		if argv, err := webtunnel.Argv(cfg.Updates.Live.TunnelOptions()); err == nil && len(argv) > 0 {
			opts.TunnelProgram = argv[0]
		}
	}
	return opts
}

// liveSummary is what live updates achieved, for the checks.
func (w *webCLI) liveSummary(context.Context) (checks.LiveSummary, error) {
	cfg, _, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		return checks.LiveSummary{}, err
	}
	state, err := config.ReadWebState(w.statePath)
	if err != nil {
		return checks.LiveSummary{}, err
	}
	out := checks.LiveSummary{PublicURL: state.Live.PublicURL, TunnelError: state.Live.TunnelError}
	if group, err := w.group(false); err == nil && group != nil && group.State == "ready" && group.WebhookPort != 0 {
		out.Running = true
	}
	for _, repo := range cfg.Updates.Live.Repositories {
		_, r, _ := state.Live.Repository(repo)
		out.Repos = append(out.Repos, checks.LiveRepo{Name: repo, State: r.State, Error: r.LastError})
	}
	return out, nil
}

// leftoverCandidates are the repositories web-state.json remembers a hook on
// that live updates no longer cover (removed from web.json by hand, or a
// deletion that failed).
func leftoverCandidates(cfg config.WebConfig, state config.WebLiveState) []string {
	var out []string
	for repo, r := range state.Repositories {
		if r.HookID != 0 && !containsFold(setup.LiveRepositories(cfg), repo) {
			out = append(out, repo)
		}
	}
	sort.Strings(out)
	return out
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// leftoverHooks finds this computer's Bonsai hooks on those repositories.
// It only reads.
func (w *webCLI) leftoverHooks(ctx context.Context, cfg config.WebConfig, state config.WebLiveState) ([]checks.LeftoverHook, error) {
	ctx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
	defer cancel()
	var out []checks.LeftoverHook
	for _, repo := range leftoverCandidates(cfg, state) {
		hook, found, err := livehooks.Find(ctx, w.hookAPI(), repo, state.Repositories[repo].HookID, state.InstallID)
		if err != nil {
			return nil, fmt.Errorf("%s", livehooks.Message(repo, err))
		}
		if found {
			out = append(out, checks.LeftoverHook{Repo: repo, HookID: hook.ID})
		}
	}
	return out, nil
}

// LiveRepositories lists the GitHub repositories of the local repositories
// and of the configured ones, with whether the gh login administers each.
func (b *setupBackend) LiveRepositories(ctx context.Context, repos []setup.Repo, configured []string) []setup.LiveRepo {
	ctx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
	defer cancel()
	byName := map[string]*setup.LiveRepo{}
	var order []string
	add := func(fullName, localName string) {
		key := strings.ToLower(fullName)
		if r, ok := byName[key]; ok {
			if r.Local == "" {
				r.Local = localName
			}
			return
		}
		byName[key] = &setup.LiveRepo{FullName: fullName, Local: localName}
		order = append(order, key)
	}
	seen := map[string]bool{}
	for _, repo := range repos {
		if repo.Path == "" || seen[repo.Path] {
			continue
		}
		seen[repo.Path] = true
		if name := githubRepository(ctx, repo.Path); name != "" {
			add(name, repo.Name)
		}
	}
	for _, repo := range configured {
		add(repo, "")
	}
	out := make([]setup.LiveRepo, len(order))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, key := range order {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			r := *byName[key]
			admin, err := b.w.hookAPI().RepoAdmin(ctx, r.FullName)
			if err != nil {
				r.Err = livehooks.Message(r.FullName, err)
			}
			r.Admin = admin
			out[i] = r
		}()
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	return out
}

// githubRepository is the GitHub repository (owner/name) the repository at
// dir pushes to: origin when it is on GitHub, else its first GitHub remote.
func githubRepository(ctx context.Context, dir string) string {
	name := ""
	for _, remote := range local.RemoteIdentities(ctx, dir) {
		if remote.FullName == "" {
			continue
		}
		if remote.Name == "origin" {
			return remote.FullName
		}
		if name == "" {
			name = remote.FullName
		}
	}
	return name
}

// rotateSecret writes a new webhook secret before the API (re)starts and
// reads it.
func (b *setupBackend) rotateSecret(progress func(websetupui.Step)) error {
	progress(websetupui.Step{ID: "secret", Label: "Making a new webhook secret", State: websetupui.StepRunning})
	path, err := config.WebWebhookSecretPath()
	if err == nil {
		_, err = config.RotateWebWebhookSecret(path)
	}
	if err != nil {
		progress(websetupui.Step{ID: "secret", Label: "Make a new webhook secret", State: websetupui.StepFailed, Detail: err.Error(), Fix: "run bonsai web setup again"})
		return err
	}
	progress(websetupui.Step{ID: "secret", Label: "Made a new webhook secret", State: websetupui.StepDone})
	return nil
}

// removeHooks deletes this computer's Bonsai webhook from every repository
// the user took off live updates. A failure is reported with its fix; the
// rest of Apply goes on.
func (b *setupBackend) removeHooks(ctx context.Context, repos []string, progress func(websetupui.Step)) []string {
	var notes []string
	state, err := config.ReadWebState(b.w.statePath)
	if err != nil {
		state = config.WebState{}
	}
	for _, repo := range repos {
		id := "unhook-" + strings.ToLower(repo)
		progress(websetupui.Step{ID: id, Label: "Deleting the webhook of " + repo, State: websetupui.StepRunning})
		name, current, _ := state.Live.Repository(repo)
		deleted := false
		if state.Live.InstallID != "" {
			callCtx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
			deleted, err = livehooks.Remove(callCtx, b.w.hookAPI(), repo, current, state.Live.InstallID)
			cancel()
			if err != nil {
				progress(websetupui.Step{ID: id, Label: "Delete the webhook of " + repo, State: websetupui.StepFailed,
					Detail: livehooks.Message(repo, err), Fix: "delete it by hand: https://github.com/" + repo + "/settings/hooks"})
				notes = append(notes, "The webhook of "+repo+" is still there; bonsai web doctor lists it.")
				continue
			}
		}
		if name != "" {
			if _, err := config.UpdateWebState(b.w.statePath, func(s *config.WebState) error {
				delete(s.Live.Repositories, name)
				return nil
			}); err != nil {
				notes = append(notes, "Could not update "+b.w.statePath+": "+err.Error())
			}
		}
		if deleted {
			progress(websetupui.Step{ID: id, Label: "Deleted the webhook of " + repo, State: websetupui.StepDone})
		} else {
			progress(websetupui.Step{ID: id, Label: "No webhook to delete on " + repo, State: websetupui.StepSkipped})
		}
	}
	return notes
}

// followLive reports what the running API does with live updates after
// Apply: the receiver, the tunnel's public address, each hook and its first
// ping. since is when Apply started; only state written after it, and after
// the API process started (a restarted API's predecessor may still have
// written on its way out), counts.
func (b *setupBackend) followLive(ctx context.Context, cfg config.WebConfig, group *procstore.ServeGroup, since time.Time, progress func(websetupui.Step)) []string {
	wait := b.wait
	if wait.poll == 0 {
		wait = defaultLiveWait
	}
	live := cfg.Updates.Live
	if group == nil || group.WebhookPort == 0 {
		progress(websetupui.Step{ID: "receiver", Label: "Change receiver", State: websetupui.StepFailed, Detail: "bonsai web runs without it", Fix: "bonsai web stop      then      bonsai web"})
		return []string{"Live updates are not running; see bonsai web status."}
	}
	for _, p := range group.Processes {
		if p.Name == "api" && p.StartedAt.After(since) {
			since = p.StartedAt
		}
	}
	progress(websetupui.Step{ID: "receiver", Label: "Change receiver ready", State: websetupui.StepDone, Detail: fmt.Sprintf("127.0.0.1:%d", group.WebhookPort)})

	read := func() config.WebLiveState {
		state, err := config.ReadWebState(b.w.statePath)
		if err != nil {
			return config.WebLiveState{}
		}
		return state.Live
	}
	poll := func(limit time.Duration, done func(config.WebLiveState) bool) (config.WebLiveState, bool) {
		deadline := time.Now().Add(limit)
		for {
			state := read()
			if done(state) {
				return state, true
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				return state, false
			}
			time.Sleep(wait.poll)
		}
	}

	// The tunnel's public address.
	argv, _ := webtunnel.Argv(live.TunnelOptions())
	if argv != nil && !hasProcess(group, webtunnel.SidecarName) {
		fix := "install " + argv[0] + ", then run bonsai web"
		if f := checks.TunnelInstallFix(runtime.GOOS, argv[0]); f != nil {
			fix = f.Command + "      then      bonsai web"
		}
		progress(websetupui.Step{ID: "tunnel", Label: "Start the tunnel", State: websetupui.StepFailed, Detail: argv[0] + " is not installed", Fix: fix})
		return []string{"Live updates wait for the tunnel; standard updates continue meanwhile."}
	}
	progress(websetupui.Step{ID: "tunnel", Label: "Waiting for the tunnel's address", State: websetupui.StepRunning})
	state, ok := poll(wait.tunnel, func(s config.WebLiveState) bool {
		return s.UpdatedAt.After(since) && s.PublicURL != "" && s.TunnelError == ""
	})
	if !ok {
		detail := "no address yet"
		if state.TunnelError != "" && state.UpdatedAt.After(since) {
			detail = state.TunnelError
		}
		progress(websetupui.Step{ID: "tunnel", Label: "Tunnel address", State: websetupui.StepFailed, Detail: detail, Fix: "bonsai web logs tunnel"})
		return []string{"Live updates wait for the tunnel; standard updates continue meanwhile. See bonsai web status."}
	}
	progress(websetupui.Step{ID: "tunnel", Label: "Tunnel address", State: websetupui.StepDone, Detail: state.PublicURL})

	// Each hook, created or re-pointed by the API.
	repos := live.Repositories
	checked := func(s config.WebLiveState, repo string) (config.WebLiveRepository, bool) {
		_, r, ok := s.Repository(repo)
		return r, ok && r.CheckedAt.After(since)
	}
	progress(websetupui.Step{ID: "hooks", Label: "Setting up GitHub webhooks", State: websetupui.StepRunning})
	state, _ = poll(wait.hooks, func(s config.WebLiveState) bool {
		for _, repo := range repos {
			if _, ok := checked(s, repo); !ok {
				return false
			}
		}
		return true
	})
	var problems []string
	var problemFix string
	var ready []string
	for _, repo := range repos {
		r, ok := checked(state, repo)
		switch {
		case !ok:
			problems = append(problems, repo+" not set up yet")
			problemFix = "bonsai web status"
		case r.State == config.WebLiveStateNeedsAdmin || r.State == config.WebLiveStateScopeMissing || (r.State == config.WebLiveStateFailing && r.HookID == 0):
			problems = append(problems, repo+": "+r.LastError)
			if problemFix == "" {
				problemFix = liveRepoFix(repo, r)
			}
		default:
			ready = append(ready, repo)
		}
	}
	if len(problems) > 0 {
		progress(websetupui.Step{ID: "hooks", Label: "GitHub webhooks", State: websetupui.StepFailed, Detail: strings.Join(problems, " · "), Fix: problemFix})
	} else {
		progress(websetupui.Step{ID: "hooks", Label: "GitHub webhooks", State: websetupui.StepDone, Detail: strings.Join(ready, ", ")})
	}
	if len(ready) == 0 {
		return []string{notLiveYet(len(repos))}
	}

	// GitHub's ping proves the whole path: GitHub → tunnel → receiver.
	progress(websetupui.Step{ID: "ping", Label: "Waiting for GitHub's ping", State: websetupui.StepRunning, Detail: strings.Join(ready, ", ")})
	state, _ = poll(wait.ping, func(s config.WebLiveState) bool {
		for _, repo := range ready {
			if _, r, _ := s.Repository(repo); r.State != config.WebLiveStateLive {
				return false
			}
		}
		return true
	})
	var waiting []string
	for _, repo := range ready {
		if _, r, _ := state.Repository(repo); r.State != config.WebLiveStateLive {
			waiting = append(waiting, repo)
		}
	}
	liveCount := len(ready) - len(waiting)
	if len(waiting) > 0 {
		progress(websetupui.Step{ID: "ping", Label: "GitHub's ping", State: websetupui.StepFailed, Detail: "no ping yet from " + strings.Join(waiting, ", "), Fix: "bonsai web status   (Bonsai keeps trying)"})
	} else {
		progress(websetupui.Step{ID: "ping", Label: "GitHub's ping arrived", State: websetupui.StepDone, Detail: countWord(liveCount, "repo") + " live"})
	}
	if notLive := len(repos) - liveCount; notLive > 0 {
		return []string{notLiveYet(notLive)}
	}
	return nil
}

func notLiveYet(n int) string {
	if n == 1 {
		return "1 repository is not live yet; see bonsai web status."
	}
	return fmt.Sprintf("%d repositories are not live yet; see bonsai web status.", n)
}

// liveRepoFix is the one thing to do about a repository live updates cannot
// cover.
func liveRepoFix(repo string, r config.WebLiveRepository) string {
	switch r.State {
	case config.WebLiveStateNeedsAdmin:
		return "ask an admin of " + repo + ", or drop it in: bonsai web setup → Updates"
	case config.WebLiveStateScopeMissing:
		return "gh auth refresh -h " + livehooks.GitHubHost + " -s admin:repo_hook"
	}
	return "bonsai web status"
}
