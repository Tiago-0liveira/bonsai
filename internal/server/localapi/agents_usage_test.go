//go:build linux || darwin

package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// meterTestProvider reports usage from a table, counts reads and records
// whether each one bypassed the cache.
type meterTestProvider struct {
	describedTestProvider
	mu        sync.Mutex
	remaining map[agents.AccountID]float64
	fail      map[agents.AccountID]error
	reads     []string
}

func (p *meterTestProvider) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true, Usage: true, MultiAccount: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
}
func (p *meterTestProvider) Usage(_ context.Context, account agents.Account, opts agents.UsageOptions) (agents.UsageSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads = append(p.reads, fmt.Sprintf("%s:%v", account.Name, opts.Refresh))
	if err := p.fail[account.ID]; err != nil {
		return agents.UsageSnapshot{}, err
	}
	five, week := p.remaining[account.ID], 0.9
	reset := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	return agents.UsageSnapshot{Plan: "pro", Limits: []agents.UsageLimit{
		{ID: "five_hour", Window: "5h", RemainingFraction: &five},
		{ID: "weekly", Window: "weekly", RemainingFraction: &week, ResetsAt: &reset},
		{ID: "model_x", Window: "weekly", RemainingFraction: &week},
	}}, nil
}
func (p *meterTestProvider) UsageHeadline(s agents.UsageSnapshot) agents.UsageHeadline {
	return agents.NewUsageHeadline(&s.Limits[0], &s.Limits[1], s.Limits[2:])
}
func (p *meterTestProvider) UsageNote() string { return "Read from a test meter." }
func (p *meterTestProvider) takeReads() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	reads := p.reads
	p.reads = nil
	sort.Strings(reads)
	return reads
}

// usageTestServer adds a usage service and the meter provider with two
// profiles, Alpha (low) and Beta.
func usageTestServer(t *testing.T) (*Server, *meterTestProvider, agents.AccountID, agents.AccountID) {
	t.Helper()
	s, _, _ := terminalTestServer(t)
	runtime := s.agents.runtime
	cache, err := agents.NewFileUsageCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime.UsageService = &agents.UsageService{Accounts: runtime.Accounts, Registry: runtime.Registry, Cache: cache, TTL: time.Hour}
	meter := &meterTestProvider{describedTestProvider: describedTestProvider{id: "meter", available: agents.Availability{Available: true}}, remaining: map[agents.AccountID]float64{}, fail: map[agents.AccountID]error{}}
	if err := runtime.Registry.Register(meter); err != nil {
		t.Fatal(err)
	}
	create := func(name string, remaining float64) agents.AccountID {
		id, _ := agents.NewAccountID()
		if err := runtime.Accounts.Create(agents.Account{ID: id, Provider: "meter", Name: name}); err != nil {
			t.Fatal(err)
		}
		meter.remaining[id] = remaining
		return id
	}
	return s, meter, create("Alpha", 0.1), create("Beta", 0.6)
}

func decodeUsage(t *testing.T, body []byte) map[agents.AccountID]agentUsageItem {
	t.Helper()
	var items []agentUsageItem
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	out := map[agents.AccountID]agentUsageItem{}
	for _, item := range items {
		out[item.AccountID] = item
	}
	return out
}

