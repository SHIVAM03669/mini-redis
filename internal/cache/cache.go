package cache

import (
	"sync"
	"time"
)

// Cache represents an in-memory key-value store with expiration support.
// It uses a read-write mutex for thread-safe concurrent access.
type Cache struct {
	data    map[string]string   // Main storage: key -> value mapping
	expires map[string]time.Time // Expiration tracking: key -> expiration time
	mu      sync.RWMutex         // Read-write mutex for thread-safe operations
	aof     *AOF                 // Append-only file for persistence
}

// NewCache creates and returns a new Cache instance with initialized maps.
// It also initializes the AOF persistence layer.
func NewCache(aofPath string) (*Cache, error) {
	c := &Cache{
		data:    make(map[string]string),
		expires: make(map[string]time.Time),
	}

	// Initialize AOF
	aof, err := NewAOF(aofPath, c)
	if err != nil {
		return nil, err
	}
	c.aof = aof

	// Replay AOF to restore data
	if err := aof.Replay(); err != nil {
		return nil, err
	}

	return c, nil
}

// Close gracefully shuts down the cache and closes the AOF file.
func (c *Cache) Close() error {
	if c.aof != nil {
		return c.aof.Close()
	}
	return nil
}

// Set stores a key-value pair in the cache.
// If ttl > 0, the key will expire after the specified duration.
// If ttl == 0, the key will never expire (zero time is used as a marker).
func (c *Cache) Set(key, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value

	if ttl > 0 {
		// Set expiration time to current time + TTL
		c.expires[key] = time.Now().Add(ttl)
	} else {
		// No expiry - set to zero time (IsZero() check in Get/cleanup)
		c.expires[key] = time.Time{}
	}

	// Log to AOF
	if c.aof != nil {
		c.aof.LogSet(key, value, ttl)
	}
}

// Get retrieves a value by key from the cache.
// Returns the value and true if the key exists and is not expired.
// Returns empty string and false if the key doesn't exist or has expired.
// Expired keys are automatically deleted during the Get operation.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key exists in the data map
	value, ok := c.data[key]
	if !ok {
		return "", false
	}

	// Check if the key has expired
	expiresAt, hasExpiry := c.expires[key]
	if hasExpiry && !expiresAt.IsZero() {
		// Key has an expiration time set, check if it's expired
		if time.Now().After(expiresAt) {
			// Key expired - delete it from both maps
			delete(c.data, key)
			delete(c.expires, key)
			return "", false
		}
	}

	return value, true
}

// Del removes a key-value pair from the cache.
// Also removes the associated expiration entry if it exists.
func (c *Cache) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	delete(c.expires, key)

	// Log to AOF
	if c.aof != nil {
		c.aof.LogDel(key)
	}
}

// cleanup scans the cache and removes all expired keys.
// This method is called periodically by the background goroutine.
// Keys with zero expiration time (no expiry) are never removed.
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, expiresAt := range c.expires {
		// Only check keys with non-zero expiration time
		// Zero time means the key never expires
		if !expiresAt.IsZero() && now.After(expiresAt) {
			// Key has expired - remove it from both maps
			delete(c.data, key)
			delete(c.expires, key)
		}
	}
}

// setInternal is used by AOF replay to set values without logging to AOF.
// This prevents infinite loops during replay.
func (c *Cache) setInternal(key, value string, ttl time.Duration) {
	c.data[key] = value

	if ttl > 0 {
		c.expires[key] = time.Now().Add(ttl)
	} else {
		c.expires[key] = time.Time{}
	}
}

// delInternal is used by AOF replay to delete values without logging to AOF.
func (c *Cache) delInternal(key string) {
	delete(c.data, key)
	delete(c.expires, key)
}
