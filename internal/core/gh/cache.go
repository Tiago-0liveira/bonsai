package gh

import (
	"strconv"
	"sync"
	"time"
)

// DefaultCacheTTL bounds how long cached gh results stay fresh. It is longer
// than the UI's 30s PR-state tick so every other tick is served from cache.
const DefaultCacheTTL = 60 * time.Second

// Cache memoizes the read-only gh queries bonsai repeats (PR list, per-PR CI
// checks) so periodic refreshes and post-op cascades don't each shell out to
// gh. Only successful results are cached; errors always re-fetch. Concurrent
// callers for the same key collapse into a single gh invocation.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu       sync.Mutex
	prs      map[string]prsCacheEntry
	checks   map[string]checksCacheEntry
	inFlight map[string]*cacheCall
}

type prsCacheEntry struct {
	prs []PR
	at  time.Time
}

type checksCacheEntry struct {
	checks []Check
	at     time.Time
}

// cacheCall is one in-flight fetch; late callers block on it and share the
// result.
type cacheCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

// NewCache returns a cache with the given TTL. A zero or negative TTL
// disables caching (every call fetches).
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		ttl:      ttl,
		now:      time.Now,
		prs:      map[string]prsCacheEntry{},
		checks:   map[string]checksCacheEntry{},
		inFlight: map[string]*cacheCall{},
	}
}

// Seams so tests can stub the gh subprocesses and the clock.
var (
	listPRsFunc = ListPRs
	checksFunc  = Checks
)

// PRs returns the repo's PRs in the given state, reusing the last successful
// fetch while it is younger than the TTL unless force is set.
func (c *Cache) PRs(dir, state string, force bool) ([]PR, error) {
	key := "prs\x00" + dir + "\x00" + state
	if !force {
		c.mu.Lock()
		e, ok := c.prs[key]
		c.mu.Unlock()
		if ok && c.ttl > 0 && c.now().Sub(e.at) < c.ttl {
			return e.prs, nil
		}
	}
	v, err := c.do(key, func() (any, error) { return listPRsFunc(dir, state) })
	if err != nil {
		return nil, err
	}
	prs := v.([]PR)
	c.mu.Lock()
	c.prs[key] = prsCacheEntry{prs: prs, at: c.now()}
	c.mu.Unlock()
	return prs, nil
}

// Checks returns a PR's CI checks, reusing the last successful fetch while it
// is younger than the TTL unless force is set.
func (c *Cache) Checks(dir string, number int, force bool) ([]Check, error) {
	key := "checks\x00" + dir + "\x00" + strconv.Itoa(number)
	if !force {
		c.mu.Lock()
		e, ok := c.checks[key]
		c.mu.Unlock()
		if ok && c.ttl > 0 && c.now().Sub(e.at) < c.ttl {
			return e.checks, nil
		}
	}
	v, err := c.do(key, func() (any, error) { return checksFunc(dir, number) })
	if err != nil {
		return nil, err
	}
	checks := v.([]Check)
	c.mu.Lock()
	c.checks[key] = checksCacheEntry{checks: checks, at: c.now()}
	c.mu.Unlock()
	return checks, nil
}

// do dedupes concurrent fetches under key: the first caller runs fetch, later
// callers block and share its result.
func (c *Cache) do(key string, fetch func() (any, error)) (any, error) {
	c.mu.Lock()
	if cl, ok := c.inFlight[key]; ok {
		c.mu.Unlock()
		cl.wg.Wait()
		return cl.val, cl.err
	}
	cl := &cacheCall{}
	cl.wg.Add(1)
	c.inFlight[key] = cl
	c.mu.Unlock()

	cl.val, cl.err = fetch()
	cl.wg.Done()

	c.mu.Lock()
	delete(c.inFlight, key)
	c.mu.Unlock()
	return cl.val, cl.err
}
