package agentactivity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const waitFor = 5 * time.Second

func newSpool(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "activity.jsonl")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func appendRaw(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func collect() (func(Signal), chan Signal) {
	ch := make(chan Signal, 1024)
	return func(s Signal) { ch <- s }, ch
}

func expect(t *testing.T, ch chan Signal, kinds ...Kind) []Signal {
	t.Helper()
	var got []Signal
	for range kinds {
		select {
		case s := <-ch:
			got = append(got, s)
		case <-time.After(waitFor):
			t.Fatalf("timeout after %d of %d signals: %+v", len(got), len(kinds), got)
		}
	}
	for i, k := range kinds {
		if got[i].Kind != k {
			t.Fatalf("signal %d = %s, want %s (%+v)", i, got[i].Kind, k, got)
		}
	}
	return got
}

func expectNone(t *testing.T, ch chan Signal) {
	t.Helper()
	select {
	case s := <-ch:
		t.Fatalf("unexpected signal %+v", s)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestAppendSignalsNeverCreates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.jsonl")
	err := AppendSignals(p, []Signal{{Kind: ToolStarted, Source: SourceHook}})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
	if _, serr := os.Stat(p); !errors.Is(serr, os.ErrNotExist) {
		t.Fatal("AppendSignals created the file")
	}
}

func TestAppendSignalsRefusesSymlinkAndNonRegular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "activity.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := AppendSignals(link, []Signal{{Kind: ToolStarted, Source: SourceHook}}); err == nil {
		t.Fatal("AppendSignals followed a symlink")
	}
	if data, _ := os.ReadFile(target); string(data) != "keep\n" {
		t.Fatalf("symlink target was written: %q", data)
	}
	// A directory opens read-only-ish on some systems; it must be refused too.
	if err := AppendSignals(dir, []Signal{{Kind: ToolStarted, Source: SourceHook}}); err == nil {
		t.Fatal("AppendSignals accepted a directory")
	}
}

