package usagestore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func usageAt(age time.Duration, plan string) agentsapi.Usage {
	return agentsapi.Usage{Snapshot: agents.UsageSnapshot{Plan: plan}, FetchedAt: t0.Add(-age)}
}

func TestApply(t *testing.T) {
	good := Entry{Usage: usageAt(time.Minute, "good"), Have: true, Refreshing: true, Seq: 7}
	tests := []struct {
		name        string
		before      Entry
		result      Result
		wantApplied bool
		check       func(t *testing.T, e Entry)
	}{
		{
			name:        "late seq is dropped",
			before:      good,
			result:      Result{Account: "a", Seq: 6, Usage: usageAt(0, "late")},
			wantApplied: false,
			check: func(t *testing.T, e Entry) {
				if e.Usage.Snapshot.Plan != "good" || !e.Refreshing {
					t.Errorf("late result changed the entry: %+v", e)
				}
			},
		},
		{
			name:        "late failure is dropped",
			before:      good,
			result:      Result{Account: "a", Seq: 6, Err: errors.New("boom")},
			wantApplied: false,
			check: func(t *testing.T, e Entry) {
				if e.Stale() {
					t.Errorf("late failure marked the entry stale: %+v", e)
				}
			},
		},
		{
			name:        "error keeps last good data and marks it stale",
			before:      good,
			result:      Result{Account: "a", Seq: 7, Err: errors.New("failed at /home/u/.claude/token")},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if !e.Have || !e.Stale() || e.Usage.Snapshot.Plan != "good" || e.Refreshing {
					t.Errorf("entry = %+v, want last good data marked stale", e)
				}
				if strings.Contains(e.Message(), "/home") || e.Message() == "" {
					t.Errorf("message %q must be sanitized and set", e.Message())
				}
			},
		},
		{
			name:        "error without data records the failure",
			before:      Entry{Seq: 7, Refreshing: true},
			result:      Result{Account: "a", Seq: 7, Err: context.DeadlineExceeded},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if e.Have || e.Err != "timed out" || e.Message() != "timed out" || e.Refreshing {
					t.Errorf("entry = %+v", e)
				}
			},
		},
		{
			name:        "success replaces and clears the failure",
			before:      Entry{Usage: usageAt(time.Minute, "good"), Have: true, Seq: 7, Refreshing: true},
			result:      Result{Account: "a", Seq: 7, Usage: usageAt(0, "new")},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if e.Usage.Snapshot.Plan != "new" || e.Stale() || e.Refreshing {
					t.Errorf("entry = %+v", e)
				}
			},
		},
		{
			name:        "older FetchedAt never overwrites newer data",
			before:      Entry{Usage: usageAt(0, "newer"), Have: true, Seq: 7, Refreshing: true},
			result:      Result{Account: "a", Seq: 7, Usage: usageAt(time.Hour, "older")},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if e.Usage.Snapshot.Plan != "newer" || e.Refreshing {
					t.Errorf("entry = %+v, want newer data kept and the fetch settled", e)
				}
			},
		},
		{
			name:        "observation (seq 0) is ordered by FetchedAt, not seq",
			before:      Entry{Usage: usageAt(time.Hour, "old"), Have: true, Seq: 7, Refreshing: true},
			result:      Result{Account: "a", Usage: usageAt(0, "observed")},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if e.Usage.Snapshot.Plan != "observed" || !e.Refreshing {
					t.Errorf("entry = %+v, want observed data and the in-flight fetch left alone", e)
				}
			},
		},
		{
			name:        "stored read fills an empty entry",
			before:      Entry{Seq: 7, Refreshing: true},
			result:      Result{Account: "a", Stored: true, Found: true, Usage: usageAt(time.Hour, "disk")},
			wantApplied: true,
			check: func(t *testing.T, e Entry) {
				if !e.Have || e.Usage.Snapshot.Plan != "disk" || !e.Refreshing || e.Seq != 7 {
					t.Errorf("entry = %+v, want data painted without touching the fetch", e)
				}
			},
		},
		{
			name:        "stored read never overwrites data",
			before:      good,
			result:      Result{Account: "a", Stored: true, Found: true, Usage: usageAt(time.Hour, "disk")},
			wantApplied: false,
			check: func(t *testing.T, e Entry) {
				if e.Usage.Snapshot.Plan != "good" {
					t.Errorf("stored read overwrote data: %+v", e)
				}
			},
		},
		{
			name:        "stored read with nothing stored changes nothing",
			before:      Entry{},
			result:      Result{Account: "a", Stored: true},
			wantApplied: false,
			check: func(t *testing.T, e Entry) {
				if e.Have {
					t.Errorf("entry = %+v", e)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := Store{"a": tc.before}
			got, applied := before.Apply(tc.result)
			if applied != tc.wantApplied {
				t.Fatalf("applied = %v, want %v", applied, tc.wantApplied)
			}
			tc.check(t, got["a"])
			if !reflect.DeepEqual(before["a"], tc.before) {
				t.Error("Apply mutated the store it was called on")
			}
		})
	}
}

