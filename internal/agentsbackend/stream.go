package agentsbackend

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var _ agentsapi.Stream = (*stream)(nil)

type streamConn interface {
	Recv() (agenthost.Frame, error)
	Send(agenthost.Request) error
	Close() error
}

type stream struct {
	backend  *Backend
	record   agenthost.Record
	conn     streamConn
	readOnly bool
	// generation is the holder's, from the ready frame.
	generation string

	frames chan agentterminal.Frame
	writer atomic.Bool
	size   atomic.Pointer[dimensions]

	done      chan struct{} // closed by Close
	ended     chan struct{} // closed when the reader exits
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type dimensions struct{ cols, rows int }

func newStream(b *Backend, r agenthost.Record, conn streamConn, ready agenthost.Frame, readOnly bool, info agenthost.Info) *stream {
	s := &stream{
		backend: b, record: r, conn: conn, readOnly: readOnly, generation: ready.Generation,
		frames: make(chan agentterminal.Frame, 256),
		done:   make(chan struct{}), ended: make(chan struct{}),
	}
	s.writer.Store(!readOnly && ready.Writer != nil && *ready.Writer)
	s.setSize(info)
	return s
}

func (s *stream) setSize(i agenthost.Info) {
	s.size.Store(&dimensions{cols: i.Cols, rows: i.Rows})
}

func (s *stream) start() {
	s.wg.Add(3)
	go s.read()
	go s.poll()
}

func (s *stream) read() {
	defer s.wg.Done()
	defer s.conn.Close()
	defer close(s.frames)
	defer close(s.ended)
	for {
		f, err := s.conn.Recv()
		if err != nil {
			return
		}
		switch f.Type {
		case "error", "info":
			continue
		case "writer":
			s.writer.Store(!s.readOnly && f.Writer != nil && *f.Writer)
		}
		// Blocking send: socket backpressure beats dropping output.
		select {
		case s.frames <- f.TerminalFrame():
		case <-s.done:
			return
		}
	}
}

func (s *stream) poll() {
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer s.wg.Done()
		select {
		case <-s.done:
		case <-s.ended:
		}
		cancel()
	}()
	t := time.NewTicker(sizePollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if info, err := agenthost.QueryContext(ctx, s.record); err == nil {
				s.setSize(info)
			}
		}
	}
}

func (s *stream) Frames() <-chan agentterminal.Frame { return s.frames }
func (s *stream) Writer() bool                       { return s.writer.Load() }
func (s *stream) Generation() string                 { return s.generation }
func (s *stream) Size() (int, int) {
	d := s.size.Load()
	return d.cols, d.rows
}

func (s *stream) Input(p []byte) error {
	if !s.writer.Load() || len(p) > agentterminal.InputLimit {
		return errInputUnavailable
	}
	if err := s.conn.Send(agenthost.Request{Type: "input", Data: p}); err != nil {
		return fmt.Errorf("send terminal input: %w", ErrSessionUnavailable)
	}
	return nil
}

func (s *stream) Resize(cols, rows int) error {
	if !s.writer.Load() || !agentterminal.Dimensions(cols, rows) {
		return errResizeUnavailable
	}
	if err := s.conn.Send(agenthost.Request{Type: "resize", Cols: cols, Rows: rows}); err != nil {
		return fmt.Errorf("send terminal resize: %w", ErrSessionUnavailable)
	}
	return nil
}

func (s *stream) Take() error {
	if s.readOnly {
		return errReadOnly
	}
	if err := s.conn.Send(agenthost.Request{Type: "take"}); err != nil {
		return fmt.Errorf("take terminal control: %w", ErrSessionUnavailable)
	}
	return nil
}

func (s *stream) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.Close()
		s.wg.Wait()
		s.backend.deregister(s)
	})
	// A concurrent Close also returns only once the goroutines are gone.
	s.wg.Wait()
	return nil
}
