//go:build !windows

package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

type backendResumeProvider struct{ *fakeProvider }

func (*backendResumeProvider) ResumeTarget(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
	return agents.ResumeTarget{ID: req.ProviderSessionID}, nil
}

func writeResumeRecord(t *testing.T, paths agenthost.Paths, r agenthost.Record) {
	t.Helper()
	if err := os.MkdirAll(paths.Records, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.Records, r.ID+".json")
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}

func resumeBackendFixture(t *testing.T, launch agenthost.Launcher) (agentsapi.Sessions, agenthost.Paths, agenthost.Record) {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_abc", Provider: providerID, Name: "fixture"}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	runtime, err := store.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(&backendResumeProvider{&fakeProvider{}}); err != nil {
		t.Fatal(err)
	}
	paths := agenthost.Paths{Records: filepath.Join(t.TempDir(), "records"), Sockets: t.TempDir()}
	r := agenthost.Record{Version: agenthost.RecordVersion, ID: string(runtime.ID), WorkDir: runtime.WorkDir, Generation: "old", Summary: agenthost.Summary{ID: string(runtime.ID), AccountID: account.ID, Provider: providerID, ProjectID: "project", WorktreeID: "tree", Name: "kept", State: "failed", Origin: "web", ProviderSessionID: "conversation", CreatedAt: time.Now().UTC()}}
	writeResumeRecord(t, paths, r)
	b, err := New(WithPaths(paths), WithLauncher(launch), WithRuntime(&agentruntime.Runtime{Accounts: accounts, Sessions: store, Registry: registry}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b.API().Sessions, paths, r
}

func TestSessionsResumeWrapsReason(t *testing.T) {
	s, paths, r := resumeBackendFixture(t, func(context.Context, agenthost.Paths, agenthost.Spec) error {
		t.Fatal("launched refused session")
		return nil
	})
	r.Summary.OperationID = "garden"
	writeResumeRecord(t, paths, r)
	_, err := s.Resume(context.Background(), r.ID, 80, 24)
	var refusal *agents.NotResumableError
	if !errors.Is(err, agents.ErrNotResumable) || !errors.As(err, &refusal) || refusal.Reason != agents.ReasonGarden {
		t.Fatalf("resume refusal = %v, want typed garden refusal", err)
	}
}

func TestSessionsResumeDelegates(t *testing.T) {
	var specs []agenthost.Spec
	s, _, r := resumeBackendFixture(t, func(_ context.Context, paths agenthost.Paths, spec agenthost.Spec) error {
		specs = append(specs, spec)
		// An immediately exited holder still returns its final summary.
		summary := spec.Summary
		summary.State = "exited"
		writeResumeRecord(t, paths, agenthost.Record{Version: agenthost.RecordVersion, ID: spec.Summary.ID, WorkDir: spec.WorkDir, Generation: "new", Summary: summary})
		return nil
	})
	got, err := s.Resume(context.Background(), r.ID, 101, 33)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("launch calls = %d", len(specs))
	}
	spec := specs[0]
	if spec.Summary.ID != r.ID || spec.Summary.Origin != "tui" || spec.WorkDir != r.WorkDir || spec.Cols != 101 || spec.Rows != 33 || spec.Resume == nil || spec.Resume.ID != "conversation" {
		t.Fatalf("resume spec = %+v", spec)
	}
	want := agentsapi.Session{Summary: spec.Summary, WorkDir: filepath.Clean(r.WorkDir)}
	want.State = "exited"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session = %+v, want %+v", got, want)
	}
}

func TestSessionsResumeMissingAndCancelled(t *testing.T) {
	s, _, _ := resumeBackendFixture(t, nil)
	for _, id := range []string{"sess_abcdef", ""} {
		if _, err := s.Resume(context.Background(), id, 80, 24); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("missing %q: %v", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Resume(ctx, "sess_abcdef", 80, 24); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
