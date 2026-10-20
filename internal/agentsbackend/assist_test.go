package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// structuredProvider is a testProvider that can run structured calls.
type structuredProvider struct {
	*testProvider
	validateErr error
	runErr      error
	runs        []agents.StructuredRequest
}

func (p *structuredProvider) Structured() agents.StructuredSupport {
	return agents.StructuredSupport{Supported: true, Mechanism: "fake"}
}

func (p *structuredProvider) ValidateLaunch(_ agents.Account, l agents.LaunchOptions) error {
	if l.Model == "bad" {
		return errors.New("invalid model")
	}
	return p.validateErr
}

func (p *structuredProvider) RunStructured(_ context.Context, req agents.StructuredRequest) (json.RawMessage, error) {
	p.runs = append(p.runs, req)
	return json.RawMessage(`{"ok":true}`), p.runErr
}

func newAssistFixture(t *testing.T, structured bool) (*fixture, *structuredProvider) {
	t.Helper()
	f := newFixture(t)
	sp := &structuredProvider{testProvider: f.provider}
	if structured {
		registry := agents.NewRegistry()
		if err := registry.Register(sp); err != nil {
			t.Fatal(err)
		}
		f.acc.rt.Registry = registry
	}
	return f, sp
}

func TestAssistRefusesUnsupportedBeforeRunning(t *testing.T) {
	f, sp := newAssistFixture(t, false)
	a := &assist{rt: f.acc.rt}
	_, err := a.Run(t.Context(), agentsapi.AssistRequest{AccountID: f.account.ID, System: "s", Input: []byte("x")})
	if !errors.Is(err, agents.ErrStructuredUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if len(sp.runs) != 0 {
		t.Fatal("ran")
	}
}

func TestAssistRunsSupportedProvider(t *testing.T) {
	f, sp := newAssistFixture(t, true)
	a := &assist{rt: f.acc.rt}
	got, err := a.Run(t.Context(), agentsapi.AssistRequest{
		AccountID: f.account.ID, Model: "m", Effort: "e", System: "s", Input: []byte("in"),
		Schema: json.RawMessage(`{}`), MaxOutput: 99,
	})
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("got %s, %v", got, err)
	}
	r := sp.runs[0]
	if r.Account.ID != f.account.ID || r.Model != "m" || r.Effort != "e" || r.System != "s" || string(r.Input) != "in" || r.MaxOutput != 99 {
		t.Fatalf("request = %+v", r)
	}
}

func TestAssistInvalidLaunchBeforeRunning(t *testing.T) {
	f, sp := newAssistFixture(t, true)
	a := &assist{rt: f.acc.rt}
	_, err := a.Run(t.Context(), agentsapi.AssistRequest{AccountID: f.account.ID, Model: "bad"})
	if !errors.Is(err, agents.ErrInvalidLaunch) || len(sp.runs) != 0 {
		t.Fatalf("err = %v, runs = %d", err, len(sp.runs))
	}
}

func TestAssistUnknownAccountAndErrorMapping(t *testing.T) {
	f, sp := newAssistFixture(t, true)
	a := &assist{rt: f.acc.rt}
	if _, err := a.Run(t.Context(), agentsapi.AssistRequest{AccountID: "nope"}); !errors.Is(err, agents.ErrAccountNotFound) {
		t.Fatalf("err = %v", err)
	}
	sp.runErr = errors.New("/secret/path leaked detail")
	_, err := a.Run(t.Context(), agentsapi.AssistRequest{AccountID: f.account.ID})
	if !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("err = %v", err)
	}
	sp.runErr = agents.ErrStructuredOutput
	if _, err := a.Run(t.Context(), agentsapi.AssistRequest{AccountID: f.account.ID}); !errors.Is(err, agents.ErrStructuredOutput) {
		t.Fatalf("err = %v", err)
	}
}

func TestListReportsStructuredPerAccount(t *testing.T) {
	f, _ := newAssistFixture(t, false)
	list, err := f.acc.List(t.Context())
	if err != nil || list[0].Structured.Supported || list[0].Structured.Reason == "" {
		t.Fatalf("unsupported provider: %+v, %v", list, err)
	}
	f, _ = newAssistFixture(t, true)
	list, err = f.acc.List(t.Context())
	if err != nil || !list[0].Structured.Supported || list[0].Structured.Mechanism != "fake" {
		t.Fatalf("supported provider: %+v, %v", list, err)
	}
}

func TestBackendAPIWiresAssist(t *testing.T) {
	b := &Backend{assist: &assist{}}
	if b.API().Assist == nil {
		t.Fatal("Assist not wired")
	}
}
