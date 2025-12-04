package steam

import (
	"sync"
	"time"
)

// CacheEntry holds cached data with expiration time
type CacheEntry[T any] struct {
	Data      T
	ExpiresAt time.Time
}

// TTLCache is a generic thread-safe cache with TTL support
type TTLCache[K comparable, V any] struct {
	mu       sync.RWMutex
	data     map[K]CacheEntry[V]
	ttl      time.Duration
	maxSize  int
	cleanupN int // number of oldest items to remove on cleanup
}

// CacheOption is a functional option for configuring the cache
type CacheOption[K comparable, V any] func(*TTLCache[K, V])

// WithTTL sets the TTL for cache entries
func WithTTL[K comparable, V any](ttl time.Duration) CacheOption[K, V] {
	return func(c *TTLCache[K, V]) {
		c.ttl = ttl
	}
}

// WithMaxSize sets the maximum size of the cache
func WithMaxSize[K comparable, V any](size int) CacheOption[K, V] {
	return func(c *TTLCache[K, V]) {
		c.maxSize = size
	}
}

// WithCleanupCount sets how many items to remove when cache is full
func WithCleanupCount[K comparable, V any](n int) CacheOption[K, V] {
	return func(c *TTLCache[K, V]) {
		c.cleanupN = n
	}
}

// NewTTLCache creates a new TTL cache with the given options
func NewTTLCache[K comparable, V any](opts ...CacheOption[K, V]) *TTLCache[K, V] {
	cache := &TTLCache[K, V]{
		data:     make(map[K]CacheEntry[V]),
		ttl:      10 * time.Minute, // default TTL
		maxSize:  100,              // default max size
		cleanupN: 25,               // default cleanup count
	}

	for _, opt := range opts {
		opt(cache)
	}

	return cache
}

// Get retrieves a value from the cache. Returns the value and true if found and not expired.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	if !exists {
		var zero V
		return zero, false
	}

	if time.Now().After(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.data, key)
		c.mu.Unlock()
		var zero V
		return zero, false
	}

	return entry.Data, true
}

// Set stores a value in the cache with the configured TTL
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clean up if at max size
	if len(c.data) >= c.maxSize {
		c.cleanupExpired()
		// If still at max, remove oldest entries
		if len(c.data) >= c.maxSize {
			c.removeOldest()
		}
	}

	c.data[key] = CacheEntry[V]{
		Data:      value,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// GetOrFetch attempts to get from cache, or fetches using the provided function
func (c *TTLCache[K, V]) GetOrFetch(key K, fetch func() (V, error)) (V, error) {
	if cached, ok := c.Get(key); ok {
		return cached, nil
	}

	value, err := fetch()
	if err != nil {
		var zero V
		return zero, err
	}

	c.Set(key, value)
	return value, nil
}

// cleanupExpired removes all expired entries (must be called with lock held)
func (c *TTLCache[K, V]) cleanupExpired() {
	now := time.Now()
	for key, entry := range c.data {
		if now.After(entry.ExpiresAt) {
			delete(c.data, key)
		}
	}
}

// removeOldest removes the oldest entries (must be called with lock held)
func (c *TTLCache[K, V]) removeOldest() {
	type keyExpiry struct {
		key       K
		expiresAt time.Time
	}

	entries := make([]keyExpiry, 0, len(c.data))
	for k, v := range c.data {
		entries = append(entries, keyExpiry{key: k, expiresAt: v.ExpiresAt})
	}

	// Sort by expiry time (soonest first = oldest)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].expiresAt.After(entries[j].expiresAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove the oldest entries
	removeCount := min(c.cleanupN, len(entries))
	for i := range removeCount {
		delete(c.data, entries[i].key)
	}
}

// Size returns the current number of items in the cache
func (c *TTLCache[K, V]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Clear removes all entries from the cache
func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[K]CacheEntry[V])
}

// Global cache instance for Steam app details
var appDetailsCache = NewTTLCache[string, *SteamAppDetails](
	WithTTL[string, *SteamAppDetails](15*time.Minute),
	WithMaxSize[string, *SteamAppDetails](200),
	WithCleanupCount[string, *SteamAppDetails](50),
)

// GetAppDetailsCache returns the global app details cache
func GetAppDetailsCache() *TTLCache[string, *SteamAppDetails] {
	return appDetailsCache
}
