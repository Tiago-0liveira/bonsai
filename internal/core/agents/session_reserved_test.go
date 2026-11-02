package agents

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestReservedSessionConcurrentReplayAndCanonicalDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root, dir := t.TempDir(), t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := Account{ID: "acct_reserved", Provider: "fake", Name: "personal"}
	store, err := NewFileSessionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	var _ ReservedSessionStore = store
	const id SessionID = "sess_012345abcdef"
	start := make(chan struct{})
	results := make(chan Session, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, err := NewFileSessionStore(root)
			if err != nil {
				errs <- err
				return
			}
			<-start
			s, err := other.CreateWithID(a, filepath.Join(dir, "child", ".."), id)
			results <- s
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first Session
	for s := range results {
		if first.ID == "" {
			first = s
		} else if !reflect.DeepEqual(first, s) {
			t.Fatalf("replay differs: %+v / %+v", first, s)
		}
	}
	if first.ID != id || first.WorkDir != dir {
		t.Fatalf("identity=%+v", first)
	}
	all, err := store.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("sessions=%+v %v", all, err)
	}
	info, err := os.Stat(filepath.Join(first.RuntimeDir, "session.json"))
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("session permissions=%v %v", info, err)
	}
	ordinary, err := store.Create(a, dir)
	if err != nil || ordinary.ID == id {
		t.Fatalf("ordinary compatibility=%+v %v", ordinary, err)
	}
}
func TestReservedSessionConflictNeverChangesOriginal(t *testing.T) {
	store, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := Account{ID: "acct_one", Provider: "fake", Name: "personal"}
	dir := t.TempDir()
	s, err := store.CreateWithID(a, dir, "sess_aabb")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.RuntimeDir, "session.json")
	before, _ := os.ReadFile(path)
	for _, tc := range []struct {
		name    string
		account Account
		dir     string
	}{{"account", Account{ID: "acct_other", Provider: "fake"}, dir}, {"provider", Account{ID: a.ID, Provider: "other"}, dir}, {"directory", a, t.TempDir()}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.CreateWithID(tc.account, tc.dir, s.ID)
			if !errors.Is(err, ErrSessionConflict) {
				t.Fatalf("err=%v", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("conflict overwrote reserved metadata")
			}
		})
	}
}
func TestReservedSessionRejectsUnsafeIDsAndRetainsPartialDirectory(t *testing.T) {
	store, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := Account{ID: "acct_safe", Provider: "fake"}
	dir := t.TempDir()
	for _, id := range []string{"", "sess_", "sess_AABB", "sess_xyz", "../sess_aa", "sess_aa/../../escape", "sess_" + strings.Repeat("a", 65)} {
		if _, err = store.CreateWithID(a, dir, SessionID(id)); !errors.Is(err, ErrInvalidLaunch) {
			t.Fatalf("id=%q err=%v", id, err)
		}
	}
	partial := filepath.Join(store.Root(), "sess_aa")
	if err = os.Mkdir(partial, 0700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(partial, "user-file")
	if err = os.WriteFile(unknown, []byte("retain me"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateWithID(a, dir, "sess_aa"); err == nil {
		t.Fatal("adopted unknown partial directory")
	}
	data, err := os.ReadFile(unknown)
	if err != nil || string(data) != "retain me" {
		t.Fatalf("erased unknown partial file: %q %v", data, err)
	}
}
