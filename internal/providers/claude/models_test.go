package claude

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func modelIDs(models []agents.ModelOption) string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return strings.Join(ids, ",")
}

const aliasIDs = "opus,sonnet,haiku,fable"

func TestModelsListsAliasesThenAPIModels(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	server := newUsageServer(t, 200, readFixture(t, "models-response.json"))
	e.provider.modelsEndpoint = server.URL + "/v1/models"

	models, err := e.provider.Models(t.Context(), account)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := modelIDs(models), aliasIDs+",claude-opus-5-5,claude-sonnet-5-5,claude-haiku-4-5-20251001"; got != want {
		t.Fatalf("models = %s, want %s", got, want)
	}
	if models[0].Source != "alias" || models[4].Source != "api" || models[4].Label != "Claude Opus 5.5" || models[6].Label != "claude-haiku-4-5-20251001" {
		t.Fatalf("models = %+v", models)
	}
	req := server.last.Load()
	if req.Header.Get("Authorization") != "Bearer access-abc" || req.Header.Get("anthropic-beta") == "" || req.Header.Get("anthropic-version") == "" {
		t.Fatalf("headers = %v", req.Header)
	}
	// Cached: a second call makes no request.
	if _, err := e.provider.Models(t.Context(), account); err != nil || server.hits.Load() != 1 {
		t.Fatalf("hits = %d err = %v", server.hits.Load(), err)
	}
}

func TestModelsTokenProfileUsesStoredToken(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	server := newUsageServer(t, 200, readFixture(t, "models-response.json"))
	e.provider.modelsEndpoint = server.URL
	models, _ := e.provider.Models(t.Context(), account)
	if len(models) != 7 || server.last.Load().Header.Get("Authorization") != "Bearer "+testToken {
		t.Fatalf("models = %s, auth = %v", modelIDs(models), server.last.Load().Header.Get("Authorization"))
	}
}

func TestModelsFallBackToAliases(t *testing.T) {
	for name, setup := range map[string]func(e *testEnv, account agents.Account) (status int){
		"rejected": func(e *testEnv, a agents.Account) int {
			writeCredentials(t, e, a, "x", time.Now().Add(time.Hour))
			return 403
		},
		"server error": func(e *testEnv, a agents.Account) int {
			writeCredentials(t, e, a, "x", time.Now().Add(time.Hour))
			return 500
		},
		"garbage": func(e *testEnv, a agents.Account) int {
			writeCredentials(t, e, a, "x", time.Now().Add(time.Hour))
			return 200
		},
		"expired login": func(e *testEnv, a agents.Account) int {
			writeCredentials(t, e, a, "x", time.Now().Add(-time.Hour))
			return 200
		},
		"logged out": func(e *testEnv, a agents.Account) int { return 200 },
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			account := e.addLogin("work")
			status := setup(e, account)
			if name == "logged out" {
				_ = removeCredentials(e, account)
			}
			server := newUsageServer(t, status, []byte(`<html>not json</html>`))
			e.provider.modelsEndpoint = server.URL
			models, err := e.provider.Models(t.Context(), account)
			if err != nil || modelIDs(models) != aliasIDs {
				t.Fatalf("models = %s, err = %v", modelIDs(models), err)
			}
			if (name == "expired login" || name == "logged out") && server.hits.Load() != 0 {
				t.Fatal("request made without a usable login")
			}
			// The failure is remembered, then retried after its TTL.
			now := time.Now()
			e.provider.now = func() time.Time { return now }
			e.provider.modelsCache = nil
			_, _ = e.provider.Models(t.Context(), account)
			before := server.hits.Load()
			_, _ = e.provider.Models(t.Context(), account)
			if server.hits.Load() != before {
				t.Fatal("failure was not remembered")
			}
			now = now.Add(modelsFailureTTL + time.Second)
			_, _ = e.provider.Models(t.Context(), account)
			if name != "expired login" && name != "logged out" && server.hits.Load() != before+1 {
				t.Fatalf("hits = %d, want %d after the failure TTL", server.hits.Load(), before+1)
			}
		})
	}
}

func removeCredentials(e *testEnv, account agents.Account) error {
	return removeFile(configDir(e.accounts, account) + "/.credentials.json")
}

func TestParseModelsFiltersAndCaps(t *testing.T) {
	models, err := ParseModels(readFixture(t, "models-response.json"))
	if err != nil || len(models) != 3 {
		t.Fatalf("models = %+v, err = %v", models, err)
	}
	var big strings.Builder
	big.WriteString(`{"data":[`)
	for i := 0; i < 500; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		fmt.Fprintf(&big, `{"id":"claude-m-%d"}`, i)
	}
	big.WriteString(`]}`)
	models, err = ParseModels([]byte(big.String()))
	if err != nil || len(models) != maxModels {
		t.Fatalf("len = %d, err = %v", len(models), err)
	}
	for _, body := range []string{``, `nope`, `[]`} {
		if _, err := ParseModels([]byte(body)); err == nil {
			t.Errorf("%q parsed", body)
		}
	}
	if models, err := ParseModels([]byte(`{"surprise":1}`)); err != nil || len(models) != 0 {
		t.Fatalf("unknown shape: %v %v", models, err)
	}
}

func TestModelsNeverLeakTokenAndImplementLister(t *testing.T) {
	e := newTestEnv(t)
	var _ agents.ModelLister = e.provider
	account := e.addLogin("work")
	const secret = "sk-ant-oat01-LEAKLEAKLEAKLEAKLEAKLEAK"
	writeCredentials(t, e, account, secret, time.Now().Add(time.Hour))
	server := newUsageServer(t, 200, []byte(`{"data":[{"id":"claude-ok-1","display_name":"fine"}]}`))
	e.provider.modelsEndpoint = server.URL
	models, _ := e.provider.Models(t.Context(), account)
	for _, m := range models {
		if strings.Contains(m.ID+m.Label+m.Description, secret) {
			t.Fatal("token in model list")
		}
	}
}
