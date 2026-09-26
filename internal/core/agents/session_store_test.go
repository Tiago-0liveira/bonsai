package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameAccountSessionsUseDifferentHomes(t *testing.T) {
	store, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := Account{ID: "acct_same", Provider: "fake", Name: "personal"}
	homes := map[string]bool{}
	var sessions []Session
	for i := 0; i < 20; i++ {
		session, err := store.Create(account, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if homes[session.HomeDir] {
			t.Fatalf("duplicate HOME %s", session.HomeDir)
		}
		homes[session.HomeDir] = true
		sessions = append(sessions, session)
	}
	if len(homes) != 20 {
		t.Fatalf("unique HOME count = %d, want 20", len(homes))
	}
	firstSibling := sessions[1].RuntimeDir
	if err := store.Cleanup(sessions[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstSibling); err != nil {
		t.Fatalf("cleanup removed sibling session: %v", err)
	}
	if err := store.Cleanup(sessions[0]); err != nil {
		t.Fatalf("idempotent cleanup failed: %v", err)
	}
}

func TestSessionMetadataContainsNoCredentials(t *testing.T) {
	store, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := Account{ID: "acct_meta", Provider: "fake", Name: "personal"}
	session, err := store.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(session.RuntimeDir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("empty session metadata")
	}
	if _, err := os.Stat(session.HomeDir); err != nil {
		t.Fatal(err)
	}
}
