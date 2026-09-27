package server

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

type fakePTYSession struct {
	mu   sync.Mutex
	data bytes.Buffer
	cols int
	rows int
}

func (f *fakePTYSession) Read([]byte) (int, error) { return 0, io.EOF }
func (f *fakePTYSession) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data.Write(p)
}
func (f *fakePTYSession) Resize(cols, rows int) error {
	f.mu.Lock()
	f.cols, f.rows = cols, rows
	f.mu.Unlock()
	return nil
}
func (f *fakePTYSession) Size() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows, nil
}
func (f *fakePTYSession) Close() error { return nil }

func TestPTYHubReplayBroadcastBackpressureAndControl(t *testing.T) {
	session := &fakePTYSession{cols: 80, rows: 24}
	h := newPTYHub(session, 80, 24, 1)
	h.replayCap = 5

	h.Publish([]byte("abc")) // evicted when next chunk pushes replay above 5 bytes
	h.Publish([]byte("def"))

	replay, _, _, next := h.Subscribe(0)
	defer replay.Close()
	if next != 3 {
		t.Fatalf("next seq = %d, want 3", next)
	}
	ev := <-replay.C
	if ev.seq != 2 || string(ev.data) != "def" {
		t.Fatalf("replay event = seq %d data %q", ev.seq, ev.data)
	}

	h.subQueue = 1
	slow, _, _, _ := h.Subscribe(next - 1)
	defer slow.Close()
	h.subQueue = 16
	fast, _, _, _ := h.Subscribe(next - 1)
	defer fast.Close()

	for _, text := range []string{"1", "2", "3", "4"} {
		h.Publish([]byte(text))
	}

	h.mu.Lock()
	_, slowStillAttached := h.subs[slow.id]
	_, fastStillAttached := h.subs[fast.id]
	h.mu.Unlock()
	if slowStillAttached {
		t.Fatal("slow subscriber should have been dropped")
	}
	if !fastStillAttached {
		t.Fatal("healthy subscriber should remain attached")
	}

	for i, want := range []string{"1", "2", "3", "4"} {
		if got := <-fast.C; string(got.data) != want {
			t.Fatalf("fast event %d = %q, want %q", i, got.data, want)
		}
	}

	if err := h.WriteInput([]byte("input")); err != nil {
		t.Fatal(err)
	}
	if got := session.data.String(); got != "input" {
		t.Fatalf("input = %q", got)
	}
	if err := h.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	cols, rows, _ := session.Size()
	if cols != 120 || rows != 40 {
		t.Fatalf("resize = %dx%d", cols, rows)
	}
	if err := h.Resize(0, 40); err == nil {
		t.Fatal("invalid resize should fail")
	}
}

func TestPTYHubPreservesRawBytes(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	raw := []byte{0x00, 0xff, 0x1b, '[', '3', '1', 'm', '\r', '\n'}
	h.Publish(raw)
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()
	ev := <-sub.C
	if !bytes.Equal(ev.data, raw) {
		t.Fatalf("raw PTY bytes changed: got %v want %v", ev.data, raw)
	}
}

func TestPTYHubCloseReplaysExit(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.Publish([]byte("final"))
	h.Close(7, "failed")
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()
	out := <-sub.C
	exit := <-sub.C
	if string(out.data) != "final" || exit.kind == "" || exit.exitCode != 7 {
		t.Fatalf("closed replay = output %#v exit %#v", out, exit)
	}
	if _, ok := <-sub.C; ok {
		t.Fatal("closed hub subscription should close")
	}
}

func TestPTYHubExitDeliveredWithFullOutputBacklog(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 2
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()

	h.Publish([]byte("one"))
	h.Publish([]byte("two"))
	h.Close(0, "")

	for _, want := range []string{"one", "two"} {
		ev, ok := <-sub.C
		if !ok || ev.kind != "ptyOutput" || string(ev.data) != want {
			t.Fatalf("output = %#v, %v; want %q", ev, ok, want)
		}
	}
	exit, ok := <-sub.C
	if !ok || exit.kind != "ptyExit" {
		t.Fatalf("exit = %#v, %v; want ptyExit", exit, ok)
	}
	if _, ok := <-sub.C; ok {
		t.Fatal("subscription should close after exit")
	}
}

