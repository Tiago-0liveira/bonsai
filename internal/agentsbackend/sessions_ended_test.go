//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"testing"
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
