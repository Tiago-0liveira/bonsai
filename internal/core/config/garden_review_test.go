package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGardenReviewPrefsValidate(t *testing.T) {
	cases := []struct {
		name string
		p    GardenReviewPrefs
		ok   bool
	}{
		{"zero is valid and off", GardenReviewPrefs{}, true},
		{"enabled with account", GardenReviewPrefs{Enabled: true, AccountID: "acc-1", Trigger: "awaiting-review"}, true},
		{"enabled without account", GardenReviewPrefs{Enabled: true}, false},
		{"unknown trigger", GardenReviewPrefs{Trigger: "always"}, false},
		{"unknown scope", GardenReviewPrefs{Scope: "all-prs"}, false},
		{"max over cap", GardenReviewPrefs{MaxAutoRuns: 11}, false},
		{"max negative", GardenReviewPrefs{MaxAutoRuns: -1}, false},
		{"max at cap", GardenReviewPrefs{MaxAutoRuns: 10}, true},
		{"account control byte", GardenReviewPrefs{AccountID: "a\x1bb"}, false},
		{"account padded", GardenReviewPrefs{AccountID: " a"}, false},
		{"account too long", GardenReviewPrefs{AccountID: strings.Repeat("a", 129)}, false},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrInvalidGardenReview)) {
			t.Errorf("%s: err=%v ok=%v", c.name, err, c.ok)
		}
	}
}

func TestGardenReviewDefaultOffAndEffective(t *testing.T) {
	var p *GardenReviewPrefs
	e := p.Effective()
	if e.Enabled || e.Trigger != GardenReviewTriggerManual || e.Scope != GardenReviewScopeAttempts || e.MaxAutoRuns != 3 {
		t.Fatalf("default = %+v", e)
	}
	if e := (&GardenReviewPrefs{Enabled: true, AccountID: "a", MaxAutoRuns: 7}).Effective(); e.MaxAutoRuns != 7 || !e.Enabled {
		t.Fatalf("effective = %+v", e)
	}
}

func TestPrefsCloneCopiesGardenReview(t *testing.T) {
	p := Prefs{GardenReview: &GardenReviewPrefs{Enabled: true, AccountID: "a"}}
	c := p.Clone()
	c.GardenReview.AccountID = "b"
	if p.GardenReview.AccountID != "a" {
		t.Fatal("clone shares the review policy")
	}
}

func TestGardenReviewLoadedOnlyFromLocalState(t *testing.T) {
	p := stateEnv(t)
	write := func(body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"prefs":{"garden_review":{"enabled":true,"account_id":"acc","trigger":"new-commit"}}}`)
	st, err := LoadState()
	if err != nil || st.Prefs.GardenReview == nil || !st.Prefs.GardenReview.Enabled {
		t.Fatalf("load = %+v, %v", st.Prefs.GardenReview, err)
	}
	// Invalid hand edit fails closed.
	write(`{"prefs":{"garden_review":{"enabled":true,"trigger":"bogus"}}}`)
	st, err = LoadState()
	if err != nil || st.Prefs.GardenReview != nil {
		t.Fatalf("invalid policy kept: %+v, %v", st.Prefs.GardenReview, err)
	}
	// Persisting an invalid policy is refused and changes nothing.
	if err := st.PersistPrefs(1, Prefs{GardenReview: &GardenReviewPrefs{Enabled: true}}); !errors.Is(err, ErrInvalidGardenReview) {
		t.Fatalf("persist invalid = %v", err)
	}
	if err := st.SetPrefs(Prefs{GardenReview: &GardenReviewPrefs{Enabled: true}}); !errors.Is(err, ErrInvalidGardenReview) {
		t.Fatalf("set invalid = %v", err)
	}
	good := Prefs{GardenReview: &GardenReviewPrefs{Enabled: true, AccountID: "acc"}}
	if err := st.PersistPrefs(2, good); err != nil {
		t.Fatal(err)
	}
	st, _ = LoadState()
	if st.Prefs.GardenReview == nil || st.Prefs.GardenReview.AccountID != "acc" {
		t.Fatal("round trip lost the policy")
	}
}

func TestGardenReviewPathUnderConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	p, err := GardenReviewPath()
	if err != nil || filepath.Base(p) != "garden-review.json" || !strings.HasPrefix(p, dir) {
		t.Fatalf("path = %q, %v", p, err)
	}
}
