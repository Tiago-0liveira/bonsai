package fake

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

func TestChangesCoalesceAndCloseWithContext(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := b.Sessions.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.Emit()
	b.Emit()
	b.Emit()
	select {
	case <-ch:
	default:
		t.Fatal("Emit did not signal")
	}
	select {
	case <-ch:
		t.Fatal("three Emits should coalesce into one pending signal")
	default:
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel, got a signal")
		}
	case <-time.After(time.Second):
		t.Fatal("Changes did not close with ctx")
	}
	b.Emit() // must not panic on the removed subscriber
}

func TestStartStopFireChanges(t *testing.T) {
	b := New()
	ch, _ := b.Sessions.Changes(t.Context())
	s, err := b.Sessions.Start(t.Context(), agentsapi.StartRequest{WorkDir: "/w", Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	if err := b.Sessions.Stop(t.Context(), s.ID); err != nil {
		t.Fatal(err)
	}
	<-ch
	list, _ := b.Sessions.List(t.Context())
	if len(list) != 1 || list[0].State != "exited" || list[0].WorkDir != "/w" {
		t.Fatalf("list = %+v", list)
	}
}

func TestStreamCloseClosesFrames(t *testing.T) {
	b := New()
	b.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: "a"}})
	st, err := b.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fs := b.Sessions.Streams("a")[0]
	if !fs.Push(agentterminal.Frame{Type: "output", Data: []byte("hi")}) {
		t.Fatal("push failed")
	}
	if err := st.Input([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if f := <-st.Frames(); string(f.Data) != "hi" {
		t.Fatalf("queued frame lost: %+v", f)
	}
	if _, ok := <-st.Frames(); ok {
		t.Fatal("Frames should be closed")
	}
	if st.Input([]byte("y")) == nil || fs.Push(agentterminal.Frame{}) {
		t.Fatal("closed stream accepted input or frames")
	}
	if in := fs.Inputs(); len(in) != 1 || string(in[0]) != "x" {
		t.Fatalf("inputs = %q", in)
	}
}

func TestReadOnlyStreamNeverWrites(t *testing.T) {
	b := New()
	b.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: "a"}})
	st, _ := b.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{ReadOnly: true})
	if st.Writer() || st.Input([]byte("x")) == nil || st.Resize(1, 1) == nil || st.Take() == nil {
		t.Fatal("read-only stream must not write, resize or take")
	}
}

func TestAttachControl(t *testing.T) {
	b := New()
	b.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: "a"}})
	first, _ := b.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{})
	second, _ := b.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{})
	if !first.Writer() || second.Writer() {
		t.Fatal("plain attach should write only when nobody else does")
	}
	third, _ := b.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{Take: true})
	if first.Writer() || !third.Writer() {
		t.Fatal("Take should demote the current writer")
	}
	if err := second.Take(); err != nil || second.(*Stream).Takes() != 1 {
		t.Fatalf("Take on a writable stream: err %v", err)
	}
}

func TestEmitRacesCancel(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := b.Sessions.Changes(ctx); err != nil {
			t.Fatal(err)
		}
		wg.Add(2)
		go func() { defer wg.Done(); b.Emit() }()
		go func() { defer wg.Done(); cancel() }()
	}
	wg.Wait()
}

func TestUpdateDefaults(t *testing.T) {
	a := &Accounts{}
	model, effort := "opus", "high"
	if err := a.UpdateDefaults(t.Context(), "acct", map[string]*string{"model": &model, "effort": &effort}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateDefaults(t.Context(), "acct", map[string]*string{"effort": nil}); err != nil {
		t.Fatal(err)
	}
	d, err := a.Defaults(t.Context(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Values) != 1 || d.Values["model"] != "opus" {
		t.Fatalf("values = %v, want only model", d.Values)
	}
	d.Values["model"] = "mutated"
	if again, _ := a.Defaults(t.Context(), "acct"); again.Values["model"] != "opus" {
		t.Fatal("Defaults must return a copy")
	}
	if got := a.Updates(); len(got) != 2 || got[1].Values["effort"] != nil {
		t.Fatalf("updates = %+v", got)
	}
}

func TestUsageServesStoredInsideTheSharedWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a := &Accounts{
		Usages: map[agents.AccountID]agentsapi.Usage{"a": {FetchedAt: now}},
		Now:    func() time.Time { return now },
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := a.Usage(ctx, "a", false); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.Fetches(); len(got) != 1 {
		t.Fatalf("fetches = %v, want the first call only", got)
	}
	now = now.Add(agents.UsageMaxAge + time.Second)
	a.Usage(ctx, "a", false)
	a.Usage(ctx, "a", true)
	if got := a.Fetches(); len(got) != 3 {
		t.Fatalf("fetches = %v, want a refetch past the window and on refresh", got)
	}
}
