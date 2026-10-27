package drafts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

var testScope = Scope{Local: garden.LocalRepoID("clone-a"), Repo: garden.RepoID(12)}
var testTime = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func testStore(t *testing.T, path string) *Store {
	t.Helper()
	var n int
	s, err := New(path, func() time.Time { return testTime }, func() ID {
		n++
		return ID(fmt.Sprintf("local-%d", n))
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func seed(t *testing.T, s *Store) Draft {
	t.Helper()
	d, err := s.Save(context.Background(), testScope, Draft{
		Title: "Private idea", Description: "Original description",
		Spec:             spec.Spec{Summary: "Summary", Extra: []spec.Section{{Title: "Context", Text: "Details"}}},
		ConfirmedAnswers: []Answer{{Question: "Who?", Answer: "Me"}},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func putBytes(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := gitstore.WriteJSON(path, b); err != nil {
		t.Fatal(err)
	}
}

func TestRestartRecoveryAndArchive(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	if d.ID == "" || d.Revision != 1 || !d.CreatedAt.Equal(testTime) || d.Publication.State != Unpublished {
		t.Fatalf("created: %+v", d)
	}
	restarted := testStore(t, path)
	got, err := restarted.Read(ctx, testScope, d.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("restart: %+v, %v", got, err)
	}
	got.Title = "Edited"
	got.ConfirmedAnswers[0].Answer = "You"
	got.Spec.Extra[0].Text = "Changed"
	restarted.now = func() time.Time { return testTime.Add(-time.Hour) }
	updated, err := restarted.Save(ctx, testScope, got, d.Revision)
	if err != nil || updated.Revision != 2 || updated.UpdatedAt.Before(d.UpdatedAt) {
		t.Fatalf("save: %+v, %v", updated, err)
	}
	if d.ConfirmedAnswers[0].Answer != "Me" || d.Spec.Extra[0].Text != "Details" {
		t.Fatal("returned values alias prior content")
	}
	before := fileBytes(t, path)
	if _, err := restarted.Archive(ctx, testScope, d.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale archive: %v", err)
	}
	if !bytes.Equal(before, fileBytes(t, path)) {
		t.Fatal("stale archive wrote state")
	}
	archived, err := restarted.Archive(ctx, testScope, d.ID, 2)
	if err != nil || archived.Revision != 3 || archived.ArchivedAt.IsZero() {
		t.Fatalf("archive: %+v, %v", archived, err)
	}
	for _, inactive := range []bool{false, true} {
		list, err := restarted.List(ctx, testScope, inactive)
		want := 0
		if inactive {
			want = 1
		}
		if err != nil || len(list) != want {
			t.Fatalf("inactive=%v: %+v, %v", inactive, list, err)
		}
	}
	got, err = testStore(t, path).Read(ctx, testScope, d.ID)
	if err != nil || got.Title != "Edited" || got.Revision != 3 {
		t.Fatalf("archived restart: %+v, %v", got, err)
	}
}

func TestConcurrentSavesPreserveBothVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	initial := seed(t, testStore(t, path))
	start := make(chan struct{})
	type result struct {
		draft Draft
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"Writer one", "Writer two"} {
		s, err := New(path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			d := clone(initial)
			d.Title = title
			saved, err := s.Save(context.Background(), testScope, d, initial.Revision)
			results <- result{saved, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, conflicts int
	for r := range results {
		if r.err == nil {
			successes++
			continue
		}
		var conflict *ConflictError
		if !errors.As(r.err, &conflict) || !errors.Is(r.err, ErrConflict) {
			t.Fatal(r.err)
		}
		conflicts++
		if conflict.Current.ID != initial.ID || conflict.Incoming.ID == initial.ID || conflict.Incoming.ID != r.draft.ID || conflict.Incoming.ConflictOf != initial.ID {
			t.Fatalf("conflict: %+v", conflict)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
	list, err := testStore(t, path).List(context.Background(), testScope, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("restarted list: %+v, %v", list, err)
	}
	titles := map[string]bool{}
	for _, d := range list {
		titles[d.Title] = true
	}
	if !titles["Writer one"] || !titles["Writer two"] {
		t.Fatalf("lost edit: %+v", list)
	}
}

func TestBadFilesRetainKnownDraftsAndBlockWrites(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"truncated", []byte(`{"schema_version":1,"drafts":`), ErrCorrupt},
		{"null", []byte(`null`), ErrCorrupt},
		{"null drafts", []byte(`{"schema_version":1,"drafts":null}`), ErrCorrupt},
		{"missing drafts", []byte(`{"schema_version":1}`), ErrCorrupt},
		{"incomplete snapshot", []byte(`{"schema_version":1,"drafts":{}}`), ErrCorrupt},
		{"duplicate keys", []byte(`{"schema_version":1,"schema_version":1,"drafts":{}}`), ErrCorrupt},
		{"trailing JSON", []byte(`{"schema_version":1,"drafts":{}} {}`), ErrCorrupt},
		{"unknown fields", []byte(`{"schema_version":1,"drafts":{},"token":"must not be stored"}`), ErrCorrupt},
		{"invalid UTF8", []byte{0xff}, ErrCorrupt},
		{"oversized", bytes.Repeat([]byte(" "), MaxStoreBytes+1), ErrTooLarge},
		{"newer schema", []byte(`{"schema_version":2,"new_content":{"keep":"verbatim"}}`), ErrUnsupportedSchema},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "drafts.json")
			s := testStore(t, path)
			d := seed(t, s)
			good := fileBytes(t, path)
			putBytes(t, path, tc.data)
			list, err := s.List(context.Background(), testScope, false)
			if !errors.Is(err, tc.want) || len(list) != 1 || list[0].Title != d.Title {
				t.Fatalf("list: %+v, %v", list, err)
			}
			got, err := s.Read(context.Background(), testScope, d.ID)
			if !errors.Is(err, tc.want) || got.ID != d.ID {
				t.Fatalf("read: %+v, %v", got, err)
			}
			if _, err := s.Save(context.Background(), testScope, d, d.Revision); !errors.Is(err, tc.want) {
				t.Fatalf("save: %v", err)
			}
			if _, err := s.Archive(context.Background(), testScope, d.ID, d.Revision); !errors.Is(err, tc.want) {
				t.Fatalf("archive: %v", err)
			}
			if !bytes.Equal(fileBytes(t, path), tc.data) {
				t.Fatal("rewrote invalid or newer file")
			}
			putBytes(t, path, good)
			list, err = s.List(context.Background(), testScope, false)
			if err != nil || len(list) != 1 {
				t.Fatalf("recovery: %+v, %v", list, err)
			}
		})
	}
}

func TestNewerSchemaPreservedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	b := []byte("{\n  \"schema_version\": 99, \"future\": [1, 2]\n}\n")
	putBytes(t, path, b)
	s := testStore(t, path)
	if list, err := s.List(context.Background(), testScope, false); !errors.Is(err, ErrUnsupportedSchema) || len(list) != 0 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if _, err := s.Save(context.Background(), testScope, Draft{Title: "new"}, 0); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatal(err)
	}
	if !bytes.Equal(fileBytes(t, path), b) {
		t.Fatal("new schema rewritten")
	}
}

func TestMissingAndRegressingSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	old := fileBytes(t, path)
	d.Title = "new revision"
	d, err := s.Save(context.Background(), testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "old revision", "changed same revision"} {
		t.Run(mode, func(t *testing.T) {
			switch mode {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "old revision":
				putBytes(t, path, old)
			case "changed same revision":
				changed := clone(d)
				changed.Title = "silent mutation"
				b, err := json.Marshal(disk{SchemaVersion: 1, Drafts: map[ID]Draft{d.ID: changed}})
				if err != nil {
					t.Fatal(err)
				}
				putBytes(t, path, b)
			}
			got, err := s.Read(context.Background(), testScope, d.ID)
			if !errors.Is(err, ErrCorrupt) || got.Title != "new revision" {
				t.Fatalf("read: %+v, %v", got, err)
			}
			if _, err := s.Save(context.Background(), testScope, Draft{}, 0); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("save: %v", err)
			}
		})
	}
}

func TestRepositoryIsolation(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, filepath.Join(t.TempDir(), "drafts.json"))
	d := seed(t, s)
	for _, scope := range []Scope{{Local: "clone-b", Repo: testScope.Repo}, {Local: testScope.Local, Repo: 99}} {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			list, err := s.List(ctx, scope, true)
			if err != nil || len(list) != 0 {
				t.Fatalf("list: %+v, %v", list, err)
			}
			if _, err := s.Read(ctx, scope, d.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("read: %v", err)
			}
			if _, err := s.Save(ctx, scope, d, d.Revision); !errors.Is(err, ErrInvalid) {
				t.Fatalf("save with foreign scope: %v", err)
			}
			copy := clone(d)
			copy.Scope = Scope{}
			if _, err := s.Save(ctx, scope, copy, d.Revision); !errors.Is(err, ErrNotFound) {
				t.Fatalf("save: %v", err)
			}
			if _, err := s.Archive(ctx, scope, d.ID, d.Revision); !errors.Is(err, ErrNotFound) {
				t.Fatalf("archive: %v", err)
			}
			foreign, err := s.Save(ctx, scope, Draft{Title: "Other inbox"}, 0)
			if err != nil || foreign.Scope != scope {
				t.Fatalf("create: %+v, %v", foreign, err)
			}
		})
	}
	list, err := s.List(ctx, testScope, true)
	if err != nil || len(list) != 1 || list[0].ID != d.ID {
		t.Fatalf("original: %+v, %v", list, err)
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	path := filepath.Join(t.TempDir(), "private", "drafts.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	s := testStore(t, path)
	d := seed(t, s)
	for _, mode := range []string{"created", "repair existing"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "repair existing" {
				for _, p := range []string{filepath.Dir(path), path, path + ".lock"} {
					if err := os.Chmod(p, 0777); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.Save(context.Background(), testScope, d, d.Revision); err != nil {
					t.Fatal(err)
				}
			}
			for p, want := range map[string]os.FileMode{filepath.Dir(path): 0700, path: 0600, path + ".lock": 0600} {
				info, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != want {
					t.Fatalf("%s: mode %o, want %o", p, info.Mode().Perm(), want)
				}
			}
		})
	}
}

