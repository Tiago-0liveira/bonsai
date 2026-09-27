package server

import (
	"bytes"
	"io"
	"sync"
	"testing"
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
