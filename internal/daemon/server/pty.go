package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	corepty "github.com/Tiago-0liveira/bonsai/internal/core/pty"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

const (
	ptyReplayBytes     = 256 << 10
	ptySubscriberQueue = 32
	ptyReadBuffer      = 32 << 10
	ptyDrainGrace      = 500 * time.Millisecond
)

type ptyEvent struct {
	kind     string
	seq      uint64
	data     []byte
	exitCode int
	err      string
}

type ptySubscription struct {
	hub  *ptyHub
	id   uint64
	once sync.Once
	C    <-chan ptyEvent
}

func (s *ptySubscription) Close() {
	s.once.Do(func() {
		if s.hub != nil {
			s.hub.unsubscribe(s.id)
		}
	})
}

type ptyHub struct {
	mu      sync.Mutex
	writeMu sync.Mutex

	session corepty.Session
	cols    int
	rows    int

	nextSeq uint64
	nextSub uint64
	subs    map[uint64]chan ptyEvent

	replay      []ptyEvent
	replayBytes int
	replayCap   int
	subQueue    int

	closed   bool
	exitCode int
	exitErr  string
}

func newPTYHub(session corepty.Session, cols, rows int, startSeq uint64) *ptyHub {
	if startSeq == 0 {
		startSeq = 1
	}
	return &ptyHub{
		session:   session,
		cols:      cols,
		rows:      rows,
		nextSeq:   startSeq,
		subs:      make(map[uint64]chan ptyEvent),
		replayCap: ptyReplayBytes,
		subQueue:  ptySubscriberQueue,
	}
}

func (h *ptyHub) NextSeq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.nextSeq
}

func (h *ptyHub) Subscribe(afterSeq uint64) (*ptySubscription, int, int, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextSub++
	id := h.nextSub
	replayCount := 0
	for _, ev := range h.replay {
		if ev.seq > afterSeq {
			replayCount++
		}
	}
	capacity := h.subQueue + replayCount + 1
	if capacity < 1 {
		capacity = 1
	}
	ch := make(chan ptyEvent, capacity)
	for _, ev := range h.replay {
		if ev.seq > afterSeq {
			ch <- ev
		}
	}

	if h.closed {
		ch <- ptyEvent{kind: protocol.KindPTYExit, exitCode: h.exitCode, err: h.exitErr}
		close(ch)
		return &ptySubscription{hub: h, id: id, C: ch}, h.cols, h.rows, h.nextSeq
	}
	h.subs[id] = ch
	return &ptySubscription{hub: h, id: id, C: ch}, h.cols, h.rows, h.nextSeq
}

func (h *ptyHub) unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.subs[id]
	if !ok {
		return
	}
	delete(h.subs, id)
	close(ch)
}

func (h *ptyHub) Publish(data []byte) {
	if len(data) == 0 {
		return
	}
	chunk := append([]byte(nil), data...)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	ev := ptyEvent{kind: protocol.KindPTYOutput, seq: h.nextSeq, data: chunk}
	h.nextSeq++

	if h.replayCap > 0 {
		h.replay = append(h.replay, ev)
		h.replayBytes += len(chunk)
		for h.replayBytes > h.replayCap && len(h.replay) > 0 {
			h.replayBytes -= len(h.replay[0].data)
			h.replay = h.replay[1:]
		}
	}

	for id, ch := range h.subs {
		select {
		case ch <- ev:
		default:
			delete(h.subs, id)
			close(ch)
		}
	}
}

func (h *ptyHub) WriteInput(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return net.ErrClosed
	}
	session := h.session
	h.mu.Unlock()

	for len(data) > 0 {
		n, err := session.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (h *ptyHub) Resize(cols, rows int) error {
	if err := corepty.ValidateSize(cols, rows); err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return net.ErrClosed
	}
	session := h.session
	h.mu.Unlock()

	if err := session.Resize(cols, rows); err != nil {
		return err
	}
	h.mu.Lock()
	h.cols, h.rows = cols, rows
	h.mu.Unlock()
	return nil
}

func (h *ptyHub) Close(exitCode int, exitErr string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	h.exitCode = exitCode
	h.exitErr = exitErr
	ev := ptyEvent{kind: protocol.KindPTYExit, exitCode: exitCode, err: exitErr}
	for id, ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
		delete(h.subs, id)
		close(ch)
	}
}

func (s *Server) startPTYLocked(mp *managedProc, cmd *exec.Cmd, logw *logWriter, expectedGen uint64) error {
	if mp.generation != expectedGen || mp.rec.Status != procstore.StatusStarting {
		_ = logw.Close()
		return errGenerationMismatch
	}
	cols, rows, err := corepty.NormalizeSize(mp.rec.PTYCols, mp.rec.PTYRows)
	if err != nil {
		_ = logw.Close()
		return err
	}
	mp.rec.PTYCols, mp.rec.PTYRows = cols, rows

	nextSeq := uint64(1)
	if mp.ptyHub != nil {
		nextSeq = mp.ptyHub.NextSeq()
	}

	s.appendMarker(mp.rec.ID, procstoreMarkerStart(mp.rec))
	proc, err := corepty.Start(cmd, cols, rows)
	if err != nil {
		s.appendMarker(mp.rec.ID, failedStartMarker(err))
		_ = logw.Close()
		mp.rec.Status = procstore.StatusFailed
		mp.rec.ExitError = err.Error()
		_ = s.store.WriteRecord(mp.rec)
		return err
	}

	session := proc.Session()
	hub := newPTYHub(session, cols, rows, nextSeq)
	pumpDone := make(chan struct{})
	done := make(chan struct{})

	mp.cmd = cmd
	mp.logw = logw
	mp.ptySession = session
	mp.ptyHub = hub
	mp.ptyPumpDone = pumpDone
	mp.waitDone = done
	mp.rec.PID = cmd.Process.Pid
	mp.rec.Status = procstore.StatusRunning
	mp.rec.StartedAt = time.Now()
	mp.rec.ExitError = ""
	_ = s.store.WriteRecord(mp.rec)

	started := mp.rec.StartedAt
	gen := mp.generation
	go pumpPTY(session, logw, hub, pumpDone)
	go func() {
		werr := proc.Wait(context.Background())
		select {
		case <-pumpDone:
		case <-time.After(ptyDrainGrace):
			_ = session.Close()
			<-pumpDone
		}
		_ = session.Close()
		_ = logw.Close()

		exitErr := ""
		if werr != nil {
			exitErr = werr.Error()
		}
		hub.Close(exitCodeOf(werr), exitErr)

		mp.mu.Lock()
		if mp.generation == gen && mp.ptySession == session {
			mp.ptySession = nil
			mp.ptyPumpDone = nil
		}
		mp.mu.Unlock()

		s.onExit(mp, werr, started, done, gen)
	}()
	return nil
}

