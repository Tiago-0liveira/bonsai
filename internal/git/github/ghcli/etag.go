package ghcli

import (
	"container/list"
	"net/http"
	"sync"
)

const (
	// etagCacheBytes bounds the in-memory conditional-request cache.
	etagCacheBytes = 32 << 20
	// etagMaxEntryBytes skips bodies too large to be worth keeping.
	etagMaxEntryBytes = 4 << 20
)

// etagEntry is a GET response kept for conditional requests. The body is
// stored raw and decoded again on a 304.
type etagEntry struct {
	key          string
	tokenID      string
	etag         string
	lastModified string
	header       http.Header
	body         []byte
}

func (e *etagEntry) size() int { return len(e.key) + len(e.body) + 256 }

// etagCache is an LRU bounded by total bytes.
type etagCache struct {
	mu    sync.Mutex
	max   int
	used  int
	order *list.List
	items map[string]*list.Element
}

func newETagCache(max int) *etagCache {
	return &etagCache{max: max, order: list.New(), items: map[string]*list.Element{}}
}

// get returns the entry for key if it was stored under the same token. A
// different login must not reuse another login's view of a resource.
func (c *etagCache) get(key, tokenID string) *etagEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.items[key]
	if element == nil {
		return nil
	}
	entry := element.Value.(*etagEntry)
	if entry.tokenID != tokenID {
		c.removeLocked(element)
		return nil
	}
	c.order.MoveToFront(element)
	return entry
}

func (c *etagCache) put(entry *etagEntry) {
	if entry.size() > etagMaxEntryBytes || entry.size() > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.items[entry.key]; element != nil {
		c.removeLocked(element)
	}
	c.items[entry.key] = c.order.PushFront(entry)
	c.used += entry.size()
	for c.used > c.max {
		c.removeLocked(c.order.Back())
	}
}

func (c *etagCache) removeLocked(element *list.Element) {
	entry := c.order.Remove(element).(*etagEntry)
	delete(c.items, entry.key)
	c.used -= entry.size()
}

func (c *etagCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
