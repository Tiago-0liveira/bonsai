package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSyncBenchReportsMilestonesSpawnsAndSpans(t *testing.T) {
	root := t.TempDir()
	repo := repoFixture(t, filepath.Join(root, "one"))
	gitFixture(t, repo, "worktree", "add", "-b", "feature", filepath.Join(t.TempDir(), "feature"))
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := config.UpdateProjectRoots(path, "one", 0, root, ""); err != nil {
		t.Fatal(err)
	}
	registry := newProjectRegistry(path, "")
	if _, err := registry.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	selectAllDiscovered(t, registry)

	// Provider goroutines may still be finishing a span when the bench returns.
	spans := &lockedBuffer{}
	defer trace.SetOutput(spans)()
	trace.SetEnabled(true)
	defer trace.SetEnabled(false)

	result, err := SyncBench(context.Background(), SyncBenchOptions{ProjectRootsPath: path, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Incomplete {
		t.Fatalf("unexpected result: %+v", result)
	}
	row := result.Rows[0]
	if row.Worktrees != 2 || row.Inventory == 0 || row.LocalReady == 0 || row.PRCatalog == 0 || row.AllChecks == 0 {
		t.Fatalf("milestones missing: %+v", row)
	}
	// No remote is configured, so the provider is unavailable and gh is never used.
	if result.GitSpawns == 0 || result.GHSpawns != 0 {
		t.Fatalf("spawns git=%d gh=%d", result.GitSpawns, result.GHSpawns)
	}
	seen := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(spans.String()), "\n") {
		var rec struct{ Span string }
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("span line is not JSON: %q", line)
		}
		seen[rec.Span]++
	}
	for _, name := range []string{"local.inventory", "local.ready", "provider.ready"} {
		if seen[name] == 0 {
			t.Errorf("missing span %s; saw %v", name, seen)
		}
	}
	if seen["local.status"] != 2 {
		t.Errorf("local.status spans = %d, want one per worktree (2)", seen["local.status"])
	}
}

func TestSyncBenchWithoutProjectsFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := SyncBench(context.Background(), SyncBenchOptions{ProjectRootsPath: path}); err == nil {
		t.Fatal("expected an error when no projects are selected")
	}
}

func TestSyncBenchRootAndRepoFilter(t *testing.T) {
	root := t.TempDir()
	one := repoFixture(t, filepath.Join(root, "one"))
	repoFixture(t, filepath.Join(root, "two"))
	result, err := SyncBench(context.Background(), SyncBenchOptions{Root: root, Repo: one, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Name != "one" || result.Rows[0].Incomplete {
		t.Fatalf("expected only repo one: %+v", result.Rows)
	}
	all, err := SyncBench(context.Background(), SyncBenchOptions{Root: root, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 2 {
		t.Fatalf("expected both repos: %+v", all.Rows)
	}
}
