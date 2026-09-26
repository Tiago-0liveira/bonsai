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
