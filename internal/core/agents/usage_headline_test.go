package agents

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func headlineLimit(id string, remaining float64, reset *time.Time) *UsageLimit {
	return &UsageLimit{ID: id, RemainingFraction: &remaining, ResetsAt: reset}
}

func TestNewUsageHeadline(t *testing.T) {
	five := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	week := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		primary, second *UsageLimit
		score           float64
		scored, low     bool
		reset           *time.Time
	}{
		{"lower of the pair", headlineLimit("5h", 0.9, &five), headlineLimit("week", 0.4, &week), 0.4, true, false, &week},
		{"just under threshold is low", headlineLimit("5h", 0.199, &five), headlineLimit("week", 0.5, nil), 0.199, true, true, &five},
		{"threshold itself is not low", headlineLimit("5h", 0.20, nil), nil, 0.20, true, false, nil},
		{"clamped above one and below zero", headlineLimit("5h", 1.5, nil), headlineLimit("week", -0.2, &week), 0, true, true, &week},
		{"no fractions", &UsageLimit{ID: "5h", ResetsAt: &five}, nil, 0, false, false, &five},
		{"nothing", nil, nil, 0, false, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewUsageHeadline(tt.primary, tt.second, nil)
			if (h.Score != nil) != tt.scored || (h.Score != nil && *h.Score != tt.score) || h.Low != tt.low {
				t.Fatalf("score=%v low=%v", h.Score, h.Low)
			}
			if !reflect.DeepEqual(h.NextReset, tt.reset) {
				t.Fatalf("next reset = %v, want %v", h.NextReset, tt.reset)
			}
		})
	}
}

func TestNewUsageHeadlineCopiesLimits(t *testing.T) {
	limits := []UsageLimit{*headlineLimit("5h", 0.5, nil), *headlineLimit("model", 0.1, nil)}
	h := NewUsageHeadline(&limits[0], nil, limits[1:])
	limits[0].ID, limits[1].ID = "changed", "changed"
	if h.Primary.ID != "5h" || h.Extra[0].ID != "model" {
		t.Fatalf("headline aliases the snapshot: %+v", h)
	}
	// Extra limits never lower the score.
	if h.Score == nil || *h.Score != 0.5 || h.Low {
		t.Fatalf("score = %v low = %v", h.Score, h.Low)
	}
}

type layoutTestProvider struct{ usageTestProvider }

func (p *layoutTestProvider) ID() ProviderID { return "layout-fake" }
func (p *layoutTestProvider) UsageHeadline(s UsageSnapshot) UsageHeadline {
	return NewUsageHeadline(&s.Limits[0], nil, nil)
}

func TestUsageServiceFillsHeadlines(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plain, layout := &usageTestProvider{}, &layoutTestProvider{}
	registry := NewRegistry()
	_ = registry.Register(plain)
	_ = registry.Register(layout)
	_ = store.Create(testAccount("acct_plain", plain.ID(), "plain"))
	_ = store.Create(testAccount("acct_layout", layout.ID(), "layout"))
	service := &UsageService{Accounts: store, Registry: registry}
	var refreshed []AccountID
	results := service.AllFunc(context.Background(), func(a Account) UsageOptions {
		if a.ID == "acct_layout" {
			refreshed = append(refreshed, a.ID)
			return UsageOptions{Refresh: true}
		}
		return UsageOptions{}
	})
	if len(results) != 2 || !reflect.DeepEqual(refreshed, []AccountID{"acct_layout"}) {
		t.Fatalf("results = %+v refreshed = %v", results, refreshed)
	}
	for _, r := range results {
		switch r.Account.ID {
		case "acct_layout":
			if r.Headline == nil || r.Headline.Primary.ID != "window" || *r.Headline.Score != 0.5 {
				t.Fatalf("layout headline = %+v", r.Headline)
			}
		case "acct_plain":
			if r.Headline != nil {
				t.Fatalf("provider without UsageLayout has a headline: %+v", r.Headline)
			}
		}
	}
	if service.Headline(UsageSnapshot{Provider: "missing"}) != nil {
		t.Fatal("unregistered provider has a headline")
	}
}

func TestLaunchOptionsValues(t *testing.T) {
	yes, no := true, false
	if got := (LaunchOptions{Model: "m", Prompt: "secret prompt", DisplayName: "n"}).Values(); got != nil {
		t.Fatalf("values = %v", got)
	}
	got := LaunchOptions{Model: "m", Prompt: "p", Effort: "high", PermissionMode: "plan", Sandbox: "workspace-write", Approval: "never", FullAccess: &yes}.Values()
	want := map[string]string{"effort": "high", "permission_mode": "plan", "sandbox": "workspace-write", "approval": "never", "full_access": "true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v", got)
	}
	if got := (LaunchOptions{FullAccess: &no}).Values(); !reflect.DeepEqual(got, map[string]string{"full_access": "false"}) {
		t.Fatalf("values = %v", got)
	}
}
