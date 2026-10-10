package localapi

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// TestWarmStartCacheMeasurements reports the warm-start document size, the
// restore time and the memory a restore retains, for the Phase 7 fixture
// sizes. Run with BONSAI_P7_MEASURE=1 -v.
func TestWarmStartCacheMeasurements(t *testing.T) {
	if os.Getenv("BONSAI_P7_MEASURE") != "1" {
		t.Skip("set BONSAI_P7_MEASURE=1")
	}
	for _, size := range []struct{ worktrees, prs int }{{25, 50}, {100, 500}} {
		f := &warmFixture{t: t, base: t.TempDir(), gates: &warmGates{}, worktrees: size.worktrees, prs: size.prs, removed: map[string]bool{}}
		registry := f.registry()
		store := newWarmStore(t.TempDir(), "ns")
		var projects []warmTestProject
		for _, info := range registry.List() {
			entry, _ := registry.Lookup(info.ID)
			projects = append(projects, warmTestProject{id: info.ID, root: info.Path, daemon: entry.daemon, github: entry.github})
		}
		// Encoding cost: allocations of one save of the settled projections.
		syncer := newStateSync(warmTestRegistry(projects...), newEventHub())
		syncer.ReconcileCatalog()
		for _, project := range projects {
			syncer.Queue(project.id, refreshAll, false)
		}
		for _, project := range projects {
			waitForProjection(t, syncer, project.id, warmFixtureSettled)
		}
		exports, priority := syncer.warmExports()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		data, skipped, err := encodeWarmDocument("ns", exports, priority, time.Now())
		runtime.ReadMemStats(&after)
		if err != nil || skipped != 0 {
			t.Fatalf("encode: %v, skipped %d", err, skipped)
		}
		encodeAlloc := after.TotalAlloc - before.TotalAlloc
		if err := store.save(data); err != nil {
			t.Fatal(err)
		}

		// Restore: load, decode, install and seed, best of five.
		var best time.Duration
		var retained uint64
		for run := 0; run < 5; run++ {
			runtime.GC()
			runtime.ReadMemStats(&before)
			start := time.Now()
			restored := newStateSync(warmTestRegistry(projects...), newEventHub())
			restored.loadWarmStart(store)
			elapsed := time.Since(start)
			runtime.GC()
			runtime.ReadMemStats(&after)
			if snapshot, _ := restored.CachedSnapshot(warmFixtureActive); snapshot.Freshness["local"].Source != freshnessSourceDisk {
				t.Fatal("active project not restored")
			}
			if best == 0 || elapsed < best {
				best = elapsed
			}
			if after.HeapAlloc > before.HeapAlloc {
				retained = after.HeapAlloc - before.HeapAlloc
			}
			runtime.KeepAlive(restored)
		}
		t.Logf("P7 cache %dwt/%dpr (3 projects): document %d B, encode allocations %d B, restore %v (best of 5), retained after restore %d B",
			size.worktrees, size.prs, len(data), encodeAlloc, best, retained)
	}
}
