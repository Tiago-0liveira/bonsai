package events

import (
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"path/filepath"
	"testing"
)

func TestReplayOrderedScopedAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	db, _ := store.Open(path)
	h := New(db)
	for _, v := range []Event{{ID: "1", ProjectID: "a"}, {ID: "2", ProjectID: "b"}, {ID: "3", ProjectID: "a"}, {ID: "1", ProjectID: "a"}} {
		if _, e := h.Publish(v); e != nil {
			t.Fatal(e)
		}
	}
	reopened, _ := store.Open(path)
	h = New(reopened)
	items, reset, e := h.Replay(0, func(v Event) bool { return v.ProjectID == "a" })
	if e != nil || reset || len(items) != 2 || items[0].Sequence != 1 || items[1].Sequence != 3 {
		t.Fatal(items, reset, e)
	}
	_, reset, _ = h.Replay(100, func(Event) bool { return true })
	if !reset {
		t.Fatal("future cursor accepted")
	}
}
