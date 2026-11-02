package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	idA = "11111111-1111-4111-8111-111111111111"
	idB = "22222222-2222-4222-8222-222222222222"
	idC = "33333333-3333-4333-8333-333333333333"
)

// writeRollout lays out a rollout file the way codex does: a local-time date
// directory and a first line of session_meta. dirTime picks the directory.
func writeRollout(t *testing.T, home, id, cwd string, meta, dirTime time.Time, firstLine string) {
	t.Helper()
	local := dirTime.Local()
	dir := filepath.Join(home, "sessions", local.Format("2006"), local.Format("01"), local.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if firstLine == "" {
		firstLine = fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"session_id":%q,"timestamp":%q,"cwd":%q,"originator":"codex-tui"}}`,
			meta.UTC().Format(time.RFC3339Nano), id, id, meta.UTC().Format(time.RFC3339Nano), cwd)
	}
	name := "rollout-" + local.Format("2006-01-02T15-04-05") + "-" + id + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(firstLine+"\n{\"type\":\"event\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRollout(t *testing.T) {
	start := time.Date(2026, 10, 4, 0, 0, 30, 0, time.Local)
	work := "/work/tree"
	for _, tc := range []struct {
		name      string
		setup     func(t *testing.T, home string)
		claimed   []string
		want      string
		ambiguous bool
	}{
		{name: "single match", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start.Add(2*time.Second), start, "")
		}, want: idA},
		{name: "other cwd ignored", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, "/other", start.Add(2*time.Second), start, "")
		}},
		{name: "older than start ignored", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start.Add(-time.Minute), start, "")
		}},
		{name: "within clock skew accepted", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start.Add(-3*time.Second), start, "")
		}, want: idA},
		{name: "claimed ignored", claimed: []string{idA}, setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start.Add(2*time.Second), start, "")
		}},
		{name: "two matches are ambiguous", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start.Add(2*time.Second), start, "")
			writeRollout(t, h, idB, work, start.Add(3*time.Second), start, "")
		}, ambiguous: true},
		{name: "non-meta first line ignored", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start, start, `{"type":"event_msg","payload":{"id":"`+idA+`","cwd":"`+work+`"}}`)
		}},
		{name: "oversized first line ignored", setup: func(t *testing.T, h string) {
			writeRollout(t, h, idA, work, start, start, `{"type":"session_meta","pad":"`+strings.Repeat("x", 70<<10)+`","payload":{"id":"`+idA+`","cwd":"`+work+`","timestamp":"`+start.UTC().Format(time.RFC3339)+`"}}`)
		}},
		{name: "non-uuid id ignored", setup: func(t *testing.T, h string) {
			writeRollout(t, h, "not-a-uuid", work, start.Add(time.Second), start, "")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)
			got, ambiguous, err := findRollout(home, work, start, tc.claimed)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || ambiguous != tc.ambiguous {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ambiguous, tc.want, tc.ambiguous)
			}
		})
	}
}

// A rollout in the next day's directory than the start (session began just
// before local midnight) must still be found even though that day is neither
// the start day nor, in tests, necessarily today.
func TestFindRolloutDateBoundary(t *testing.T) {
	home := t.TempDir()
	today := time.Now()
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local).Add(-time.Minute)
	writeRollout(t, home, idA, "/w", start.Add(30*time.Second), today, "")
	got, _, err := findRollout(home, "/w", start, nil)
	if err != nil || got != idA {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFindRolloutTwoSessionsSameWorktree(t *testing.T) {
	home := t.TempDir()
	t0 := time.Now().Add(-time.Minute)
	writeRollout(t, home, idA, "/w", t0.Add(time.Second), t0, "")
	writeRollout(t, home, idB, "/w", t0.Add(4*time.Second), t0, "")
	got, ambiguous, err := findRollout(home, "/w", t0, nil)
	if err != nil || got != "" || !ambiguous {
		t.Fatalf("before claim: (%q, %v, %v)", got, ambiguous, err)
	}
	got, ambiguous, err = findRollout(home, "/w", t0, []string{idB})
	if err != nil || got != idA || ambiguous {
		t.Fatalf("after B claimed: (%q, %v, %v)", got, ambiguous, err)
	}
}

func TestResumeArgsCodex(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	session, err := e.sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	prepared, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{
		Account: account, Session: session,
		Launch: agents.LaunchOptions{Model: "gpt-6.1-sol", Sandbox: "workspace-write", Approval: "on-request", Prompt: "ignored"},
		Resume: &agents.ResumeTarget{ID: idA},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"resume", idA, "-c", storeOverride, "--no-daemon", "-m", "gpt-6.1-sol", "-s", "workspace-write", "-a", "on-request"}
	if !slices.Equal(prepared.Args, want) {
		t.Fatalf("args = %q, want %q", prepared.Args, want)
	}
}

func TestResumeTargetCodex(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	home := e.profileHome(account)
	start := time.Now().Add(-time.Minute)
	session := agents.Session{WorkDir: "/w"}
	req := func(id string) agents.ResumeRequest {
		return agents.ResumeRequest{Account: account, Session: session, ProviderSessionID: id, StartedAt: start}
	}

	if _, err := e.provider.ResumeTarget(t.Context(), req("")); !errors.Is(err, agents.ErrNotResumable) || !strings.Contains(err.Error(), agents.ReasonNoConversation) {
		t.Fatalf("missing sessions dir: %v", err)
	}
	writeRollout(t, home, idA, "/w", start.Add(time.Second), start, "")

	got, err := e.provider.ResumeTarget(t.Context(), req(idA))
	if err != nil || got.ID != idA {
		t.Fatalf("id set + file: %v, %v", got, err)
	}
	if _, err := e.provider.ResumeTarget(t.Context(), req(idB)); !errors.Is(err, agents.ErrNotResumable) || !strings.Contains(err.Error(), agents.ReasonNotFound) {
		t.Fatalf("id set, no file: %v", err)
	}
	got, err = e.provider.ResumeTarget(t.Context(), req(""))
	if err != nil || got.ID != idA {
		t.Fatalf("empty + one match: %v, %v", got, err)
	}
	writeRollout(t, home, idC, "/w", start.Add(2*time.Second), start, "")
	if _, err := e.provider.ResumeTarget(t.Context(), req("")); !errors.Is(err, agents.ErrNotResumable) {
		t.Fatalf("ambiguous: %v", err)
	}

	id, err := e.provider.DiscoverSessionID(t.Context(), agents.ResumeRequest{Account: account, Session: session, StartedAt: start, Claimed: []string{idA}})
	if err != nil || id != idC {
		t.Fatalf("discover: %q, %v", id, err)
	}
}

func TestHistoricalNextDayRollout(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2020, 1, 2, 23, 59, 0, 0, time.Local)
	next := start.Add(2 * time.Minute)
	writeRollout(t, home, idA, "/w", next, next, "")
	id, _, err := findRollout(home, "/w", start, nil)
	if err != nil || id != idA {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
func TestDelayedRolloutContender(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	start := time.Now().Add(-time.Minute)
	writeRollout(t, e.profileHome(account), idB, "/w", start, start, "")
	req := agents.ResumeRequest{Account: account, Session: agents.Session{WorkDir: "/w"}, StartedAt: start, Contenders: 1}
	if id, err := e.provider.DiscoverSessionID(t.Context(), req); err != nil || id != "" {
		t.Fatalf("%q %v", id, err)
	}
	if _, err := e.provider.ResumeTarget(t.Context(), req); !errors.Is(err, agents.ErrNotResumable) {
		t.Fatal(err)
	}
	req.ProviderSessionID = idB
	if got, err := e.provider.ResumeTarget(t.Context(), req); err != nil || got.ID != idB {
		t.Fatalf("%+v %v", got, err)
	}
}
