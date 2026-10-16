package agentterminal

import (
	"errors"
	"fmt"
	"slices"
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
	// Writer is set on writer frames, sent to a subscriber whose control
	// changed: whether it now writes.
	Writer *bool `json:"writer,omitempty"`
}

// Control is how an attachment takes part in writing.
type Control int

const (
	// ControlAuto writes when no other subscriber does.
	ControlAuto Control = iota
	// ControlTake writes at once; the previous writer observes.
	ControlTake
	// ControlNever only observes, and cannot take control later.
	ControlNever
)

// StreamClient is one subscriber as Clients reports it.
type StreamClient struct {
	Client string `json:"client"`
	Writer bool   `json:"writer"`
}

// Stream is the bounded output buffer of one terminal with its subscribers.
// The first attachment writes; the others observe until the writer detaches
// or one of them takes control. Input and resize reach the process bound by RunInStream; between processes
// input is refused and the last size is kept for the next one.
type Stream[S any] struct {
	mu          sync.Mutex
	output      []byte
	head        int
	offset      uint64
	subscribers map[uint64]chan StreamFrame[S]
	clients     map[uint64]string
	observers   map[uint64]bool
	writer      uint64
	next        uint64
	proc        *process
	cols, rows  int
	lastOutput  time.Time
}

// NewStream returns an empty stream whose first process starts at cols×rows.
func NewStream[S any](cols, rows int) *Stream[S] {
	return &Stream[S]{subscribers: map[uint64]chan StreamFrame[S]{}, clients: map[uint64]string{}, observers: map[uint64]bool{}, cols: cols, rows: rows}
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
	s.lastOutput = time.Now().UTC()
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

// LastOutput is when output last arrived; zero before any.
func (s *Stream[S]) LastOutput() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastOutput
}

// broadcast drops subscribers that cannot keep up. Callers hold s.mu.
func (s *Stream[S]) broadcast(f StreamFrame[S]) {
	for id := range s.subscribers {
		s.send(id, f)
	}
}

// send delivers f to one subscriber, dropping it when it cannot keep up.
// Callers hold s.mu.
func (s *Stream[S]) send(id uint64, f StreamFrame[S]) {
	ch, ok := s.subscribers[id]
	if !ok {
		return
	}
	select {
	case ch <- f:
	default:
		s.remove(id)
	}
}

// remove closes a subscriber and releases writing if it held it. Callers
// hold s.mu.
func (s *Stream[S]) remove(id uint64) {
	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
		delete(s.clients, id)
		delete(s.observers, id)
	}
	if s.writer == id {
		s.writer = 0
	}
}

// Attach registers a subscriber and returns the replay from cursor (a gap
// first when cursor is outside the retained range) followed by status.
// Replay and subscription are atomic: no output is missed or repeated.
func (s *Stream[S]) Attach(cursor uint64, status S) (uint64, bool, []StreamFrame[S], <-chan StreamFrame[S]) {
	return s.AttachAs(cursor, status, "", ControlAuto)
}

// AttachAs is Attach for a named client (such as web or cli, reported by
// Clients) with its control: ControlTake writes even when another subscriber
// does (the previous writer gets a writer frame), ControlNever never writes.
func (s *Stream[S]) AttachAs(cursor uint64, status S, client string, control Control) (uint64, bool, []StreamFrame[S], <-chan StreamFrame[S]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	subscriber := s.next
	ch := make(chan StreamFrame[S], 64)
	s.subscribers[subscriber] = ch
	s.clients[subscriber] = client
	if control == ControlNever {
		s.observers[subscriber] = true
	}
	if control == ControlTake && s.writer != 0 {
		s.sendWriter(s.writer, false)
		s.writer = 0
	}
	if s.writer == 0 && control != ControlNever {
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
	s.remove(subscriber)
}

// Take makes subscriber the writer. The previous writer and subscriber each
// get a writer frame; taking control already held changes nothing. A
// ControlNever subscriber cannot take control.
func (s *Stream[S]) Take(subscriber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subscribers[subscriber]; !ok || s.observers[subscriber] {
		return fmt.Errorf("terminal subscriber cannot take control")
	}
	if s.writer == subscriber {
		return nil
	}
	if s.writer != 0 {
		s.sendWriter(s.writer, false)
	}
	s.writer = subscriber
	s.sendWriter(subscriber, true)
	return nil
}

// Clients lists the subscribers in attachment order.
func (s *Stream[S]) Clients() []StreamClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint64, 0, len(s.subscribers))
	for id := range s.subscribers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]StreamClient, len(ids))
	for i, id := range ids {
		out[i] = StreamClient{Client: s.clients[id], Writer: s.writer == id}
	}
	return out
}

// sendWriter tells one subscriber whether it writes. Callers hold s.mu.
func (s *Stream[S]) sendWriter(id uint64, writer bool) {
	s.send(id, StreamFrame[S]{Type: "writer", Offset: s.offset, Writer: &writer})
}

// DetachAll closes every subscriber; their connections end.
func (s *Stream[S]) DetachAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detachAll()
}

func (s *Stream[S]) detachAll() {
	for id := range s.subscribers {
		s.remove(id)
	}
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
