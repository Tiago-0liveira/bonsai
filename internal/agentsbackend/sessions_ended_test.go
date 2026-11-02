//go:build !windows

package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

func TestAttachEndedRecordReturnsFinalStatus(t *testing.T) {
	f := newFixture(t)
	f.hold(t, "aa03", "exited")
	s := &sessions{paths: f.paths}
	stream, err := s.Attach(context.Background(), "sess_aa03", agentsapi.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	frame, ok := <-stream.Frames()
	if !ok || frame.Type != "status" || frame.Status == nil || frame.Status.State != "exited" {
		t.Fatalf("final frame = %+v, open = %v", frame, ok)
	}
	if _, ok := <-stream.Frames(); ok {
		t.Fatal("ended stream did not close")
	}
	if stream.Writer() {
		t.Fatal("ended stream is writable")
	}
	if cols, rows := stream.Size(); cols != 0 || rows != 0 {
		t.Fatalf("ended size = %dx%d", cols, rows)
	}
	for _, err := range []error{stream.Input([]byte("x")), stream.Resize(80, 24), stream.Take()} {
		if !errors.Is(err, ErrSessionEnded) {
			t.Fatalf("ended mutation = %v", err)
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRenameAndSnapshotMapErrorsAndEndedRecords(t *testing.T) {
	f := newFixture(t)
	// No lock is held, so the record is an ended session with no holder.
	rec, err := json.Marshal(agenthost.Record{Version: 1, ID: "sess_aa04", Summary: agentterminal.Summary{ID: "sess_aa04", Name: "old", State: "exited", CreatedAt: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.paths.Records, "sess_aa04.json"), rec, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &sessions{paths: f.paths}
	ctx := context.Background()
	if _, err := s.Rename(ctx, "sess_aa04", " "); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank = %v", err)
	}
	if _, err := s.Rename(ctx, "sess_aa05", "x"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing = %v", err)
	}
	name, err := s.Rename(ctx, "sess_aa04", " renamed ")
	if err != nil || name != "renamed" {
		t.Fatalf("rename = %q %v", name, err)
	}
	list, _ := s.List(ctx)
	if len(list) != 1 || list[0].Name != "renamed" {
		t.Fatalf("list = %+v", list)
	}
	if lines, err := s.Snapshot(ctx, "sess_aa04", 5); lines != nil || err != nil {
		t.Fatalf("ended snapshot = %q %v", lines, err)
	}
	if _, err := s.Snapshot(ctx, "sess_aa05", 5); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing snapshot = %v", err)
	}
}