func TestAppendSignalsEmptyIsNoop(t *testing.T) {
	if err := AppendSignals(filepath.Join(t.TempDir(), "missing"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAppendSignalsWritesOneLinePerSignal(t *testing.T) {
	p := newSpool(t)
	err := AppendSignals(p, []Signal{
		{Kind: ToolStarted, At: t0, Source: SourceHook, Detail: "Bash\x1b[0m"},
		{Kind: Interrupted, At: t0, Source: SourceHook},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("spool = %q", data)
	}
	if strings.Contains(string(data), "\x1b") || !strings.Contains(lines[0], `"detail":"Bash[0m"`) {
		t.Fatalf("detail not sanitized: %q", lines[0])
	}
}

func TestAppendSignalsRejectsOversize(t *testing.T) {
	p := newSpool(t)
	// Source is not sanitized, so it is how a bad writer could blow the cap.
	err := AppendSignals(p, []Signal{
		{Kind: ToolStarted, Source: SourceHook},
		{Kind: ToolStarted, Source: Source(strings.Repeat("x", MaxLineBytes))},
	})
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("err = %v, want ErrLineTooLong", err)
	}
	if data, _ := os.ReadFile(p); len(data) != 0 {
		t.Fatalf("partial batch written: %q", data)
	}
}

func TestAppendSignalsConcurrentAppenders(t *testing.T) {
	p := newSpool(t)
	const writers, each = 8, 50
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				sig := Signal{Kind: ToolStarted, At: t0, Source: SourceHook, Detail: fmt.Sprintf("w%d-%d-%s", w, i, strings.Repeat("p", 100))}
				if err := AppendSignals(p, []Signal{sig}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	fn, ch := collect()
	stop, err := tail(context.Background(), p, fn, tailOptions{poll: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	seen := map[string]bool{}
	for range writers * each {
		select {
		case s := <-ch:
			seen[s.Detail] = true
		case <-time.After(waitFor):
			t.Fatalf("only %d lines intact", len(seen))
		}
	}
	if len(seen) != writers*each {
		t.Fatalf("distinct lines = %d, want %d (interleaved writes?)", len(seen), writers*each)
	}
}

func TestTailMissingFile(t *testing.T) {
	_, err := Tail(context.Background(), filepath.Join(t.TempDir(), "nope"), func(Signal) {})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}

// modes runs a tail test with fsnotify and with polling only.
func modes(t *testing.T, run func(t *testing.T, opts tailOptions)) {
	t.Run("fsnotify", func(t *testing.T) { run(t, tailOptions{poll: time.Hour, watch: true}) })
	t.Run("poll", func(t *testing.T) { run(t, tailOptions{poll: 10 * time.Millisecond}) })
}

func TestTailFollowsAppends(t *testing.T) {
	modes(t, func(t *testing.T, opts tailOptions) {
		p := newSpool(t)
		appendRaw(t, p, `{"kind":"session_started","source":"hook"}`+"\n")
		fn, ch := collect()
		stop, err := tail(context.Background(), p, fn, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		expect(t, ch, SessionStarted)
		if err := AppendSignals(p, []Signal{{Kind: PromptSubmitted, At: t0, Source: SourceHook}}); err != nil {
			t.Fatal(err)
		}
		got := expect(t, ch, PromptSubmitted)
		if !got[0].At.Equal(t0) || got[0].Source != SourceHook {
			t.Fatalf("signal = %+v", got[0])
		}
		expectNone(t, ch) // already-read lines are not replayed
	})
}

func TestTailKeepsPartialTrailingLine(t *testing.T) {
	modes(t, func(t *testing.T, opts tailOptions) {
		p := newSpool(t)
		fn, ch := collect()
		stop, err := tail(context.Background(), p, fn, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		appendRaw(t, p, `{"kind":"tool_sta`)
		expectNone(t, ch)
		appendRaw(t, p, `rted","source":"hook"}`)
		expectNone(t, ch) // still no newline
		appendRaw(t, p, "\n")
		expect(t, ch, ToolStarted)
	})
}

func TestTailSkipsBadLines(t *testing.T) {
	modes(t, func(t *testing.T, opts tailOptions) {
		p := newSpool(t)
		huge := `{"kind":"tool_started","detail":"` + strings.Repeat("x", MaxLineBytes) + `"}`
		appendRaw(t, p, strings.Join([]string{
			"not json",
			`{"kind":`,
			`{"source":"hook"}`, // no kind
			"",
			huge,
			`{"kind":"turn_completed","source":"hook","detail":"a\u001b[31mb"}`,
		}, "\n")+"\n")
		fn, ch := collect()
		stop, err := tail(context.Background(), p, fn, opts)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		got := expect(t, ch, TurnCompleted)
		if got[0].Detail != "a[31mb" {
			t.Fatalf("detail = %q", got[0].Detail)
		}
		expectNone(t, ch)
	})
}

func TestTailOversizeLineWithoutNewlineDoesNotGrow(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	r := &spoolReader{path: p, fn: fn}
	appendRaw(t, p, strings.Repeat("y", 10*MaxLineBytes))
	r.drain()
	if len(r.pending) != 0 || !r.discard {
		t.Fatalf("pending=%d discard=%v", len(r.pending), r.discard)
	}
	// The rest of the oversize line, then a good one.
	appendRaw(t, p, strings.Repeat("y", 100)+"\n"+`{"kind":"interrupted","source":"hook"}`+"\n")
	r.drain()
	expect(t, ch, Interrupted)
}

func TestTailOversizeAcrossPartialPending(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	r := &spoolReader{path: p, fn: fn}
	appendRaw(t, p, strings.Repeat("z", MaxLineBytes-10))
	r.drain()
	if len(r.pending) == 0 {
		t.Fatal("partial line not kept")
	}
	appendRaw(t, p, strings.Repeat("z", 100)+"\n"+`{"kind":"interrupted","source":"hook"}`+"\n")
	r.drain()
	expect(t, ch, Interrupted)
	expectNone(t, ch)
}

func TestTailRestartsAfterTruncate(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	r := &spoolReader{path: p, fn: fn}
	appendRaw(t, p, `{"kind":"session_started","source":"hook"}`+"\n")
	r.drain()
	expect(t, ch, SessionStarted)
	if err := os.WriteFile(p, []byte(`{"kind":"interrupted","source":"hook"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.drain()
	expect(t, ch, Interrupted)
}

func TestTailSurvivesRemovedFile(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	stop, err := tail(context.Background(), p, fn, tailOptions{poll: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	expectNone(t, ch)
	stop()
}

func TestTailStopDrainsLastLinesAndIsIdempotent(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	// No watcher and a slow poll: only the final read can deliver the line.
	stop, err := tail(context.Background(), p, fn, tailOptions{poll: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	appendRaw(t, p, `{"kind":"session_ended","source":"hook"}`+"\n")
	stop()
	stop()
	expect(t, ch, SessionEnded)
	appendRaw(t, p, `{"kind":"interrupted","source":"hook"}`+"\n")
	expectNone(t, ch) // nothing after stop returns
}

func TestTailContextCancelStops(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	ctx, cancel := context.WithCancel(context.Background())
	stop, err := tail(ctx, p, fn, tailOptions{poll: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	stop() // waits for the goroutine, returns promptly
	appendRaw(t, p, `{"kind":"interrupted","source":"hook"}`+"\n")
	expectNone(t, ch)
}

func TestTailPublicAPI(t *testing.T) {
	p := newSpool(t)
	fn, ch := collect()
	stop, err := Tail(context.Background(), p, fn)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := AppendSignals(p, []Signal{{Kind: PromptSubmitted, Source: SourceHook}}); err != nil {
		t.Fatal(err)
	}
	expect(t, ch, PromptSubmitted)
}
