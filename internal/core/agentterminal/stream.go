package agentterminal

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNoProcess means input arrived while no process is bound to the stream.
var ErrNoProcess = errors.New("terminal has no running process")

// StreamFrame is one message of a terminal stream: output bytes at an absolute
// offset, a replay gap, or a status of type S (an agent summary, a setup job).
type StreamFrame[S any] struct {
	Type   string `json:"type"`
	Offset uint64 `json:"offset"`
	Data   []byte `json:"data,omitempty"`
	Status *S     `json:"status,omitempty"`
}

// Stream is the bounded output buffer of one terminal with its subscribers.
// The first attachment writes; the others observe until the writer detaches.
// Input and resize reach the process bound by RunInStream; between processes
// input is refused and the last size is kept for the next one.
type Stream[S any] struct {
	mu          sync.Mutex
	output      []byte
	head        int
	offset      uint64
	subscribers map[uint64]chan StreamFrame[S]
	writer      uint64
	next        uint64
	proc        *process
	cols, rows  int
}

// NewStream returns an empty stream whose first process starts at cols×rows.
func NewStream[S any](cols, rows int) *Stream[S] {
	return &Stream[S]{subscribers: map[uint64]chan StreamFrame[S]{}, cols: cols, rows: rows}
}

// Write appends terminal output and sends it to every subscriber.
func (s *Stream[S]) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data := append([]byte(nil), b...)
	offset := s.offset
	s.offset += uint64(len(b))
	if len(b) >= OutputLimit {
		s.output = append(s.output[:0], b[len(b)-OutputLimit:]...)
		s.head = 0
	} else {
		if len(s.output) < OutputLimit {
			n := min(OutputLimit-len(s.output), len(b))
			s.output = append(s.output, b[:n]...)
			b = b[n:]
		}
		if len(b) > 0 {
			n := copy(s.output[s.head:], b)
			copy(s.output, b[n:])
			s.head = (s.head + len(b)) % OutputLimit
		}
	}
	s.broadcast(StreamFrame[S]{Type: "output", Offset: offset, Data: data})
	return len(data), nil
}

// Status sends status to every subscriber at the current offset.
func (s *Stream[S]) Status(status S) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcast(StreamFrame[S]{Type: "status", Offset: s.offset, Status: &status})
}

// Offset is the absolute offset of the next output byte.
func (s *Stream[S]) Offset() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}

// broadcast drops subscribers that cannot keep up. Callers hold s.mu.
func (s *Stream[S]) broadcast(f StreamFrame[S]) {
	for id, ch := range s.subscribers {
		select {
		case ch <- f:
		default:
			close(ch)
			delete(s.subscribers, id)
			if s.writer == id {
				s.writer = 0
			}
		}
	}
}

// Attach registers a subscriber and returns the replay from cursor (a gap
// first when cursor is outside the retained range) followed by status.
// Replay and subscription are atomic: no output is missed or repeated.
func (s *Stream[S]) Attach(cursor uint64, status S) (uint64, bool, []StreamFrame[S], <-chan StreamFrame[S]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	subscriber := s.next
	ch := make(chan StreamFrame[S], 64)
	s.subscribers[subscriber] = ch
	if s.writer == 0 {
		s.writer = subscriber
	}
	start := s.offset - uint64(len(s.output))
	frames := []StreamFrame[S]{}
	if cursor < start || cursor > s.offset {
		frames = append(frames, StreamFrame[S]{Type: "gap", Offset: start})
		cursor = start
	}
	if cursor < s.offset {
		frames = append(frames, StreamFrame[S]{Type: "output", Offset: cursor, Data: s.replay(cursor - start)})
	}
	frames = append(frames, StreamFrame[S]{Type: "status", Offset: s.offset, Status: &status})
	return subscriber, s.writer == subscriber, frames, ch
}

// Detach removes a subscriber and releases writing if it held it.
func (s *Stream[S]) Detach(subscriber uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subscribers[subscriber]; ok {
		close(ch)
		delete(s.subscribers, subscriber)
	}
	if s.writer == subscriber {
		s.writer = 0
	}
}

// DetachAll closes every subscriber; their connections end.
func (s *Stream[S]) DetachAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detachAll()
}

func (s *Stream[S]) detachAll() {
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.writer = 0
}

// Release detaches everyone and frees the buffer. Offsets keep counting, so a
// later attachment sees a gap instead of old bytes.
func (s *Stream[S]) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detachAll()
	s.output, s.head = nil, 0
}

// Input writes data to the bound process when subscriber is the writer. With
// no process bound it returns ErrNoProcess.
func (s *Stream[S]) Input(subscriber uint64, data []byte) error {
	s.mu.Lock()
	p := s.proc
	if s.writer != subscriber || len(data) > InputLimit {
		s.mu.Unlock()
		return fmt.Errorf("terminal input unavailable")
	}
	s.mu.Unlock()
	if p == nil {
		return ErrNoProcess
	}
	_ = p.file.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := p.file.Write(data)
	return err
}

// Resize changes the bound process's terminal size when subscriber is the
// writer. Without a process the size is kept for the next one.
func (s *Stream[S]) Resize(subscriber uint64, cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != subscriber || !Dimensions(cols, rows) {
		return fmt.Errorf("terminal resize unavailable")
	}
	s.cols, s.rows = cols, rows
	if s.proc == nil {
		return nil
	}
	return s.proc.resize(cols, rows)
}

func (s *Stream[S]) bind(p *process) {
	s.mu.Lock()
	s.proc = p
	s.mu.Unlock()
}

// Size is the terminal size the next process starts with: the last resize,
// or the size the stream was created with.
func (s *Stream[S]) Size() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

func (s *Stream[S]) replay(skip uint64) []byte {
	out := make([]byte, 0, len(s.output))
	out = append(out, s.output[s.head:]...)
	out = append(out, s.output[:s.head]...)
	return out[skip:]
}