func TestPTYHubExitDeliveredAfterReplayAndFullLiveBacklog(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 2
	h.Publish([]byte("replay"))

	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()
	h.Publish([]byte("live-one"))
	h.Publish([]byte("live-two"))
	h.Close(0, "")

	for _, want := range []string{"replay", "live-one", "live-two"} {
		ev, ok := <-sub.C
		if !ok || ev.kind != "ptyOutput" || string(ev.data) != want {
			t.Fatalf("event = %#v, %v; want output %q", ev, ok, want)
		}
	}
	exit, ok := <-sub.C
	if !ok || exit.kind != "ptyExit" {
		t.Fatalf("exit = %#v, %v; want ptyExit", exit, ok)
	}
}

func TestPTYHubExitFollowsQueuedOutputAtCapacity(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 10)
	h.subQueue = 3
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()

	h.Publish([]byte("a"))
	h.Publish([]byte("b"))
	h.Publish([]byte("c"))
	h.Close(0, "")

	var got []string
	for ev := range sub.C {
		if ev.kind == "ptyOutput" {
			got = append(got, string(ev.data))
			continue
		}
		if ev.kind != "ptyExit" {
			t.Fatalf("unexpected terminal event: %#v", ev)
		}
		got = append(got, "exit")
	}
	want := []string{"a", "b", "c", "exit"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestPTYHubExitDeliveredExactlyOnceAtCapacity(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 1
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()

	h.Publish([]byte("queued"))
	h.Close(0, "")
	h.Close(99, "second close must be ignored")

	exitCount := 0
	outputCount := 0
	for ev := range sub.C {
		switch ev.kind {
		case "ptyOutput":
			outputCount++
		case "ptyExit":
			exitCount++
		}
	}
	if outputCount != 1 || exitCount != 1 {
		t.Fatalf("output=%d exit=%d, want output=1 exit=1", outputCount, exitCount)
	}
}

func TestPTYHubExitMetadataDeliveredAtCapacity(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 2
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()

	h.Publish([]byte("one"))
	h.Publish([]byte("two"))
	h.Close(23, "boom")

	<-sub.C
	<-sub.C
	exit, ok := <-sub.C
	if !ok {
		t.Fatal("subscription closed before exit")
	}
	if exit.kind != "ptyExit" || exit.exitCode != 23 || exit.err != "boom" {
		t.Fatalf("exit = %#v, want code 23 and error boom", exit)
	}
}

func newPTYSizePersistenceFixture(t *testing.T) (*Server, *managedProc, *ptyHub) {
	t.Helper()
	store := procstore.New(t.TempDir())
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	hub := newPTYHub(&fakePTYSession{cols: 80, rows: 24}, 80, 24, 1)
	rec := &procstore.Record{
		ID:      1,
		IOMode:  procstore.IOModePTY,
		PTYCols: 80,
		PTYRows: 24,
	}
	if err := store.WriteRecord(rec); err != nil {
		t.Fatal(err)
	}
	mp := &managedProc{rec: rec, ptyHub: hub}
	return &Server{store: store}, mp, hub
}

func TestPTYHubSizeTracksLatestResize(t *testing.T) {
	_, _, hub := newPTYSizePersistenceFixture(t)
	if err := hub.Resize(101, 41); err != nil {
		t.Fatal(err)
	}
	if err := hub.Resize(132, 52); err != nil {
		t.Fatal(err)
	}
	cols, rows := hub.Size()
	if cols != 132 || rows != 52 {
		t.Fatalf("hub size = %dx%d, want 132x52", cols, rows)
	}
}

func TestPersistPTYSizeUsesLatestHubSizeAfterStaleCaller(t *testing.T) {
	s, mp, hub := newPTYSizePersistenceFixture(t)
	if err := hub.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	if err := hub.Resize(120, 50); err != nil {
		t.Fatal(err)
	}

	// This persistence call represents the older 100x40 request resuming only
	// after the newer 120x50 resize has already completed.
	s.persistPTYSize(mp, hub)

	mp.mu.Lock()
	cols, rows := mp.rec.PTYCols, mp.rec.PTYRows
	mp.mu.Unlock()
	if cols != 120 || rows != 50 {
		t.Fatalf("persisted size = %dx%d, want latest 120x50", cols, rows)
	}
}

func TestPersistPTYSizeSkipsReplacedHub(t *testing.T) {
	s, mp, oldHub := newPTYSizePersistenceFixture(t)
	if err := oldHub.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	newHub := newPTYHub(&fakePTYSession{cols: 140, rows: 60}, 140, 60, oldHub.NextSeq())
	mp.mu.Lock()
	mp.ptyHub = newHub
	mp.rec.PTYCols, mp.rec.PTYRows = 140, 60
	mp.mu.Unlock()

	s.persistPTYSize(mp, oldHub)

	mp.mu.Lock()
	cols, rows := mp.rec.PTYCols, mp.rec.PTYRows
	mp.mu.Unlock()
	if cols != 140 || rows != 60 {
		t.Fatalf("replaced hub regressed record to %dx%d", cols, rows)
	}
}

func TestPersistPTYSizeWritesLatestHubSizeToStore(t *testing.T) {
	s, mp, hub := newPTYSizePersistenceFixture(t)
	if err := hub.Resize(150, 55); err != nil {
		t.Fatal(err)
	}
	s.persistPTYSize(mp, hub)

	rec, err := s.store.ReadRecord(mp.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.PTYCols != 150 || rec.PTYRows != 55 {
		t.Fatalf("stored size = %dx%d, want 150x55", rec.PTYCols, rec.PTYRows)
	}
}

func TestPersistPTYSizeConcurrentStaleCallerCannotRegressRecord(t *testing.T) {
	s, mp, hub := newPTYSizePersistenceFixture(t)
	firstResized := make(chan struct{})
	newerPersisted := make(chan struct{})
	firstDone := make(chan struct{})

	go func() {
		defer close(firstDone)
		if err := hub.Resize(90, 30); err != nil {
			t.Errorf("first resize: %v", err)
			return
		}
		close(firstResized)
		<-newerPersisted
		// Persist after the newer request to reproduce the original stale-write
		// ordering deterministically.
		s.persistPTYSize(mp, hub)
	}()

	<-firstResized
	if err := hub.Resize(160, 70); err != nil {
		t.Fatal(err)
	}
	s.persistPTYSize(mp, hub)
	close(newerPersisted)
	<-firstDone

	mp.mu.Lock()
	cols, rows := mp.rec.PTYCols, mp.rec.PTYRows
	mp.mu.Unlock()
	if cols != 160 || rows != 70 {
		t.Fatalf("stale caller regressed record to %dx%d, want 160x70", cols, rows)
	}
	rec, err := s.store.ReadRecord(mp.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.PTYCols != 160 || rec.PTYRows != 70 {
		t.Fatalf("stale caller regressed stored size to %dx%d, want 160x70", rec.PTYCols, rec.PTYRows)
	}
}

type signalingWriteConn struct {
	net.Conn
	mu                 sync.Mutex
	writes             int
	secondWriteStarted chan struct{}
	secondOnce         sync.Once
}

func (c *signalingWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	writeNo := c.writes
	c.mu.Unlock()
	if writeNo == 2 {
		c.secondOnce.Do(func() { close(c.secondWriteStarted) })
	}
	return c.Conn.Write(p)
}

func startBlockedPTYStream(t *testing.T, s *Server, id int) (*signalingWriteConn, net.Conn, <-chan struct{}) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	conn := &signalingWriteConn{
		Conn:               serverConn,
		secondWriteStarted: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.streamPTY(
			conn,
			protocol.NewDecoder(conn),
			protocol.NewEncoder(conn),
			&protocol.Request{Kind: protocol.KindPTYAttach, ID: id},
		)
	}()

	resp, err := protocol.NewDecoder(clientConn).ReadResponse()
	if err != nil {
		clientConn.Close()
		t.Fatalf("read attach handshake: %v", err)
	}
	if resp.Kind != protocol.KindPTYAttached {
		clientConn.Close()
		t.Fatalf("attach response kind = %q, want %q", resp.Kind, protocol.KindPTYAttached)
	}
	return conn, clientConn, done
}

func TestPTYHubSlowSubscriberSignalsEviction(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 1
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()

	h.Publish([]byte("queued"))
	h.Publish([]byte("overflow"))

	select {
	case <-sub.evicted:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber eviction was not signaled")
	}
	if _, ok := <-sub.C; !ok {
		t.Fatal("queued output should remain readable before channel closure is observed")
	}
	if _, ok := <-sub.C; ok {
		t.Fatal("evicted subscriber channel should close after queued output")
	}
}

func TestPTYHubManualUnsubscribeDoesNotSignalEviction(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	sub, _, _, _ := h.Subscribe(0)
	sub.Close()

	select {
	case <-sub.evicted:
		t.Fatal("manual unsubscribe must not be reported as slow-subscriber eviction")
	default:
	}
	if _, ok := <-sub.C; ok {
		t.Fatal("manual unsubscribe should close the event channel")
	}
}

func TestPTYHubNormalCloseDoesNotSignalEviction(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	sub, _, _, _ := h.Subscribe(0)
	defer sub.Close()
	h.Close(0, "")

	select {
	case <-sub.evicted:
		t.Fatal("normal PTY exit must not be reported as slow-subscriber eviction")
	default:
	}
	ev, ok := <-sub.C
	if !ok || ev.kind != protocol.KindPTYExit {
		t.Fatalf("normal close event = %#v, %v; want ptyExit", ev, ok)
	}
}

func TestPTYStreamEvictionUnblocksBlockedSocketWrite(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 2
	mp := &managedProc{
		rec:    &procstore.Record{ID: 1, IOMode: procstore.IOModePTY, Status: procstore.StatusRunning},
		ptyHub: h,
	}
	s := &Server{procs: map[int]*managedProc{1: mp}, done: make(chan struct{})}
	conn, peer, handlerDone := startBlockedPTYStream(t, s, 1)
	defer peer.Close()

	h.Publish([]byte("blocks-on-peer"))
	select {
	case <-conn.secondWriteStarted:
	case <-time.After(time.Second):
		t.Fatal("PTY output write did not block on non-reading peer")
	}

	// The blocked stream has consumed the first event. Fill its bounded queue,
	// then publish one more event to evict it.
	h.Publish([]byte("queued-1"))
	h.Publish([]byte("queued-2"))
	h.Publish([]byte("evict"))

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("evicted PTY stream did not exit after its blocked socket was closed")
	}
}

func TestPTYStreamEvictionUnblocksAllBlockedSlowSockets(t *testing.T) {
	h := newPTYHub(&fakePTYSession{}, 80, 24, 1)
	h.subQueue = 2
	mp := &managedProc{
		rec:    &procstore.Record{ID: 1, IOMode: procstore.IOModePTY, Status: procstore.StatusRunning},
		ptyHub: h,
	}
	s := &Server{procs: map[int]*managedProc{1: mp}, done: make(chan struct{})}

	connA, peerA, doneA := startBlockedPTYStream(t, s, 1)
	defer peerA.Close()
	connB, peerB, doneB := startBlockedPTYStream(t, s, 1)
	defer peerB.Close()

	h.subQueue = 16
	healthy, _, _, _ := h.Subscribe(0)
	defer healthy.Close()
	healthyDone := make(chan struct{})
	go func() {
		defer close(healthyDone)
		for range healthy.C {
		}
	}()

	h.Publish([]byte("blocks-both"))
	for name, blocked := range map[string]<-chan struct{}{
		"A": connA.secondWriteStarted,
		"B": connB.secondWriteStarted,
	} {
		select {
		case <-blocked:
		case <-time.After(time.Second):
			t.Fatalf("PTY output write %s did not block", name)
		}
	}

	h.Publish([]byte("queued-1"))
	h.Publish([]byte("queued-2"))
	h.Publish([]byte("evict"))

	for name, done := range map[string]<-chan struct{}{"A": doneA, "B": doneB} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("evicted PTY stream %s did not exit", name)
		}
	}
	h.mu.Lock()
	_, healthyStillAttached := h.subs[healthy.id]
	h.mu.Unlock()
	if !healthyStillAttached {
		t.Fatal("healthy subscriber was dropped while slow socket streams were evicted")
	}
	h.Close(0, "")
	select {
	case <-healthyDone:
	case <-time.After(time.Second):
		t.Fatal("healthy subscriber did not finish on normal PTY close")
	}
}
