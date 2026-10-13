package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func modelByID(models []agents.ModelOption, id string) (agents.ModelOption, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return agents.ModelOption{}, false
}

func TestModelsFromAppServer(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	models, err := e.provider.Models(t.Context(), account)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 7 {
		t.Fatalf("models = %d: %+v", len(models), models)
	}
	if models[0].ID != "gpt-6.1-sol" || !models[0].Default || models[0].Source != "api" || models[0].Label != "GPT-6.1-Sol" {
		t.Fatalf("first = %+v", models[0])
	}
	if want := []string{"low", "medium", "high", "xhigh", "max", "ultra"}; !reflect.DeepEqual(models[0].Efforts, want) || models[0].DefaultEffort != "low" {
		t.Fatalf("efforts = %v default %q", models[0].Efforts, models[0].DefaultEffort)
	}
	luna, ok := modelByID(models, "gpt-6-luna")
	if !ok || strings.Contains(strings.Join(luna.Efforts, ","), "ultra") || luna.DefaultEffort != "medium" {
		t.Fatalf("luna = %+v", luna)
	}
	if want := []string{"rpc initialize", "rpc initialized", "rpc model/list"}; !strings.HasSuffix(strings.TrimSpace(e.calls()), strings.Join(want, "\n")) {
		t.Fatalf("request order = %q", e.calls())
	}
	if !strings.Contains(e.calls(), `store="file"`) {
		t.Fatalf("credential store override missing: %q", e.calls())
	}
}

func TestModelsDropHiddenAndPutDefaultFirst(t *testing.T) {
	e := newTestEnv(t)
	e.fixtures(map[string]string{"model-list.json": `{"id":4,"result":{"data":[
		{"id":"a","displayName":"A","hidden":false,"isDefault":false,"supportedReasoningEfforts":[{"reasoningEffort":"low"}],"defaultReasoningEffort":"low"},
		{"id":"secret","displayName":"Secret","hidden":true,"isDefault":false,"supportedReasoningEfforts":[]},
		{"id":"b","displayName":"B","hidden":false,"isDefault":true,"supportedReasoningEfforts":[{"reasoningEffort":"high"},{"reasoningEffort":"BAD EFFORT"},{"reasoningEffort":"high"}],"defaultReasoningEffort":"nope"},
		{"id":"a","displayName":"dup","hidden":false,"supportedReasoningEfforts":[]},
		{"id":"-flag","displayName":"flag","hidden":false,"supportedReasoningEfforts":[]}
	],"nextCursor":null}}`})
	account := e.addLogin("work")
	models, _ := e.provider.Models(t.Context(), account)
	var ids []string
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a"}) {
		t.Fatalf("ids = %v", ids)
	}
	if !reflect.DeepEqual(models[0].Efforts, []string{"high"}) || models[0].DefaultEffort != "" {
		t.Fatalf("b = %+v", models[0])
	}
}

func TestModelsFollowCursor(t *testing.T) {
	e := newTestEnv(t)
	// The fake answers every page with the same fixture; a non-empty cursor
	// would loop forever without the page cap.
	e.fixtures(map[string]string{"model-list.json": `{"id":4,"result":{"data":[{"id":"a","displayName":"A","supportedReasoningEfforts":[]}],"nextCursor":"more"}}`})
	account := e.addLogin("work")
	models, _ := e.provider.Models(t.Context(), account)
	if len(models) != 1 || models[0].ID != "a" {
		t.Fatalf("models = %+v", models)
	}
	if n := len(e.rpcs()); n != 2+maxModelPages {
		t.Fatalf("rpcs = %v", e.rpcs())
	}
}

func TestModelsFallBackToBundledList(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_APPSERVER", "exit")
	account := e.addLogin("work")
	models, err := e.provider.Models(t.Context(), account)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 8 {
		t.Fatalf("models = %d (hidden ones must be dropped): %+v", len(models), models)
	}
	if models[0].ID != "gpt-6.1-sol" || !models[0].Default || models[0].Source != "bundled" {
		t.Fatalf("first = %+v", models[0])
	}
	if _, ok := modelByID(models, "codex-auto-review"); ok {
		t.Fatal("hidden model listed")
	}
	if got := e.calls(); !strings.Contains(got, "debug-models home="+e.profileHome(account)) {
		t.Fatalf("calls = %q", got)
	}
}

