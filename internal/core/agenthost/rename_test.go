//go:build linux || darwin

package agenthost

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

func TestCleanName(t *testing.T) {
	tests := []struct {
		in, want string
		err      error
	}{
		{"  fix auth  ", "fix auth", nil},
		{"a\x1b[31mb\nc\t", "a[31mbc", nil},
		{"", "", ErrNameInvalid},
		{" \x07 ", "", ErrNameInvalid},
		{strings.Repeat("é", MaxNameRunes), strings.Repeat("é", MaxNameRunes), nil},
		{strings.Repeat("é", MaxNameRunes+1), "", ErrNameInvalid},
	}
	for _, tc := range tests {
		got, err := CleanName(tc.in)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("CleanName(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestRenameRunningHolderPublishesAndPersists(t *testing.T) {
	f := newFixture(t, `printf 'READY\n'; while read line; do :; done`)
	r, _ := f.start(t)
	monitor, _, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()

	if _, err := Rename(f.deps.Paths, r.ID, "  "); !errors.Is(err, ErrNameInvalid) {
		t.Fatalf("blank name: %v", err)
	}
	got, err := Rename(f.deps.Paths, r.ID, " new name ")
	if err != nil || got.Name != "new name" {
		t.Fatalf("rename %+v %v", got, err)
	}
	readUntil(t, monitor, func(f Frame, _ string) bool { return f.Type == "status" && f.Status.Name == "new name" })
	after, err := Read(f.deps.Paths, r.ID)
	if err != nil || after.Summary.Name != "new name" {
		t.Fatalf("record %+v %v", after.Summary, err)
	}
}

func TestRenameEndedRecordRewritesNameOnly(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rec := Record{Version: RecordVersion, ID: "sess_aa", Protocol: Version, Socket: p.socket("sess_aa"), WorkDir: "/w",
		Summary: agentterminal.Summary{ID: "sess_aa", Name: "old", State: "exited", CreatedAt: now, EndedAt: &now}}
	if err := writeRecord(p, rec); err != nil {
		t.Fatal(err)
	}
	got, err := Rename(p, "sess_aa", "fresh")
	if err != nil || got.Name != "fresh" || got.State != "exited" {
		t.Fatalf("rename %+v %v", got, err)
	}
	after, _ := Read(p, "sess_aa")
	if after.Summary.Name != "fresh" || after.WorkDir != "/w" {
		t.Fatalf("record %+v", after)
	}
	if _, err := Rename(p, "sess_bb", "x"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing record: %v", err)
	}
	// A live holder's lock taken after the liveness read must not be overwritten.
	lock, err := lockFile(p.lock("sess_aa"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := renameEndedLocked(p, "sess_aa", "late"); !errors.Is(err, ErrRenameBusy) {
		t.Fatalf("locked record: %v", err)
	}
}

func TestSnapshotRendersTailWithoutTakingControl(t *testing.T) {
	f := newFixture(t, `i=1; while [ $i -le 40 ]; do printf 'line %s\r\n' $i; i=$((i+1)); done; while read line; do :; done`)
	r, _ := f.start(t)
	web, ready, err := Dial(r, Request{Mode: ModeTerminal, Client: "web"})
	if err != nil || !*ready.Writer {
		t.Fatalf("attach %+v %v", ready, err)
	}
	defer web.Close()
	readUntil(t, web, func(_ Frame, out string) bool { return strings.Contains(out, "line 40") })

	lines, err := Snapshot(context.Background(), r, 3)
	if err != nil || len(lines) != 3 || lines[2] != "line 40" || lines[0] != "line 38" {
		t.Fatalf("snapshot %q %v", lines, err)
	}
	// The holder notices the snapshot's closed connection asynchronously, so
	// wait (bounded) for it to leave instead of asserting immediately.
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := Query(r)
		if err == nil && len(info.Clients) == 1 && info.Clients[0].Writer {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("peek disturbed clients: %+v %v", info.Clients, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSnapshotOfEndedSessionIsEmpty(t *testing.T) {
	lines, err := Snapshot(context.Background(), Record{Summary: agentterminal.Summary{State: "exited"}}, 5)
	if lines != nil || err != nil {
		t.Fatalf("%q %v", lines, err)
	}
}
