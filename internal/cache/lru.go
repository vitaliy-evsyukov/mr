package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"meters-reader/internal/analyzer"
)

type cacheEntry struct {
	key       string
	report    *analyzer.ConsumptionReport
	cachedAt  time.Time
	expiresAt time.Time
}

type LRUCache struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element
	evict    *list.List
}

func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 50
	}
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		evict:    list.New(),
	}
}

// GenerateKey generates a privacy-preserving hash key from email, SP ID, and days.
// (Password is deliberately NOT used or stored in the cache key).
func GenerateKey(email, spID string, daysBack int) string {
	raw := fmt.Sprintf("%s:%s:%d", email, spID, daysBack)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:16]) // 32 hex chars
}

func (c *LRUCache) Get(key string) (*analyzer.ConsumptionReport, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[key]
	if !found {
		return nil, time.Time{}, false
	}

	entry := elem.Value.(*cacheEntry)
	// Check expiration
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		return nil, time.Time{}, false
	}

	c.evict.MoveToFront(elem)
	return entry.report, entry.cachedAt, true
}

func (c *LRUCache) Set(key string, report *analyzer.ConsumptionReport, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expiresAt := now.Add(ttl)

	// Update existing if present
	if elem, found := c.items[key]; found {
		c.evict.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		entry.report = report
		entry.cachedAt = now
		entry.expiresAt = expiresAt
		return
	}

	// Evict oldest if capacity exceeded
	if c.evict.Len() >= c.capacity {
		oldest := c.evict.Back()
		if oldest != nil {
			c.removeElement(oldest)
		}
	}

	entry := &cacheEntry{
		key:       key,
		report:    report,
		cachedAt:  now,
		expiresAt: expiresAt,
	}
	elem := c.evict.PushFront(entry)
	c.items[key] = elem
}

func (c *LRUCache) removeElement(elem *list.Element) {
	c.evict.Remove(elem)
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
}