func TestFailedWritesKeepCommittedContent(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint("conflict=", conflict), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "drafts.json")
			s := testStore(t, path)
			d := seed(t, s)
			before := fileBytes(t, path)
			failure := errors.New("injected replacement failure")
			s.write = func(path string, b []byte) error {
				f, err := os.CreateTemp(filepath.Dir(path), ".interrupted-*")
				if err != nil {
					return err
				}
				defer f.Close()
				if _, err := f.Write(b[:len(b)/2]); err != nil {
					return err
				}
				return failure
			}
			d.Title = "Uncommitted"
			expected := d.Revision
			if conflict {
				expected = 0
			}
			if _, err := s.Save(context.Background(), testScope, d, expected); !errors.Is(err, failure) {
				t.Fatalf("save: %v", err)
			}
			if !bytes.Equal(before, fileBytes(t, path)) {
				t.Fatal("failed write changed committed state")
			}
			list, err := testStore(t, path).List(context.Background(), testScope, true)
			if err != nil || len(list) != 1 || list[0].Title != "Private idea" {
				t.Fatalf("restart: %+v, %v", list, err)
			}
		})
	}
}

func TestCommittedReplacementWithLostAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	lostAck := errors.New("injected error after replacement")
	s.write = func(path string, b []byte) error {
		if err := gitstore.WriteJSON(path, b); err != nil {
			return err
		}
		return lostAck
	}
	d.Title = "Committed before error"
	if _, err := s.Save(context.Background(), testScope, d, d.Revision); !errors.Is(err, lostAck) {
		t.Fatalf("save: %v", err)
	}
	for _, reader := range []*Store{s, testStore(t, path)} {
		got, err := reader.Read(context.Background(), testScope, d.ID)
		if err != nil || got.Title != d.Title || got.Revision != 2 {
			t.Fatalf("recovery: %+v, %v", got, err)
		}
	}
}

func TestDraftCountLimitPreservesAllRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	full := disk{SchemaVersion: SchemaVersion, Drafts: map[ID]Draft{}}
	for i := 0; i < MaxDrafts; i++ {
		copy := clone(d)
		copy.ID = ID(fmt.Sprintf("existing-%d", i))
		full.Drafts[copy.ID] = copy
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	putBytes(t, path, b)
	s = testStore(t, path)
	if _, err := s.Save(context.Background(), testScope, Draft{Title: "One too many"}, 0); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("save: %v", err)
	}
	if !bytes.Equal(b, fileBytes(t, path)) {
		t.Fatal("full store evicted authored content")
	}
	list, err := s.List(context.Background(), testScope, true)
	if err != nil || len(list) != MaxDrafts {
		t.Fatalf("list: %d, %v", len(list), err)
	}
}

func TestUnsafePathsAndIDCollisions(t *testing.T) {
	for _, path := range []string{"", "relative.json", string(filepath.Separator)} {
		if _, err := New(path, nil, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	before := fileBytes(t, path)
	for _, id := range []ID{"", d.ID} {
		s.newID = func() ID { return id }
		if _, err := s.Save(context.Background(), testScope, Draft{}, 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ID %q: %v", id, err)
		}
		if !bytes.Equal(before, fileBytes(t, path)) {
			t.Fatal("ID collision clobbered existing draft")
		}
	}
}

func TestSymlinkPathsRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	for _, target := range []string{"directory", "file", "lock"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "private", "drafts.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(dir, "other")
			var link string
			switch target {
			case "directory":
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(other, 0700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Dir(path)
			case "file", "lock":
				putBytes(t, other, []byte(`{"do_not_touch":true}`))
				link = path
				if target == "lock" {
					link += ".lock"
				}
			}
			if err := os.Symlink(other, link); err != nil {
				t.Fatal(err)
			}
			if _, err := testStore(t, path).Save(context.Background(), testScope, Draft{}, 0); !errors.Is(err, ErrInvalid) {
				t.Fatalf("save: %v", err)
			}
		})
	}
}

func TestOversizedSaveAndInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*Draft)
		expected uint64
		want     error
	}{
		{"oversized", func(d *Draft) { d.Description = strings.Repeat("x", MaxStoreBytes) }, 1, ErrTooLarge},
		{"oversized conflict", func(d *Draft) { d.Description = strings.Repeat("x", MaxStoreBytes) }, 0, ErrTooLarge},
		{"invalid UTF8", func(d *Draft) { d.Title = string([]byte{0xff}) }, 1, ErrInvalid},
		{"invalid UTF8 conflict", func(d *Draft) { d.Title = string([]byte{0xff}) }, 0, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "drafts.json")
			s := testStore(t, path)
			d := seed(t, s)
			before := fileBytes(t, path)
			tc.change(&d)
			if _, err := s.Save(context.Background(), testScope, d, tc.expected); !errors.Is(err, tc.want) {
				t.Fatalf("save: %v", err)
			}
			if !bytes.Equal(before, fileBytes(t, path)) {
				t.Fatal("invalid save changed file")
			}
		})
	}
}

func TestLockHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	seed(t, s)
	lock, err := procstore.TryLock(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	ctx, deadlineCancel := context.WithTimeout(ctx, time.Second)
	defer deadlineCancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); _, err := s.List(ctx, testScope, false); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lock: %v", err)
	}
}

func TestDefaultPathUsesPrivateConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if s.path != filepath.Join(configDir, "bonsai", "garden", "drafts", "drafts.json") {
		t.Fatalf("path: %s", s.path)
	}
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor wrote state: %v", err)
	}
	seed(t, s)
}

