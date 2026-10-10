// Package trace provides opt-in timing spans and process-spawn counters for the
// sync pipeline. Spans are JSON lines written to stderr when BONSAI_SYNC_TRACE=1.
// With tracing off a span costs one boolean load; spawn counters are a single
// atomic add and are always maintained so benchmarks can read them.
package trace

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	gitSpawns atomic.Int64
	ghSpawns  atomic.Int64

	enabled atomic.Bool
	mu      sync.Mutex
	out     io.Writer = os.Stderr
)

func init() { enabled.Store(os.Getenv("BONSAI_SYNC_TRACE") == "1") }

// SetEnabled overrides the environment setting (benchmarks and tests).
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports whether spans are emitted.
func Enabled() bool { return enabled.Load() }

// SetOutput redirects span output and returns a restore function.
func SetOutput(w io.Writer) func() {
	mu.Lock()
	previous := out
	out = w
	mu.Unlock()
	return func() {
		mu.Lock()
		out = previous
		mu.Unlock()
	}
}

// AddGit records one spawned git process.
func AddGit() { gitSpawns.Add(1) }

// AddGH records one spawned gh process.
func AddGH() { ghSpawns.Add(1) }

// Counts returns the process-wide spawn totals.
func Counts() (git, gh int64) { return gitSpawns.Load(), ghSpawns.Load() }

// Attrs are optional span dimensions.
type Attrs struct {
	Worktree string
	Page     int
}

type record struct {
	TS        string  `json:"ts"`
	Span      string  `json:"span"`
	Project   string  `json:"project,omitempty"`
	Worktree  string  `json:"worktree,omitempty"`
	Page      int     `json:"page,omitempty"`
	DurMS     float64 `json:"dur_ms"`
	GitSpawns int64   `json:"git_spawns"`
	GHSpawns  int64   `json:"gh_spawns"`
}

func noop() {}

// Start begins a span and returns the function that ends it. Spawn counts in
// the record are the process-wide deltas observed during the span, so
// concurrent spans overlap; treat them as an upper bound per span.
func Start(name, project string, attrs ...Attrs) func() {
	if !enabled.Load() {
		return noop
	}
	started := time.Now()
	g0, h0 := Counts()
	var a Attrs
	if len(attrs) > 0 {
		a = attrs[0]
	}
	return func() {
		g1, h1 := Counts()
		line, err := json.Marshal(record{
			TS:        started.UTC().Format(time.RFC3339Nano),
			Span:      name,
			Project:   project,
			Worktree:  a.Worktree,
			Page:      a.Page,
			DurMS:     float64(time.Since(started).Microseconds()) / 1000,
			GitSpawns: g1 - g0,
			GHSpawns:  h1 - h0,
		})
		if err != nil {
			return
		}
		mu.Lock()
		_, _ = out.Write(append(line, '\n'))
		mu.Unlock()
	}
}