func TestModelsLoggedOutProfileDoesNotStartAppServer(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(authPath(e.profileHome(account))); err != nil {
		t.Fatal(err)
	}
	models, err := e.provider.Models(t.Context(), account)
	if err != nil || len(models) == 0 || models[0].Source != "bundled" {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if e.spawns() != 0 {
		t.Fatalf("spawns = %d", e.spawns())
	}
}

func TestModelsNeverError(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_APPSERVER", "exit")
	t.Setenv("FAKE_CODEX_DEBUG_FAIL", "1")
	account := e.addLogin("work")
	models, err := e.provider.Models(t.Context(), account)
	if err != nil || len(models) != 0 {
		t.Fatalf("models = %+v, %v", models, err)
	}
}

func TestModelsCancelledCallerGetsNoError(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.provider.Models(ctx, account); err != nil {
		t.Fatal(err)
	}
}

func TestModelsAreCachedWithDifferentTTLs(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	now := e.clock(time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	e.provider.Models(t.Context(), account)
	e.provider.Models(t.Context(), account)
	if e.spawns() != 1 {
		t.Fatalf("spawns = %d, want the second call cached", e.spawns())
	}
	*now = now.Add(modelsTTL - time.Second)
	e.provider.Models(t.Context(), account)
	if e.spawns() != 1 {
		t.Fatalf("spawns = %d before the TTL", e.spawns())
	}
	*now = now.Add(2 * time.Second)
	e.provider.Models(t.Context(), account)
	if e.spawns() != 2 {
		t.Fatalf("spawns = %d after the TTL", e.spawns())
	}

	// Failures (the bundled fallback) are retried after a minute, not ten.
	t.Setenv("FAKE_CODEX_APPSERVER", "exit")
	other := e.addLogin("other")
	e.provider.Models(t.Context(), other)
	before := e.spawns()
	*now = now.Add(modelsFailureTTL - time.Second)
	e.provider.Models(t.Context(), other)
	if e.spawns() != before {
		t.Fatal("failure retried too early")
	}
	*now = now.Add(2 * time.Second)
	e.provider.Models(t.Context(), other)
	if e.spawns() != before+1 {
		t.Fatalf("spawns = %d, want one retry", e.spawns())
	}
}

func TestConcurrentModelsShareOneProcess(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	var wg sync.WaitGroup
	results := make([]int, 10)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			models, err := e.provider.Models(t.Context(), account)
			if err != nil {
				t.Error(err)
			}
			results[i] = len(models)
		}()
	}
	wg.Wait()
	if e.spawns() != 1 {
		t.Fatalf("spawns = %d, want 1", e.spawns())
	}
	for i, n := range results {
		if n != 7 {
			t.Fatalf("caller %d got %d models", i, n)
		}
	}
}

func TestLaunchEffortFollowsCachedModelList(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	validate := func(model, effort string) error {
		return e.provider.ValidateLaunch(account, agents.LaunchOptions{Model: model, Effort: effort})
	}
	// Before the model list is known, any shipped level is accepted.
	if err := validate("gpt-6-luna", "ultra"); err != nil {
		t.Fatalf("unknown list: %v", err)
	}
	if _, err := e.provider.Models(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	if err := validate("gpt-6-luna", "ultra"); err == nil || !strings.Contains(err.Error(), "not supported by this model") {
		t.Fatalf("luna ultra: %v", err)
	}
	for _, ok := range []struct{ model, effort string }{
		{"gpt-6.1-sol", "ultra"}, {"gpt-6-luna", "max"}, {"", "ultra"}, {"unlisted-model", "ultra"}, {"gpt-6-luna", ""},
	} {
		if err := validate(ok.model, ok.effort); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	if err := validate("unlisted-model", "turbo"); err == nil {
		t.Fatal("effort outside every shipped level must be rejected")
	}
	// The effective model also counts: a profile default model supplies the list.
	settings := agents.Account{ID: account.ID, Provider: ProviderID, Name: account.Name, Settings: Settings{AuthMode: AuthChatGPT, Model: "gpt-6-luna"}.raw()}
	if err := e.provider.ValidateLaunch(settings, agents.LaunchOptions{Effort: "ultra"}); err == nil {
		t.Fatal("profile default model must constrain the effort")
	}
}

func TestParseBundledModels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "phase0", "bundled-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseBundledModels(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 8 || models[0].ID != "gpt-6.1-sol" {
		t.Fatalf("models = %+v", models)
	}
	if _, err := ParseBundledModels([]byte("nope")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestModelOptionsFlagModelsWithoutEfforts(t *testing.T) {
	options := modelOptions([]appModel{{ID: "plain"}, {ID: "smart", DefaultReasoningEffort: "high", SupportedReasoningEfforts: []struct {
		ReasoningEffort string `json:"reasoningEffort"`
	}{{"high"}}}}, "api")
	if len(options) != 2 || !options[0].NoEffort || options[1].NoEffort || options[1].DefaultEffort != "high" {
		t.Fatalf("options = %+v", options)
	}
}