func procstoreMarkerStart(rec *procstore.Record) procstore.Marker {
	return procstore.Marker{Kind: procstore.MarkerStart, Text: startText(rec)}
}

func failedStartMarker(err error) procstore.Marker {
	return procstore.Marker{Kind: procstore.MarkerExit, Code: -1, Text: "failed to start · " + err.Error()}
}

func pumpPTY(session corepty.Session, logw *logWriter, hub *ptyHub, done chan struct{}) {
	defer close(done)
	buf := make([]byte, ptyReadBuffer)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = logw.Write(chunk)
			hub.Publish(chunk)
		}
		if err != nil {
			return
		}
	}
}

type ptyControlResult struct {
	detach bool
	err    error
}

func (s *Server) streamPTY(conn net.Conn, dec *protocol.Decoder, enc *protocol.Encoder, req *protocol.Request) {
	s.mu.Lock()
	mp, ok := s.procs[req.ID]
	s.mu.Unlock()
	if !ok {
		writeResult(enc, nil, fmt.Errorf("no process #%d", req.ID))
		return
	}

	mp.mu.Lock()
	if procstore.EffectiveIOMode(mp.rec.IOMode) != procstore.IOModePTY {
		mp.mu.Unlock()
		writeResult(enc, nil, fmt.Errorf("process #%d is not a PTY process", req.ID))
		return
	}
	hub := mp.ptyHub
	mp.mu.Unlock()
	if hub == nil {
		writeResult(enc, nil, fmt.Errorf("PTY for process #%d is unavailable after daemon restart", req.ID))
		return
	}

	sub, cols, rows, nextSeq := hub.Subscribe(req.AfterSeq)
	defer sub.Close()
	if err := enc.WriteResponse(&protocol.Response{
		OK: true, Kind: protocol.KindPTYAttached, ID: req.ID,
		PTYCols: cols, PTYRows: rows, NextSeq: nextSeq,
	}); err != nil {
		return
	}

	control := make(chan ptyControlResult, 8)
	go func() {
		for {
			frame, err := dec.ReadRequest()
			if err != nil {
				control <- ptyControlResult{detach: true}
				return
			}
			if frame.ID != 0 && frame.ID != req.ID {
				control <- ptyControlResult{err: fmt.Errorf("PTY control targets process #%d on attachment #%d", frame.ID, req.ID)}
				continue
			}
			switch frame.Kind {
			case protocol.KindPTYInput:
				if err := hub.WriteInput(frame.Data); err != nil {
					control <- ptyControlResult{err: err}
				}
			case protocol.KindPTYResize:
				if err := hub.Resize(frame.PTYCols, frame.PTYRows); err != nil {
					control <- ptyControlResult{err: err}
					continue
				}
				mp.mu.Lock()
				if mp.ptyHub == hub {
					mp.rec.PTYCols, mp.rec.PTYRows = frame.PTYCols, frame.PTYRows
					_ = s.store.WriteRecord(mp.rec)
				}
				mp.mu.Unlock()
			case protocol.KindPTYDetach:
				control <- ptyControlResult{detach: true}
				return
			default:
				control <- ptyControlResult{err: fmt.Errorf("unexpected PTY control %q", frame.Kind)}
			}
		}
	}()

	for {
		select {
		case <-s.done:
			return
		case ctl := <-control:
			if ctl.detach {
				return
			}
			if ctl.err != nil {
				if err := enc.WriteResponse(&protocol.Response{
					OK: false, Kind: protocol.KindPTYError, Error: ctl.err.Error(),
				}); err != nil {
					return
				}
			}
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			switch ev.kind {
			case protocol.KindPTYOutput:
				if err := enc.WriteResponse(&protocol.Response{
					OK: true, Kind: protocol.KindPTYOutput, ID: req.ID,
					Seq: ev.seq, Data: ev.data,
				}); err != nil {
					return
				}
			case protocol.KindPTYExit:
				_ = enc.WriteResponse(&protocol.Response{
					OK: true, Kind: protocol.KindPTYExit, ID: req.ID,
					ExitCode: ev.exitCode, ExitError: ev.err, EOF: true,
				})
				return
			}
		}
	}
}

func (s *Server) closePTYResources() {
	s.mu.Lock()
	mps := make([]*managedProc, 0, len(s.procs))
	for _, mp := range s.procs {
		mps = append(mps, mp)
	}
	s.mu.Unlock()
	for _, mp := range mps {
		mp.mu.Lock()
		if mp.ptySession != nil {
			_ = mp.ptySession.Close()
			mp.ptySession = nil
		}
		if mp.ptyHub != nil {
			mp.ptyHub.Close(-1, "daemon stopped")
		}
		mp.mu.Unlock()
	}
}
