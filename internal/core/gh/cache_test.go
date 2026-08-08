package gh

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// stubGH swaps the fetch seams and returns a restore function.
func stubGH(t *testing.T, prs func(dir, state string) ([]PR, error), checks func(dir string, number int) ([]Check, error)) {
	t.Helper()
	oldPRs, oldChecks := listPRsFunc, checksFunc
	listPRsFunc, checksFunc = prs, checks
	t.Cleanup(func() { listPRsFunc, checksFunc = oldPRs, oldChecks })
}

// fakeClock is a manually-advanced clock for TTL tests.
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func TestCachePRsTTL(t *testing.T) {
	n := 0
	stubGH(t, func(dir, state string) ([]PR, error) {
		n++
		return []PR{{Number: n}}, nil
	}, nil)

	clock := &fakeClock{t: time.Now()}
	c := NewCache(DefaultCacheTTL)
	c.now = clock.now

	first, err := c.PRs("/repo", "all", false)
	if err != nil || len(first) != 1 || first[0].Number != 1 {
		t.Fatalf("first fetch = %v, %v", first, err)
	}
	if n != 1 {
		t.Fatalf("fetches after first call = %d, want 1", n)
	}

	// Within TTL: cached, no new fetch.
	clock.advance(30 * time.Second)
	second, err := c.PRs("/repo", "all", false)
	if err != nil || second[0].Number != 1 {
		t.Fatalf("cached fetch = %v, %v", second, err)
	}
	if n != 1 {
		t.Fatalf("fetches within TTL = %d, want 1", n)
	}

	// Past TTL: refetch.
	clock.advance(40 * time.Second)
	third, err := c.PRs("/repo", "all", false)
	if err != nil || third[0].Number != 2 {
		t.Fatalf("expired fetch = %v, %v", third, err)
	}
	if n != 2 {
		t.Fatalf("fetches past TTL = %d, want 2", n)
	}

	// Force bypasses a fresh cache.
	forced, err := c.PRs("/repo", "all", true)
	if err != nil || forced[0].Number != 3 {
		t.Fatalf("forced fetch = %v, %v", forced, err)
	}
	if n != 3 {
		t.Fatalf("fetches after force = %d, want 3", n)
	}

	// Distinct states cache separately.
	if _, err := c.PRs("/repo", "open", true); err != nil || n != 4 {
		t.Fatalf("separate state fetch: n=%d, %v", n, err)
	}
}

func TestCacheChecksTTL(t *testing.T) {
	n := 0
	stubGH(t, nil, func(dir string, number int) ([]Check, error) {
		n++
		return []Check{{Name: "ci", Bucket: "pass"}}, nil
	})

	clock := &fakeClock{t: time.Now()}
	c := NewCache(DefaultCacheTTL)
	c.now = clock.now

	if _, err := c.Checks("/repo", 7, false); err != nil || n != 1 {
		t.Fatalf("first checks fetch: n=%d, %v", n, err)
	}
	if _, err := c.Checks("/repo", 7, false); err != nil || n != 1 {
		t.Fatalf("cached checks: n=%d, %v", n, err)
	}
	// A different PR number is a different cache key.
	if _, err := c.Checks("/repo", 8, false); err != nil || n != 2 {
		t.Fatalf("other PR checks: n=%d, %v", n, err)
	}
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	n := 0
	stubGH(t, func(dir, state string) ([]PR, error) {
		n++
		if n == 1 {
			return nil, errors.New("gh down")
		}
		return []PR{{Number: 9}}, nil
	}, nil)

	c := NewCache(DefaultCacheTTL)
	if _, err := c.PRs("/repo", "all", false); err == nil {
		t.Fatal("expected first fetch error")
	}
	got, err := c.PRs("/repo", "all", false)
	if err != nil || got[0].Number != 9 {
		t.Fatalf("retry after error = %v, %v", got, err)
	}
	if n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

func TestCacheInFlightDedupe(t *testing.T) {
	var n int
	var mu sync.Mutex
	release := make(chan struct{})
	stubGH(t, func(dir, state string) ([]PR, error) {
		mu.Lock()
		n++
		mu.Unlock()
		<-release
		return []PR{{Number: 42}}, nil
	}, nil)

	c := NewCache(DefaultCacheTTL)
	var wg sync.WaitGroup
	results := make([][]PR, 5)
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = c.PRs("/repo", "all", false)
		}(i)
	}
	// Give the goroutines time to pile up on the in-flight call, then release.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if n != 1 {
		t.Fatalf("concurrent fetches ran %d times, want 1", n)
	}
	for i, r := range results {
		if errs[i] != nil || len(r) != 1 || r[0].Number != 42 {
			t.Fatalf("caller %d got %v, %v", i, r, errs[i])
		}
	}
}
