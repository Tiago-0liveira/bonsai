package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestInvocationArgsExplicitModelWins(t *testing.T) {
	account := agents.Account{Settings: json.RawMessage(`{"model":"account-model","dangerously_skip_permissions":true}`)}
	settings, err := ParseSettings(account)
	if err != nil {
		t.Fatal(err)
	}
	got := invocationArgs(settings, []string{"--model", "explicit-model", "-p", "review this repo"})
	models := 0
	for _, arg := range got {
		if arg == "--model" {
			models++
		}
	}
	if models != 1 {
		t.Fatalf("model flag count = %d; args=%v", models, got)
	}
	if !hasFlag(got, "--dangerously-skip-permissions") {
		t.Fatalf("permission default missing: %v", got)
	}
	if got[3] != "review this repo" {
		t.Fatalf("argv boundary changed: %v", got)
	}
}
