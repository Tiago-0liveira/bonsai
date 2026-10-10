package localapi

import (
	"context"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/livehooks"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// Live controller cadence.
const (
	// liveURLWait is how often the public URL is looked for while it is
	// unknown or a hook waits for its ping.
	liveURLWait = 5 * time.Second
	// liveURLPoll is how often a known public URL is checked for a change
	// (a restarted quick tunnel prints a new one).
	liveURLPoll = 30 * time.Second
	// liveHealthInterval is how often each hook's last delivery is read.
	liveHealthInterval = 5 * time.Minute
	// livePingRetry is how long a waiting hook goes without its ping before
	// it is pinged again, at most livePingRetries times.
	livePingRetry   = 20 * time.Second
	livePingRetries = 3
	// liveDeliveryWrite bounds how often a delivery's timestamp alone is
	// written to web-state.json.
	liveDeliveryWrite = time.Minute
	liveGitHubTimeout = 30 * time.Second
	// liveConfigWatch is how often web.json is checked for an edit (setup
	// writes it while bonsai web runs), so new repositories get their hook
	// within seconds instead of at the next step.
	liveConfigWatch = 2 * time.Second
)

// liveController keeps the repository hooks of web.json's live repositories
// pointed at the tunnel's public URL, tracks their health and records it in
// web-state.json. It creates and re-points hooks; it never deletes one (only
// setup does, after the user confirmed it in Review).
type liveController struct {
	configPath string
	statePath  string
	secret     []byte
	hooks      livehooks.API
	publicURL  publicURLSource
	sync       *stateSync
	now        func() time.Time
	// queue replaces sync.Queue in tests.
	queue func(projectID string, scope refreshScope, forceProvider bool)

	mu      sync.Mutex
	options webtunnel.Options
	repos   []string
	state   config.WebLiveState
	// tunnelUp is whether the last URL lookup succeeded.
	tunnelUp bool
	stepped  bool
	// reconciledURL is the public URL every hook was last reconciled for
	// ("" forces a full reconcile, as on startup).
	reconciledURL string
	reconciledFor string
	lastHealth    time.Time
	pings         map[string]livePing
	dirty         bool
	lastWrite     time.Time
}

type livePing struct {
	at    time.Time
	count int
}

func newLiveController(configPath, statePath string, secret []byte, hooks livehooks.API, publicURL publicURLSource, sync *stateSync) *liveController {
	return &liveController{
		configPath: configPath,
		statePath:  statePath,
		secret:     secret,
		hooks:      hooks,
		publicURL:  publicURL,
		sync:       sync,
		now:        func() time.Time { return time.Now().UTC() },
		pings:      map[string]livePing{},
	}
}

// Run reconciles until ctx ends. Every project is refreshed once at the
// start, so events missed while bonsai web was down are picked up and
// deliveries can be matched to projects before any browser connects.
func (c *liveController) Run(ctx context.Context) {
	if !c.load() {
		return
	}
	c.refreshAll()
	timer := time.NewTimer(0)
	defer timer.Stop()
	watch := time.NewTicker(liveConfigWatch)
	defer watch.Stop()
	modified := configModified(c.configPath)
	for {
		select {
		case <-ctx.Done():
			c.persist()
			return
		case <-watch.C:
			if now := configModified(c.configPath); !now.Equal(modified) {
				modified = now
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(0)
			}
		case <-timer.C:
			timer.Reset(c.step(ctx))
		}
	}
}

func configModified(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (c *liveController) refreshAll() {
	if c.sync != nil {
		c.sync.RefreshAll(refreshAll, true)
	}
}

// load reads web.json and web-state.json; it reports whether live updates
// are on.
func (c *liveController) load() bool {
	if c.configPath == "" || c.statePath == "" {
		return false
	}
	installID, err := config.EnsureWebInstallID(c.statePath)
	if err != nil {
		log.Printf("live updates: %v", err)
		return false
	}
	state, err := config.ReadWebState(c.statePath)
	if err != nil {
		log.Printf("live updates: %v", err)
		return false
	}
	c.mu.Lock()
	c.state = state.Live
	c.state.InstallID = installID
	if c.state.Repositories == nil {
		c.state.Repositories = map[string]config.WebLiveRepository{}
	}
	// The first step always writes, so setup can tell this run's state
	// (updated_at) from the previous one's.
	c.dirty = true
	c.mu.Unlock()
	return c.reloadConfig()
}

// reloadConfig picks up web.json changes (setup writes it while bonsai web
// runs). It reports whether live updates are still on.
func (c *liveController) reloadConfig() bool {
	cfg, _, err := config.ReadWebConfig(c.configPath)
	if err != nil {
		log.Printf("live updates: %v", err)
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.Updates.Mode != config.WebUpdatesLive {
		c.repos = nil
		return false
	}
	c.options = cfg.Updates.Live.TunnelOptions()
	c.repos = slices.Clone(cfg.Updates.Live.Repositories)
	return true
}

// step runs one round and returns when the next one is due.
func (c *liveController) step(ctx context.Context) time.Duration {
	if !c.reloadConfig() {
		return liveURLPoll
	}
	c.mu.Lock()
	options := c.options
	c.mu.Unlock()
	publicURL, urlErr := c.publicURL(ctx, options)
	now := c.now()

	c.mu.Lock()
	// A tunnel back up (same URL) leaves its live hooks live: their projects
	// still need the refresh that covers the time it was down.
	var cameUp []string
	if !c.tunnelUp && urlErr == nil && c.stepped {
		for _, repo := range c.repos {
			if name, r, ok := c.state.Repository(repo); ok && r.State == config.WebLiveStateLive {
				cameUp = append(cameUp, name)
			}
		}
	}
	c.stepped = true
	c.tunnelUp = urlErr == nil
	tunnelError := ""
	if urlErr != nil {
		tunnelError = urlErr.Error()
	}
	if c.state.TunnelError != tunnelError || (urlErr == nil && c.state.PublicURL != publicURL) {
		if urlErr == nil {
			if c.state.PublicURL != publicURL {
				log.Printf("live updates: public URL %s", publicURL)
			}
			c.state.PublicURL = publicURL
		} else {
			log.Printf("live updates: no public URL: %s", tunnelError)
		}
		c.state.TunnelError = tunnelError
		c.dirty = true
	}
	repos := slices.Clone(c.repos)
	reposKey := strings.ToLower(strings.Join(repos, ","))
	full := urlErr == nil && (c.reconciledURL != publicURL || c.reconciledFor != reposKey)
	health := urlErr == nil && !full && now.Sub(c.lastHealth) >= liveHealthInterval
	want := livehooks.Desired{PublicURL: publicURL, InstallID: c.state.InstallID, Secret: c.secret}
	c.mu.Unlock()
	for _, repo := range cameUp {
		c.refreshRepository(repo)
	}

	if full || health {
		for _, repo := range repos {
			c.reconcile(ctx, repo, want, full)
		}
		if health {
			// GitHub does not redeliver: a hook failing since a tunnel
			// outage is pinged so it can show live again without waiting for
			// the next real event.
			c.pingFailing(ctx, repos)
		}
		c.mu.Lock()
		c.reconciledURL, c.reconciledFor, c.lastHealth = publicURL, reposKey, now
		c.mu.Unlock()
	} else if urlErr == nil {
		c.retryPings(ctx, repos)
	}
	c.persist()

	c.mu.Lock()
	defer c.mu.Unlock()
	if urlErr != nil {
		return liveURLWait
	}
	for _, repo := range repos {
		if _, r, ok := c.state.Repository(repo); ok && r.State == config.WebLiveStateWaiting {
			return liveURLWait
		}
	}
	return liveURLPoll
}

// reconcile runs one repository's Reconcile (full) or Check against GitHub
// and merges the result with any ping or delivery that arrived meanwhile.
func (c *liveController) reconcile(ctx context.Context, repo string, want livehooks.Desired, full bool) {
	c.mu.Lock()
	_, current, _ := c.state.Repository(repo)
	c.mu.Unlock()
	started := c.now()
	callCtx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
	var next config.WebLiveRepository
	if full {
		next = livehooks.Reconcile(callCtx, c.hooks, repo, want, current, started)
	} else {
		next = livehooks.Check(callCtx, c.hooks, repo, want, current, started)
	}
	cancel()
	if ctx.Err() != nil {
		return
	}
	if next.ConfiguredAt.Equal(started) {
		log.Printf("live updates: %s hook %d now points at %s", repo, next.HookID, want.PublicURL)
	}
	c.commit(repo, func(latest config.WebLiveRepository) config.WebLiveRepository {
		next.LastPingAt = later(next.LastPingAt, latest.LastPingAt)
		next.LastDeliveryAt = later(next.LastDeliveryAt, latest.LastDeliveryAt)
		// A ping that arrived while GitHub was being called proves the hook.
		if next.State == config.WebLiveStateWaiting && (latest.LastPingAt.After(started) || latest.LastDeliveryAt.After(started)) {
			next.State, next.LastError = config.WebLiveStateLive, ""
		}
		if next.ConfiguredAt.Equal(started) {
			c.pings[strings.ToLower(repo)] = livePing{at: started}
		}
		return next
	})
}

// retryPings pings hooks still waiting after livePingRetry (their first ping
// can reach a quick tunnel before its hostname resolves).
func (c *liveController) retryPings(ctx context.Context, repos []string) {
	now := c.now()
	for _, repo := range repos {
		key := strings.ToLower(repo)
		c.mu.Lock()
		_, current, ok := c.state.Repository(repo)
		ping := c.pings[key]
		due := ok && current.State == config.WebLiveStateWaiting && ping.count < livePingRetries &&
			now.Sub(later(ping.at, current.ConfiguredAt)) >= livePingRetry
		if due {
			c.pings[key] = livePing{at: now, count: ping.count + 1}
		}
		c.mu.Unlock()
		if !due {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
		err := livehooks.Ping(callCtx, c.hooks, repo, current)
		cancel()
		if err != nil {
			log.Printf("live updates: ping %s: %v", repo, err)
		}
	}
}

func (c *liveController) pingFailing(ctx context.Context, repos []string) {
	for _, repo := range repos {
		c.mu.Lock()
		_, current, ok := c.state.Repository(repo)
		c.mu.Unlock()
		if !ok || current.State != config.WebLiveStateFailing || current.HookID == 0 {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, liveGitHubTimeout)
		if err := livehooks.Ping(callCtx, c.hooks, repo, current); err != nil {
			log.Printf("live updates: ping %s: %v", repo, err)
		}
		cancel()
	}
}

// observe records a verified delivery: the hook works. It runs on the
// receiver's request goroutine.
func (c *liveController) observe(event webhooks.LiveEvent) {
	now := c.now()
	c.mu.Lock()
	tracked := false
	for _, repo := range c.repos {
		tracked = tracked || strings.EqualFold(repo, event.RepositoryFullName)
	}
	c.mu.Unlock()
	if !tracked {
		return
	}
	changed := c.commit(event.RepositoryFullName, func(r config.WebLiveRepository) config.WebLiveRepository {
		if event.Event == "ping" {
			r.LastPingAt = now
		} else {
			r.LastDeliveryAt = now
		}
		r.State, r.LastError = config.WebLiveStateLive, ""
		return r
	})
	// A new state or a ping is written at once; a delivery's timestamp alone
	// at most every liveDeliveryWrite (the next step writes it otherwise).
	c.mu.Lock()
	write := changed || event.Event == "ping" || now.Sub(c.lastWrite) >= liveDeliveryWrite
	c.mu.Unlock()
	if write {
		c.persist()
	}
}

// commit replaces repo's state with mutate(current) and reports whether its
// state changed. A repository that became healthy has its projects refreshed
// (events may have been missed while it was not).
func (c *liveController) commit(repo string, mutate func(config.WebLiveRepository) config.WebLiveRepository) bool {
	c.mu.Lock()
	name, current, ok := c.state.Repository(repo)
	if !ok {
		name = repo
		for _, configured := range c.repos {
			if strings.EqualFold(configured, repo) {
				name = configured
			}
		}
	}
	wasHealthy := c.tunnelUp && current.State == config.WebLiveStateLive
	next := mutate(current)
	if next != current {
		c.state.Repositories[name] = next
		c.dirty = true
	}
	becameHealthy := !wasHealthy && c.tunnelUp && next.State == config.WebLiveStateLive
	c.mu.Unlock()
	if current.State != next.State {
		log.Printf("live updates: %s is %s", name, describeLiveState(next))
	}
	if becameHealthy {
		c.refreshRepository(name)
	}
	return current.State != next.State
}

func (c *liveController) refreshRepository(repo string) {
	if c.sync == nil {
		return
	}
	queue := c.queue
	if queue == nil {
		queue = c.sync.Queue
	}
	for _, id := range c.sync.liveProjectsByName(repo) {
		queue(id, refreshProvider, true)
	}
}

// persist writes the live state to web-state.json when it changed.
func (c *liveController) persist() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	live := c.state
	live.Repositories = map[string]config.WebLiveRepository{}
	for name, r := range c.state.Repositories {
		if c.configured(name) {
			live.Repositories[name] = r
		} else {
			// Setup owns the entries of repositories taken off live updates
			// (it deletes them with their hook); keep whatever it left.
			delete(c.state.Repositories, name)
		}
	}
	live.UpdatedAt = c.now()
	c.dirty = false
	c.lastWrite = live.UpdatedAt
	c.mu.Unlock()
	if _, err := config.UpdateWebState(c.statePath, func(state *config.WebState) error {
		// The install ID is only ever generated, never changed here.
		live.InstallID = state.Live.InstallID
		if live.InstallID == "" {
			live.InstallID = c.installID()
		}
		for name, r := range state.Live.Repositories {
			if _, ok := live.Repositories[name]; !ok && !c.isConfigured(name) {
				live.Repositories[name] = r
			}
		}
		state.Live = live
		return nil
	}); err != nil {
		log.Printf("live updates: save %s: %v", c.statePath, err)
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
	}
}

// configured reports whether repo is one of web.json's live repositories;
// c.mu must be held.
func (c *liveController) configured(repo string) bool {
	for _, r := range c.repos {
		if strings.EqualFold(r, repo) {
			return true
		}
	}
	return false
}

func (c *liveController) isConfigured(repo string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.configured(repo)
}

func (c *liveController) installID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.InstallID
}

// healthy reports whether live updates currently cover repo: the tunnel is
// up and its hook works. Polling slows to a safety net for such projects.
func (c *liveController) healthy(repo string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.tunnelUp {
		return false
	}
	for _, configured := range c.repos {
		if strings.EqualFold(configured, repo) {
			_, r, ok := c.state.Repository(configured)
			return ok && r.State == config.WebLiveStateLive
		}
	}
	return false
}

// liveView is the controller state shown to the browser and the CLI.
type liveView struct {
	Tunnel       string
	PublicURL    string
	TunnelUp     bool
	TunnelError  string
	Repositories []liveRepositoryView
}

type liveRepositoryView struct {
	FullName string
	config.WebLiveRepository
}

func (c *liveController) view() liveView {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := liveView{Tunnel: c.options.Preset, PublicURL: c.state.PublicURL, TunnelUp: c.tunnelUp, TunnelError: c.state.TunnelError}
	for _, repo := range c.repos {
		_, r, _ := c.state.Repository(repo)
		out.Repositories = append(out.Repositories, liveRepositoryView{FullName: repo, WebLiveRepository: r})
	}
	return out
}

func describeLiveState(r config.WebLiveRepository) string {
	if r.State == config.WebLiveStateFailing && r.LastError != "" {
		return "failing (" + r.LastError + ")"
	}
	if r.State == "" {
		return "not set up yet"
	}
	return r.State
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