func TestAgentUsageCachedRefreshAndThrottle(t *testing.T) {
	s, meter, alpha, beta := usageTestServer(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s.agents.usageThrottle.now = func() time.Time { return now }
	get := func(path string) map[agents.AccountID]agentUsageItem {
		t.Helper()
		w := agentAPIRequest(t, s, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		return decodeUsage(t, w.Body.Bytes())
	}
	refresh := func(body string) map[agents.AccountID]agentUsageItem {
		t.Helper()
		w := agentAPIRequest(t, s, "POST", "/api/agents/usage/refresh", "", body)
		if w.Code != 200 {
			t.Fatalf("refresh %s: %d %s", body, w.Code, w.Body.String())
		}
		return decodeUsage(t, w.Body.Bytes())
	}

	// The list holds every profile with usage; the stub Antigravity has none.
	items := get("/api/agents/usage")
	if len(items) != 2 || !reflect.DeepEqual(meter.takeReads(), []string{"Alpha:false", "Beta:false"}) {
		t.Fatalf("first read: %+v", items)
	}
	a := items[alpha]
	if a.Provider != "meter" || a.Usage == nil || a.Headline == nil || a.Error != nil || a.Throttled {
		t.Fatalf("alpha = %+v", a)
	}
	if h := a.Headline; h.Primary.ID != "five_hour" || h.Secondary.ID != "weekly" || len(h.Extra) != 1 || *h.Score != 0.1 || !h.Low || h.NextReset == nil {
		t.Fatalf("alpha headline = %+v", h)
	}
	if b := items[beta].Headline; b.Low || *b.Score != 0.6 {
		t.Fatalf("beta headline = %+v", b)
	}
	// Fresh snapshots come from the cache.
	if items := get("/api/agents/usage?account_id=" + string(alpha)); len(items) != 1 || items[alpha].Usage == nil || len(meter.takeReads()) != 0 {
		t.Fatalf("filtered cached read: %+v", items)
	}

	// A refresh bypasses the cache once per 30 s per profile.
	if items := refresh(`{"account_id":"` + string(alpha) + `"}`); len(items) != 1 || items[alpha].Throttled || !reflect.DeepEqual(meter.takeReads(), []string{"Alpha:true"}) {
		t.Fatalf("refresh: %+v", items)
	}
	now = now.Add(29 * time.Second)
	if items := refresh(`{"account_id":"` + string(alpha) + `"}`); !items[alpha].Throttled || items[alpha].Usage == nil || len(meter.takeReads()) != 0 {
		t.Fatalf("throttled refresh: %+v", items)
	}
	// Refreshing all: Alpha is still throttled, Beta is not.
	items = refresh(`{}`)
	if !items[alpha].Throttled || items[beta].Throttled || !reflect.DeepEqual(meter.takeReads(), []string{"Beta:true"}) {
		t.Fatalf("refresh all: %+v", items)
	}
	now = now.Add(2 * time.Second)
	// An empty body refreshes all too.
	items = refresh("")
	if items[alpha].Throttled || !items[beta].Throttled || !reflect.DeepEqual(meter.takeReads(), []string{"Alpha:true"}) {
		t.Fatalf("refresh after 31 s: %+v", items)
	}

	if w := agentAPIRequest(t, s, "POST", "/api/agents/usage/refresh", "", `{"account":"x"}`); w.Code != 400 {
		t.Fatalf("unknown field: %d %s", w.Code, w.Body.String())
	}
	if w := agentAPIRequest(t, s, "POST", "/api/agents/usage/refresh", "", `{"account_id":"`+string(alpha)+`"} {}`); w.Code != 400 {
		t.Fatalf("trailing value: %d %s", w.Code, w.Body.String())
	}
}

func TestAgentUsageHeadlineJSONShape(t *testing.T) {
	s, _, alpha, _ := usageTestServer(t)
	w := agentAPIRequest(t, s, "GET", "/api/agents/usage?account_id="+string(alpha), "", "")
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || len(items) != 1 {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	keys := func(m map[string]json.RawMessage) []string {
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got := keys(items[0]); !reflect.DeepEqual(got, []string{"account_id", "headline", "provider", "usage"}) {
		t.Fatalf("item keys = %v", got)
	}
	var headline map[string]json.RawMessage
	_ = json.Unmarshal(items[0]["headline"], &headline)
	if got := keys(headline); !reflect.DeepEqual(got, []string{"extra", "low", "next_reset", "primary", "score", "secondary"}) {
		t.Fatalf("headline keys = %v", got)
	}
	if string(headline["score"]) != "0.1" || string(headline["low"]) != "true" || string(headline["next_reset"]) != `"2026-10-15T09:00:00Z"` {
		t.Fatalf("headline = %s", items[0]["headline"])
	}
	var primary map[string]any
	_ = json.Unmarshal(headline["primary"], &primary)
	if primary["id"] != "five_hour" || primary["window"] != "5h" || primary["remaining_fraction"] != 0.1 {
		t.Fatalf("primary = %v", primary)
	}
}

func TestAgentUsageErrors(t *testing.T) {
	s, meter, alpha, beta := usageTestServer(t)
	runtime := s.agents.runtime
	gamma, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: gamma, Provider: "meter", Name: "Gamma"}); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-secret-response-body"
	meter.fail[alpha] = fmt.Errorf("%w: API key profiles have no plan limits", agents.ErrUsageUnsupported)
	meter.fail[beta] = fmt.Errorf("%w: run `login` (%s)", agents.ErrNotAuthenticated, secret)
	meter.fail[gamma] = errors.New("HTTP 500: " + secret)
	// A profile of an unregistered provider is hidden, as in the profile list.
	hidden, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: hidden, Provider: "codex", Name: "Hidden"}); err != nil {
		t.Fatal(err)
	}
	var agy agents.AccountID
	if accounts, _ := runtime.Accounts.List(); true {
		for _, a := range accounts {
			if a.Provider == "antigravity" {
				agy = a.ID
			}
		}
	}

	w := agentAPIRequest(t, s, "GET", "/api/agents/usage", "", "")
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("provider text leaked: %s", w.Body.String())
	}
	items := decodeUsage(t, w.Body.Bytes())
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	for id, want := range map[agents.AccountID]agentUsageError{
		alpha: {Code: "unsupported", Message: "API key profiles have no plan limits"},
		beta:  {Code: "not_authenticated", Message: "Not signed in"},
		gamma: {Code: "unavailable", Message: "Usage is unavailable right now"},
	} {
		if got := items[id]; got.Error == nil || *got.Error != want || got.Usage != nil || got.Headline != nil {
			t.Errorf("%s = %+v, want %+v", id, got, want)
		}
	}

	// A provider without usage answers unsupported for its profile.
	w = agentAPIRequest(t, s, "GET", "/api/agents/usage?account_id="+string(agy), "", "")
	if items := decodeUsage(t, w.Body.Bytes()); w.Code != 200 || items[agy].Error == nil || items[agy].Error.Code != "unsupported" || items[agy].Error.Message != "Usage is not supported for this profile" {
		t.Fatalf("antigravity stub: %d %s", w.Code, w.Body.String())
	}
	for name, id := range map[string]string{"unknown": "acct_missing", "unregistered provider": string(hidden), "malformed": "../x"} {
		if w := agentAPIRequest(t, s, "GET", "/api/agents/usage?account_id="+id, "", ""); w.Code != 404 {
			t.Errorf("GET %s: %d %s", name, w.Code, w.Body.String())
		}
		if w := agentAPIRequest(t, s, "POST", "/api/agents/usage/refresh", "", `{"account_id":"`+id+`"}`); w.Code != 404 {
			t.Errorf("refresh %s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestAgentUsageRealProviders(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	codexTestdata, err := filepath.Abs(filepath.Join("..", "..", "providers", "codex", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(filepath.Join(codexTestdata, "fakecodex.sh"), filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	// Claude token setup needs a claude binary on PATH; the fake never logs in.
	claudeTestdata, err := filepath.Abs(filepath.Join("..", "..", "providers", "claude", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(claudeTestdata, "fakeclaude.sh"), filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FAKE_CODEX_FIXTURES", filepath.Join(codexTestdata, "phase0"))
	t.Setenv("FAKE_CLAUDE_FIXTURES", claudeTestdata)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claude.UsageEndpoint = "http://127.0.0.1:1/usage"
	runtime := s.agents.runtime
	launcher := agents.NewForegroundLauncher(strings.NewReader(""), io.Discard, io.Discard)
	for _, p := range []agents.Provider{codex.New(runtime.Accounts, runtime.Sessions), claude.New(runtime.Accounts, runtime.Sessions)} {
		if err := runtime.Registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	runtime.UsageService = &agents.UsageService{Accounts: runtime.Accounts, Registry: runtime.Registry, TTL: time.Hour}
	service := &agents.AccountService{Store: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry, Launcher: launcher}
	noSeed := false
	signedIn, err := service.Setup(context.Background(), codex.ProviderID, "Personal", agents.SetupOptions{AuthMode: codex.AuthChatGPT, Seed: &noSeed})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := service.Setup(context.Background(), codex.ProviderID, "CI", agents.SetupOptions{AuthMode: codex.AuthAPIKey, Secret: strings.NewReader("sk-proj-FIXTUREFIXTUREFIXTUREFIXTURE\n"), Seed: &noSeed})
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Setup(context.Background(), claude.ProviderID, "Bot", agents.SetupOptions{Secret: strings.NewReader("sk-ant-oat01-FIXTUREFIXTUREFIXTUREFIXTUREFIXTURE\n"), Seed: &noSeed})
	if err != nil {
		t.Fatal(err)
	}

	w := agentAPIRequest(t, s, "GET", "/api/agents/usage", "", "")
	for _, secret := range []string{"sk-proj-FIXTURE", "sk-ant-oat01"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("secret leaked: %s", w.Body.String())
		}
	}
	items := decodeUsage(t, w.Body.Bytes())
	got := items[signedIn.ID]
	if got.Usage == nil || got.Usage.Plan != "Plus" || got.Headline == nil || got.Headline.Primary.ID != "five_hour" || got.Headline.Secondary.ID != "weekly" || *got.Headline.Score != 0.72 {
		t.Fatalf("codex signed in = %+v", got)
	}
	if len(got.Usage.ResetCredits) != 3 || len(got.Usage.Credits) != 1 || !strings.Contains(w.Body.String(), `"reset_credits":[{"title":"Full reset (Weekly + 5 hr)","status":"available","granted_at":`) {
		t.Fatalf("codex credits = %+v / %+v", got.Usage.Credits, got.Usage.ResetCredits)
	}
	if got := items[apiKey.ID]; got.Error == nil || got.Error.Code != "unsupported" || got.Error.Message != "API key profiles have no plan limits" {
		t.Fatalf("codex API key = %+v", got)
	}
	if got := items[token.ID]; got.Error == nil || got.Error.Code != "unsupported" || got.Error.Message != "long-lived tokens cannot read usage" {
		t.Fatalf("claude token = %+v", got)
	}

	w = agentAPIRequest(t, s, "GET", "/api/agents/providers", "", "")
	var providers []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &providers)
	for _, p := range providers {
		note, hasNote := p["usage_note"]
		if p["id"] == "claude" && (!hasNote || !strings.Contains(note.(string), "undocumented endpoint")) {
			t.Fatalf("claude = %v", p)
		}
		if p["id"] == "codex" && (hasNote || p["install_hint"] != nil || p["available"] != true) {
			t.Fatalf("codex = %v", p)
		}
	}
	setupJSON := map[string]string{}
	var raw []map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	for _, p := range raw {
		var id string
		_ = json.Unmarshal(p["id"], &id)
		setupJSON[id] = string(p["setup"])
	}
	for id, want := range map[string][]string{
		"codex":  {`{"value":"chatgpt","label":"Browser sign-in"`, `"value":"device"`, `"secret":{"label":"OpenAI API key","placeholder":"sk-…"}`, `"seed":{"source":"~/.codex"}`},
		"claude": {`{"value":"login","label":"Browser sign-in"`, `"secret":{"label":"Setup token","placeholder":"sk-ant-oat01-…","optional":true}`, `"seed":{"source":"~/.claude"}`},
	} {
		for _, part := range want {
			if !strings.Contains(setupJSON[id], part) {
				t.Errorf("%s setup = %s, lacks %s", id, setupJSON[id], part)
			}
		}
	}
	// The stub Antigravity declares no setup schema.
	if setupJSON["antigravity"] != "" {
		t.Errorf("antigravity setup = %s", setupJSON["antigravity"])
	}
}

func TestAgentProvidersUsageNoteAndInstallHint(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	registry := s.agents.runtime.Registry
	missing := &meterTestProvider{describedTestProvider: describedTestProvider{id: "missing", available: agents.Availability{Reason: "Install missing", Hint: "npm install -g missing"}}}
	installed := &meterTestProvider{describedTestProvider: describedTestProvider{id: "installed", available: agents.Availability{Available: true, Version: "1.0.0", Hint: "never shown"}}}
	for _, p := range []agents.Provider{missing, installed} {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	w := agentAPIRequest(t, s, "GET", "/api/agents/providers", "", "")
	var providers []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &providers); err != nil {
		t.Fatal(w.Body.String())
	}
	byID := map[string]map[string]any{}
	for _, p := range providers {
		byID[p["id"].(string)] = p
	}
	if p := byID["missing"]; p["install_hint"] != "npm install -g missing" || p["usage_note"] != "Read from a test meter." || p["available"] != false {
		t.Fatalf("missing = %v", p)
	}
	if p := byID["installed"]; p["install_hint"] != nil || p["usage_note"] != "Read from a test meter." {
		t.Fatalf("installed = %v", p)
	}
	// The stub Antigravity has neither.
	if p := byID["antigravity"]; p["install_hint"] != nil || p["usage_note"] != nil {
		t.Fatalf("antigravity = %v", p)
	}
}

func TestAgentAccountsETag(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	get := func(etag string) (int, string, []map[string]any) {
		t.Helper()
		w := agentAPIRequestHeaders(t, s, "GET", "/api/agents/accounts", map[string]string{"If-None-Match": etag})
		var out []map[string]any
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(w.Body.String())
			}
		} else if w.Body.Len() != 0 {
			t.Fatalf("%d with a body: %s", w.Code, w.Body.String())
		}
		return w.Code, w.Header().Get("ETag"), out
	}
	code, etag, accounts := get("")
	if code != 200 || !strings.HasPrefix(etag, `"`) || len(accounts) != 1 {
		t.Fatalf("first: %d %q %v", code, etag, accounts)
	}
	created, createdOK := accounts[0]["created_at"].(string)
	updated, updatedOK := accounts[0]["updated_at"].(string)
	if !createdOK || !updatedOK || created == "" || updated == "" {
		t.Fatalf("timestamps = %v", accounts[0])
	}
	if code, again, _ := get(etag); code != 304 || again != etag {
		t.Fatalf("revalidate: %d %q", code, again)
	}
	if code, _, _ := get(`"other", W/` + etag); code != 304 {
		t.Fatalf("weak match in a list: %d", code)
	}
	// The CLI is another process with its own store over the same files.
	cli, err := agents.NewFileAccountStore(s.agents.runtime.Accounts.(*agents.FileAccountStore).Root())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := cli.Create(agents.Account{ID: id, Provider: "antigravity", Name: "FromCLI"}); err != nil {
		t.Fatal(err)
	}
	code, changed, accounts := get(etag)
	if code != 200 || changed == etag || len(accounts) != 2 {
		t.Fatalf("after CLI add: %d %q %v", code, changed, accounts)
	}
	if code, _, _ := get(changed); code != 304 {
		t.Fatalf("new tag: %d", code)
	}
}

// agentAPIRequestHeaders sends a bodiless request with extra headers.
func agentAPIRequestHeaders(t *testing.T, s *Server, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	capability, _ := s.sessions.create()
	r := httptest.NewRequest(method, "http://"+s.expectedHost+path, nil)
	r.Host = s.expectedHost
	r.Header.Set("Origin", "http://"+s.expectedHost)
	r.Header.Set("X-Bonsai-Session", capability.Token)
	for k, v := range headers {
		if v != "" {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAgentSummaryLaunchDetailsAndEvents(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	runtime := s.agents.runtime
	preset := &presetTestProvider{describedTestProvider: describedTestProvider{id: "preset"}, launches: make(chan agents.LaunchOptions, 4)}
	if err := runtime.Registry.Register(preset); err != nil {
		t.Fatal(err)
	}
	account, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: account, Provider: "preset", Name: "Preset"}); err != nil {
		t.Fatal(err)
	}
	subscription, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscription)
	changed := func() []uint64 {
		var revisions []uint64
		for {
			select {
			case event := <-events:
				if event.Type == agentsChangedEvent {
					if event.ProjectID != "" || event.Snapshot != nil || event.Projects != nil {
						t.Fatalf("agents_changed carries a payload: %+v", event)
					}
					revisions = append(revisions, event.Revision)
				}
			case <-time.After(200 * time.Millisecond):
				return revisions
			}
		}
	}

	const prompt = "PROMPT-MARKER do the thing"
	tree := gitlocal.ID(localRepositoryID, s.registry.Default().info.Path)
	body := fmt.Sprintf(`{"worktree_id":%q,"account_id":%q,"cols":80,"rows":24,"model":"m-1","prompt":%q,"sandbox":"workspace-write","approval":"on-request"}`, tree, account, prompt)
	w := agentAPIRequest(t, s, "POST", "/api/projects/repo/agents", "launch", body)
	if w.Code != 202 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if got := <-preset.launches; got.Prompt != prompt {
		t.Fatalf("provider did not get the prompt: %+v", got)
	}
	var summary agentterminal.Summary
	_ = json.Unmarshal(w.Body.Bytes(), &summary)
	want := map[string]string{"sandbox": "workspace-write", "approval": "on-request"}
	if summary.Model != "m-1" || !reflect.DeepEqual(summary.Options, want) || strings.Contains(w.Body.String(), "PROMPT-MARKER") {
		t.Fatalf("summary = %s", w.Body.String())
	}
	waitFor(t, func() bool {
		current, _ := s.agents.manager.Get("repo", summary.ID)
		return current.State == "running"
	})
	started := changed()
	if len(started) == 0 {
		t.Fatal("no agents_changed on start")
	}
	for i := 1; i < len(started); i++ {
		if started[i] <= started[i-1] {
			t.Fatalf("revisions not increasing: %v", started)
		}
	}
	if data, _ := json.Marshal(localEvent{Type: agentsChangedEvent, Revision: started[0], ProjectID: "repo"}.wire()); string(data) != fmt.Sprintf(`{"type":"agents_changed","revision":%d}`, started[0]) {
		t.Fatalf("wire = %s", data)
	}

	// Terminal bytes never announce a change.
	subscriber, _, _, frames, err := s.agents.manager.Attach("repo", summary.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.agents.manager.Detach(summary.ID, subscriber)
	if err := s.agents.manager.Input(summary.ID, subscriber, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	echoed := ""
	for !strings.Contains(echoed, "REPLY:hello") {
		select {
		case frame := <-frames:
			echoed += string(frame.Data)
		case <-time.After(5 * time.Second):
			t.Fatalf("no echo: %q", echoed)
		}
	}
	if got := changed(); len(got) != 0 {
		t.Fatalf("agents_changed on terminal bytes: %v", got)
	}

	// The journal and the browser snapshot keep the launch details, never the prompt.
	project := s.registry.Default()
	var journal []byte
	_ = project.state.View(func(data gitstore.Data) error {
		journal, _ = json.Marshal(data["api_agent_mutations"])
		record, _ := gitstore.Get[agentMutation](data, "api_agent_mutations", "launch")
		if record.Summary.Model != "m-1" || !reflect.DeepEqual(record.Summary.Options, want) {
			t.Errorf("journal summary = %+v", record.Summary)
		}
		return nil
	})
	snapshot, ok := s.stateSync.CachedSnapshot("repo")
	snapshotJSON, _ := json.Marshal(snapshot)
	if !ok || strings.Contains(string(journal), "PROMPT-MARKER") || strings.Contains(string(snapshotJSON), "PROMPT-MARKER") || !strings.Contains(string(snapshotJSON), `"model":"m-1"`) {
		t.Fatalf("journal %s\nsnapshot %s", journal, snapshotJSON)
	}

	if w := agentAPIRequest(t, s, "DELETE", "/api/projects/repo/agents/"+summary.ID, "stop", ""); w.Code != 202 {
		t.Fatalf("stop: %d %s", w.Code, w.Body.String())
	}
	waitFor(t, func() bool {
		current, _ := s.agents.manager.Get("repo", summary.ID)
		return !current.Active()
	})
	if stopped := changed(); len(stopped) == 0 || stopped[0] <= started[len(started)-1] {
		t.Fatalf("agents_changed on stop: %v after %v", stopped, started)
	}
}