// A child exits inside a transaction to exercise OS lock release and an
// abandoned partial temporary file without depending on scheduler timing.
func TestStoreChild(t *testing.T) {
	path := os.Getenv("BONSAI_DRAFT_TEST_PATH")
	mode := os.Getenv("BONSAI_DRAFT_TEST_MODE")
	if path == "" || mode == "" {
		return
	}
	s, err := New(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Read(context.Background(), testScope, "local-1")
	if err != nil {
		t.Fatal(err)
	}
	d.Title = mode
	if mode == "crash" {
		s.write = func(path string, b []byte) error {
			f, err := os.CreateTemp(filepath.Dir(path), ".crash-*")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(b[:len(b)/2]); err != nil {
				t.Fatal(err)
			}
			if err := f.Sync(); err != nil {
				t.Fatal(err)
			}
			os.Exit(23)
			return nil
		}
	}
	_, err = s.Save(context.Background(), testScope, d, 1)
	if mode != "crash" && err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func childCommand(t *testing.T, path, mode string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStoreChild$")
	cmd.Env = append(os.Environ(), "BONSAI_DRAFT_TEST_PATH="+path, "BONSAI_DRAFT_TEST_MODE="+mode)
	return cmd
}

func TestCrashRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	seed(t, testStore(t, path))
	before := fileBytes(t, path)
	output, err := childCommand(t, path, "crash").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child: %v, %s", err, output)
	}
	if !bytes.Equal(before, fileBytes(t, path)) {
		t.Fatal("crash replaced committed document")
	}
	restarted := testStore(t, path)
	d, err := restarted.Read(context.Background(), testScope, "local-1")
	if err != nil || d.Title != "Private idea" {
		t.Fatalf("restart: %+v, %v", d, err)
	}
	d.Title = "Recovered"
	if _, err := restarted.Save(context.Background(), testScope, d, d.Revision); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestSeparateProcessesPreserveEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.json")
	seed(t, testStore(t, path))
	commands := []*exec.Cmd{childCommand(t, path, "process one"), childCommand(t, path, "process two")}
	outputs := make([]bytes.Buffer, len(commands))
	for i, cmd := range commands {
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("child: %v, %s", err, outputs[i].String())
		}
	}
	list, err := testStore(t, path).List(context.Background(), testScope, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	titles := map[string]bool{}
	for _, d := range list {
		titles[d.Title] = true
	}
	if !titles["process one"] || !titles["process two"] {
		t.Fatalf("lost edit: %+v", list)
	}
}

func TestUnarchiveRestoresWithRevisionCheck(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, filepath.Join(t.TempDir(), "drafts.json"))
	d := seed(t, s)
	if _, err := s.Unarchive(ctx, testScope, d.ID, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unarchive of an active draft: %v", err)
	}
	archived, err := s.Archive(ctx, testScope, d.ID, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := s.Unarchive(ctx, testScope, d.ID, archived.Revision-1); !errors.As(err, &ce) || ce.Current.Revision != archived.Revision {
		t.Fatalf("stale unarchive: %v", err)
	}
	back, err := s.Unarchive(ctx, testScope, d.ID, archived.Revision)
	if err != nil || !back.ArchivedAt.IsZero() || back.Revision != archived.Revision+1 || back.Title != d.Title {
		t.Fatalf("unarchive: %+v %v", back, err)
	}
	list, err := s.List(ctx, testScope, false)
	if err != nil || len(list) != 1 || list[0].ID != d.ID {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := s.Unarchive(ctx, testScope, "missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

// Unarchive is scoped like Archive (a foreign scope sees ErrNotFound), refuses a draft that is not
// archived, and on an archived published draft only clears ArchivedAt: the verified publication stays.
func TestUnarchiveScopeActiveAndPublished(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, filepath.Join(t.TempDir(), "drafts.json"))
	d := seed(t, s)
	other := Scope{Local: testScope.Local, Repo: testScope.Repo + 1}
	if _, err := s.Unarchive(ctx, other, d.ID, d.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong scope: %v", err)
	}
	if _, err := s.Unarchive(ctx, testScope, d.ID, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("not archived: %v", err)
	}
	issue := IssueIdentity{ID: 909, Number: 42, URL: "https://github.com/example/repo/issues/42"}
	d.Publication = Publication{State: Publishing, OperationID: "op"}
	d, err := s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	d.Publication = Publication{State: Published, OperationID: "op", Issue: &issue, VerifiedAt: testTime}
	if d, err = s.Save(ctx, testScope, d, d.Revision); err != nil {
		t.Fatal(err)
	}
	if d, err = s.Archive(ctx, testScope, d.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
	back, err := s.Unarchive(ctx, testScope, d.ID, d.Revision)
	if err != nil || !back.ArchivedAt.IsZero() || back.Publication.State != Published {
		t.Fatalf("published unarchive: %+v %v", back, err)
	}
}