func TestBeginIssuesUniqueSeqs(t *testing.T) {
	s, one := Store{}.Begin("a")
	s2, two := s.Begin("a")
	if one == two || one == 0 || s2["a"].Seq != two || !s2["a"].Refreshing {
		t.Fatalf("seqs = %d, %d, entry %+v", one, two, s2["a"])
	}
	if s["a"].Seq != one {
		t.Error("Begin mutated the receiver")
	}
	// The earlier request is now late.
	if _, applied := s2.Apply(Result{Account: "a", Seq: one, Usage: usageAt(0, "x")}); applied {
		t.Error("superseded request was applied")
	}
}

func TestSeededKeepsDataOnlyAndRetainPrunes(t *testing.T) {
	s := Store{
		"a": {Usage: usageAt(0, "a"), Have: true, Seq: 3, Refreshing: true},
		"b": {Seq: 4, Refreshing: true},
	}
	seeded := s.Seeded()
	if len(seeded) != 1 || seeded["a"].Seq != 0 || seeded["a"].Refreshing || !seeded["a"].Have {
		t.Errorf("seeded = %+v", seeded)
	}
	if kept := s.Retain([]agents.AccountID{"b", "z"}); len(kept) != 1 || kept["b"].Seq != 4 {
		t.Errorf("retained = %+v", kept)
	}
}

func TestFetchCallsUsageOnceAndStoredNeverFetches(t *testing.T) {
	api := &fake.Accounts{
		Usages: map[agents.AccountID]agentsapi.Usage{"a": usageAt(0, "live")},
		Stored: map[agents.AccountID]agentsapi.Usage{"a": usageAt(time.Hour, "disk")},
		Now:    func() time.Time { return t0 },
	}
	// The stored snapshot is older than the shared window: the fetch reaches
	// the provider, once.
	res := Fetch(context.Background(), api, NewLimiter(2), "a", 5, false)().(Result)
	if res.Account != "a" || res.Seq != 5 || res.Err != nil || res.Usage.Snapshot.Plan != "live" {
		t.Fatalf("result = %+v", res)
	}
	if got := api.Fetches(); len(got) != 1 {
		t.Fatalf("fetches = %v", got)
	}
	// Now the snapshot is fresh: the same call is answered from it.
	Fetch(context.Background(), api, nil, "a", 6, false)()
	if got := api.Fetches(); len(got) != 1 {
		t.Fatalf("fresh snapshot fetched again: %v", got)
	}
	// A manual refresh bypasses it.
	Fetch(context.Background(), api, nil, "a", 7, true)()
	if got := api.Fetches(); len(got) != 2 {
		t.Fatalf("refresh fetches = %v", got)
	}

	calls := len(api.UsageCalls())
	st := Stored(context.Background(), api, "a")().(Result)
	if !st.Stored || !st.Found || st.Usage.Snapshot.Plan != "live" || len(api.UsageCalls()) != calls {
		t.Fatalf("stored = %+v", st)
	}
}

func TestFetchGivesUpWhileWaitingForASlot(t *testing.T) {
	lim := NewLimiter(1)
	lim <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Fetch(ctx, &fake.Accounts{}, lim, "a", 3, false)().(Result)
	if !errors.Is(res.Err, context.Canceled) || res.Seq != 3 {
		t.Fatalf("result = %+v", res)
	}
}

func TestObserveDropsTheSeq(t *testing.T) {
	msg := Observe(Result{Account: "a", Seq: 9, Usage: usageAt(0, "x")})()
	o, ok := msg.(Observation)
	if !ok || o.Seq != 0 || o.Account != "a" {
		t.Fatalf("observation = %#v", msg)
	}
}

func TestSanitizeRedactsPathsAndControls(t *testing.T) {
	got := Sanitize(errors.New("\x1b[31mfailed at /home/u/.claude/x\x1b[0m\n  now"))
	if strings.Contains(got, "/home") || strings.Contains(got, "\x1b") || strings.Contains(got, "\n") {
		t.Fatalf("Sanitize = %q", got)
	}
	if long := Sanitize(errors.New(strings.Repeat("word ", 60))); ansi.StringWidth(long) > maxErrLen {
		t.Fatalf("long error not capped: %d cells", ansi.StringWidth(long))
	}
	if Sanitize(nil) != "" {
		t.Fatal("nil error must sanitize to empty")
	}
	if Sanitize(context.DeadlineExceeded) != "timed out" {
		t.Fatal("deadline must read as timed out")
	}
}
