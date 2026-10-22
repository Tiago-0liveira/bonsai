package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func rec(id string) Record {
	return Record{ID: id, Kind: KindCapture, Local: "local", Repo: 9, State: StatePending, Owner: "1", ContentHash: "h", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
}

func put(t *testing.T, s *Store, r Record) {
	t.Helper()
	if _, err := s.Update(context.Background(), r.ID, func(Record, bool) (Record, error) { return r, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRoundTripAndList(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	put(t, s, rec("a"))
	other := rec("b")
	other.Local = "other"
	put(t, s, other)
	got, err := s.Get(context.Background(), "a")
	if err != nil || got.ID != "a" || got.State != StatePending {
		t.Fatalf("get = %+v, %v", got, err)
	}
	list, err := s.List(context.Background(), "local")
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err = s.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestStoreUpdateErrorWritesNothing(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	boom := errors.New("boom")
	if _, err := s.Update(context.Background(), "a", func(Record, bool) (Record, error) { return rec("a"), boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file written: %v", err)
	}
	if _, err := s.Update(context.Background(), "a", func(Record, bool) (Record, error) { r := rec("a"); r.State = "bogus"; return r, nil }); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("invalid record accepted: %v", err)
	}
}

func TestStoreRejectsCorruptFiles(t *testing.T) {
	good := `{"schema_version":1,"records":{"a":{"id":"a","kind":"capture","local_repo_id":"l","repo_id":9,"state":"pending","owner":"1","content_hash":"h","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}`
	cases := map[string]struct {
		content string
		want    error
	}{
		"valid":             {good, nil},
		"not json":          {`nope`, ErrCorrupt},
		"unknown field":     {strings.Replace(good, `"owner":"1"`, `"owner":"1","extra":true`, 1), ErrCorrupt},
		"duplicate key":     {strings.Replace(good, `"repo_id":9`, `"repo_id":9,"repo_id":10`, 1), ErrCorrupt},
		"trailing data":     {good + `{}`, ErrCorrupt},
		"newer schema":      {strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1), ErrUnsupportedSchema},
		"id mismatch":       {strings.Replace(good, `"id":"a"`, `"id":"b"`, 1), ErrCorrupt},
		"unknown state":     {strings.Replace(good, `"pending"`, `"weird"`, 1), ErrCorrupt},
		"missing records":   {`{"schema_version":1}`, ErrCorrupt},
		"too large":         {`{"schema_version":1,"records":{},"pad":"` + strings.Repeat("x", MaxStoreBytes) + `"}`, ErrTooLarge},
		"invalid utf8":      {"{\"schema_version\":1,\"records\":{},\"x\":\"\xff\"}", ErrCorrupt},
		"wrong schema zero": {strings.Replace(good, `"schema_version":1`, `"schema_version":0`, 1), ErrCorrupt},
	}
	for name, tc := range cases {
		s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
		if err := os.WriteFile(s.Path, []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := s.Get(context.Background(), "a")
		switch {
		case tc.want == nil && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != nil && !errors.Is(err, tc.want):
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestStoreTrimsFinishedRecordsOnly(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	d := disk{SchemaVersion: 1, Records: map[string]Record{}}
	for i := 0; i < MaxRecords; i++ {
		r := rec(fmt.Sprintf("done-%d", i))
		r.State = StateDone
		r.UpdatedAt = time.Unix(int64(100+i), 0).UTC()
		d.Records[r.ID] = r
	}
	if err := s.write(d); err != nil {
		t.Fatal(err)
	}
	put(t, s, rec("new"))
	if _, err := s.Get(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), "done-0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest finished record survived: %v", err)
	}
	// Unfinished records are evidence for reconcile: a store full of them refuses instead of dropping any.
	d = disk{SchemaVersion: 1, Records: map[string]Record{}}
	for i := 0; i < MaxRecords; i++ {
		r := rec(fmt.Sprintf("p-%d", i))
		d.Records[r.ID] = r
	}
	s2 := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	if err := s2.write(d); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Update(context.Background(), "extra", func(Record, bool) (Record, error) { return rec("extra"), nil }); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestStoreConcurrentUpdatesAreSerialized(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	put(t, s, rec("counter"))
	var wg sync.WaitGroup
	const n = 12
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Update(context.Background(), "counter", func(cur Record, _ bool) (Record, error) {
				cur.Number++
				return cur, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := s.Get(context.Background(), "counter")
	if got.Number != n {
		t.Fatalf("lost updates: %d", got.Number)
	}
}

func TestStoreHonoursCancelledContext(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Get(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
