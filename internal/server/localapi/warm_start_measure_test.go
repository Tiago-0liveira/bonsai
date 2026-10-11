package localapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type warmOperationMeasurement struct {
	Milliseconds       float64 `json:"milliseconds"`
	AllocatedBytes     uint64  `json:"allocatedBytes"`
	ObservedHeapGrowth uint64  `json:"observedHeapGrowthBytes"`
}

// observeWarmOperation samples live heap during the operation and at its end.
// This is an observed high-water mark, not a guarantee of the true peak.
// TotalAlloc counts transient allocations; retained heap is measured after GC
// separately, while the restored state is kept alive.
func observeWarmOperation(fn func()) warmOperationMeasurement {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	peak := before.HeapAlloc
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			if current.HeapAlloc > peak {
				peak = current.HeapAlloc
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	start := time.Now()
	fn()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	close(stop)
	<-done
	if after.HeapAlloc > peak {
		peak = after.HeapAlloc
	}
	return warmOperationMeasurement{float64(elapsed) / float64(time.Millisecond), after.TotalAlloc - before.TotalAlloc, peak - before.HeapAlloc}
}

// TestWarmStartCacheMeasurements repeats both fixture sizes 30 times. The
// opt-in test changes no persistence behavior and emits valid JSON for CI.
func TestWarmStartCacheMeasurements(t *testing.T) {
	if os.Getenv("BONSAI_P7_MEASURE") != "1" {
		t.Skip("set BONSAI_P7_MEASURE=1")
	}
	type sample struct {
		DocumentBytes int                      `json:"documentBytes"`
		Serialization warmOperationMeasurement `json:"serialization"`
		Load          warmOperationMeasurement `json:"loadAndDecode"`
		Restore       warmOperationMeasurement `json:"loadDecodeInstallSeed"`
		RetainedBytes uint64                   `json:"retainedBytes"`
	}
	type measurement struct {
		Worktrees        int      `json:"worktrees"`
		PRs              int      `json:"prs"`
		Projects         int      `json:"projects"`
		MaxRetainedBytes uint64   `json:"maxRetainedBytes"`
		Samples          []sample `json:"samples"`
	}
	report := struct {
		Go           string        `json:"go"`
		Platform     string        `json:"platform"`
		Arch         string        `json:"arch"`
		HeapMethod   string        `json:"heapMethod"`
		Measurements []measurement `json:"measurements"`
	}{runtime.Version(), runtime.GOOS, runtime.GOARCH, "100 microsecond live-heap sampling plus endpoints; maximum retained delta after GC with restored state kept alive; allocations include sampler overhead", nil}
	for _, size := range []struct{ worktrees, prs int }{{25, 50}, {100, 500}} {
		f := &warmFixture{t: t, base: t.TempDir(), gates: &warmGates{}, worktrees: size.worktrees, prs: size.prs, removed: map[string]bool{}}
		registry := f.registry()
		store := newWarmStore(t.TempDir(), "ns")
		var projects []warmTestProject
		for _, info := range registry.List() {
			entry, _ := registry.Lookup(info.ID)
			projects = append(projects, warmTestProject{id: info.ID, root: info.Path, daemon: entry.daemon, github: entry.github})
		}
		syncer := newStateSync(warmTestRegistry(projects...), newEventHub())
		syncer.ReconcileCatalog()
		for _, project := range projects {
			syncer.Queue(project.id, refreshAll, false)
		}
		for _, project := range projects {
			waitForProjection(t, syncer, project.id, warmFixtureSettled)
		}
		result := measurement{Worktrees: size.worktrees, PRs: size.prs, Projects: len(projects)}
		for run := 0; run < 30; run++ {
			var current sample
			var data []byte
			runtime.GC()
			current.Serialization = observeWarmOperation(func() {
				exports, priority := syncer.warmExports()
				var skipped int
				var err error
				data, skipped, err = encodeWarmDocument("ns", exports, priority, time.Now())
				if err != nil || skipped != 0 {
					t.Fatalf("encode: %v, skipped %d", err, skipped)
				}
			})
			current.DocumentBytes = len(data)
			if err := store.save(data); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			current.Load = observeWarmOperation(func() {
				if document, reason := store.load(); document == nil {
					t.Fatalf("load: %s", reason)
				}
			})
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var restored *stateSync
			current.Restore = observeWarmOperation(func() {
				restored = newStateSync(warmTestRegistry(projects...), newEventHub())
				restored.loadWarmStart(store)
			})
			if snapshot, _ := restored.CachedSnapshot(warmFixtureActive); snapshot.Freshness["local"].Source != freshnessSourceDisk {
				t.Fatal("active project not restored")
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			if after.HeapAlloc > before.HeapAlloc {
				current.RetainedBytes = after.HeapAlloc - before.HeapAlloc
			}
			if current.RetainedBytes > result.MaxRetainedBytes {
				result.MaxRetainedBytes = current.RetainedBytes
			}
			runtime.KeepAlive(restored)
			result.Samples = append(result.Samples, current)
		}
		runtime.KeepAlive(syncer)
		report.Measurements = append(report.Measurements, result)
		t.Logf("P7 cache %dwt/%dpr: %d samples, max retained %d B", size.worktrees, size.prs, len(result.Samples), result.MaxRetainedBytes)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("BONSAI_P7_CACHE_REPORT"); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
