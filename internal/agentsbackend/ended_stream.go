package agentsbackend

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var _ agentsapi.Stream = (*endedStream)(nil)

type endedStream struct {
	frames <-chan agentterminal.Frame
}

func newEndedStream(record agenthost.Record) *endedStream {
	frames := make(chan agentterminal.Frame, 1)
	frames <- agentterminal.Frame{Type: "status", Status: &record.Summary}
	close(frames)
	return &endedStream{frames: frames}
}

func (s *endedStream) Frames() <-chan agentterminal.Frame { return s.frames }
func (s *endedStream) Writer() bool                       { return false }
func (s *endedStream) Generation() string                 { return "" }

// Retained records have no PTY dimensions; zero reports that size is unknown.
func (s *endedStream) Size() (int, int)      { return 0, 0 }
func (s *endedStream) Input([]byte) error    { return ErrSessionEnded }
func (s *endedStream) Resize(int, int) error { return ErrSessionEnded }
func (s *endedStream) Take() error           { return ErrSessionEnded }
func (s *endedStream) Close() error          { return nil }
