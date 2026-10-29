package sessionlink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func localID(s string) garden.LocalRepoID { return garden.LocalRepoID(s) }
func uint64ID(n int64) garden.IssueID     { return garden.IssueID(n * 100) }

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return &FileStore{Path: filepath.Join(t.TempDir(), "links.json")}
}

func link(local, session string, n int64) Link {
	return Link{SessionID: session, LocalRepoID: localID(local), IssueNumber: n, IssueID: uint64ID(n), LinkedAt: time.Unix(1700000000, 0).UTC()}
}

func TestPutIdempotentAndConflict(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	first, created, err := s.Put(ctx, link("a", "sess_1", 7), false)
	if err != nil || !created {
		t.Fatalf("first put: %v created=%v", err, created)
	}
	again := link("a", "sess_1", 7)
	again.LinkedAt = again.LinkedAt.Add(time.Hour)
	got, created, err := s.Put(ctx, again, false)
	if err != nil || created || !got.LinkedAt.Equal(first.LinkedAt) {
		t.Fatalf("same pair must return the stored link: %+v created=%v err=%v", got, created, err)
	}
	if _, _, err := s.Put(ctx, link("a", "sess_1", 8), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("other issue without replace = %v, want ErrConflict", err)
	}
	got, created, err = s.Put(ctx, link("a", "sess_1", 8), true)
	if err != nil || !created || got.IssueNumber != 8 {
		t.Fatalf("replace: %+v created=%v err=%v", got, created, err)
	}
}

func TestOtherCloneLinkIsNotShown(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	if _, _, err := s.Put(ctx, link("a", "sess_1", 7), false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put(ctx, link("b", "sess_1", 9), false); err != nil {
		t.Fatalf("a clone's link never conflicts with another clone's: %v", err)
	}
	list, err := s.List(ctx, localID("a"))
	if err != nil || len(list) != 1 || list[0].IssueNumber != 7 {
		t.Fatalf("list a = %+v err=%v", list, err)
	}
	if _, ok, _ := s.Get(ctx, localID("c"), "sess_1"); ok {
		t.Fatal("unrelated clone sees a link")
	}
}

func TestPutRejectsInvalid(t *testing.T) {
	s := newStore(t)
	for _, l := range []Link{link("a", "", 1), link("a", "sess 1", 1), link("a", "sess_1", 0), link("", "sess_1", 1)} {
		if _, _, err := s.Put(context.Background(), l, false); !errors.Is(err, ErrInvalid) {
			t.Errorf("Put(%+v) = %v, want ErrInvalid", l, err)
		}
	}
}

func TestCorruptStoreFailsClosed(t *testing.T) {
	s := newStore(t)
	for name, content := range map[string]string{
		"junk":      `{`,
		"unknown":   `{"schema_version":1,"links":{},"x":1}`,
		"newer":     `{"schema_version":2,"links":{}}`,
		"wrong key": `{"schema_version":1,"links":{"k":{"session_id":"sess_1","local_repo_id":"a","issue_number":1,"issue_id":1,"linked_at":"2024-01-01T00:00:00Z","origin":"manual"}}}`,
		"duplicate": `{"schema_version":1,"links":{},"links":{}}`,
	} {
		if err := os.WriteFile(s.Path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.List(context.Background(), "a"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	list, err := newStore(t).List(context.Background(), "a")
	if err != nil || len(list) != 0 {
		t.Fatalf("empty store = %v %v", list, err)
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newStore(t).List(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
