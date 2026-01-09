package cache

import (
	"fmt"
	"sync"
	"time"
)

// Cache represents an in-memory key-value store with expiration support.
// It uses a read-write mutex for thread-safe concurrent access.
type Cache struct {
	data            map[string]string   // Main storage: key -> value mapping
	expires         map[string]time.Time // Expiration tracking: key -> expiration time
	lastAccess      map[string]time.Time // LRU tracking: key -> last access time
	mu              sync.RWMutex         // Read-write mutex for thread-safe operations
	aof             *AOF                 // Append-only file for persistence
	snapshotManager *SnapshotManager   // Snapshot manager for periodic snapshots
	maxKeys         int                 // Maximum number of keys allowed (0 = unlimited)
}

// NewCache creates and returns a new Cache instance with initialized maps.
// It also initializes the AOF persistence layer and loads snapshot if available.
// maxKeys: Maximum number of keys allowed (0 = unlimited). When limit is reached, least recently used keys are evicted (LRU).
func NewCache(aofPath, snapshotPath string, maxKeys int) (*Cache, error) {
	c := &Cache{
		data:       make(map[string]string),
		expires:    make(map[string]time.Time),
		lastAccess: make(map[string]time.Time),
		maxKeys:    maxKeys,
	}

	// Load snapshot first (if it exists)
	loaded, err := c.LoadSnapshot(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot: %w", err)
	}
	if loaded {
		fmt.Printf("Loaded snapshot from %s\n", snapshotPath)
	}

	// Initialize AOF
	aof, err := NewAOF(aofPath, c)
	if err != nil {
		return nil, err
	}
	c.aof = aof

	// Replay AOF to restore any operations after snapshot
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
// If maxKeys is set and limit is reached, the least recently used key is evicted (LRU).
func (c *Cache) Set(key, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if this is an update to an existing key
	isNewKey := !c.hasKey(key)

	// Clean up expired keys first to ensure accurate count
	c.cleanupExpiredLocked()

	// If we're at the limit and this is a new key, evict the least recently used key
	// Only count valid (non-expired) keys
	if c.maxKeys > 0 && isNewKey && c.countValidKeys() >= c.maxKeys {
		c.evictLRU()
	}

	c.data[key] = value

	if ttl > 0 {
		// Set expiration time to current time + TTL
		c.expires[key] = time.Now().Add(ttl)
	} else {
		// No expiry - set to zero time (IsZero() check in Get/cleanup)
		c.expires[key] = time.Time{}
	}

	// Update last access time (mark as recently used)
	now := time.Now()
	c.lastAccess[key] = now

	// Log to AOF
	if c.aof != nil {
		c.aof.LogSet(key, value, ttl)
	}
}

// hasKey checks if a key exists in the cache (must be called with lock held).
func (c *Cache) hasKey(key string) bool {
	_, exists := c.data[key]
	return exists
}

// isExpired checks if a key is expired (must be called with lock held).
func (c *Cache) isExpired(key string) bool {
	expiresAt, hasExpiry := c.expires[key]
	if !hasExpiry {
		return false // No expiration set
	}
	if expiresAt.IsZero() {
		return false // Zero time means no expiry
	}
	return time.Now().After(expiresAt)
}

// countValidKeys returns the number of non-expired keys in the cache (must be called with lock held).
func (c *Cache) countValidKeys() int {
	if len(c.data) == 0 {
		return 0
	}
	
	now := time.Now()
	count := 0
	for key := range c.data {
		expiresAt, hasExpiry := c.expires[key]
		if !hasExpiry {
			count++ // No expiration, key is valid
			continue
		}
		if expiresAt.IsZero() {
			count++ // Zero time means no expiry, key is valid
			continue
		}
		if !now.After(expiresAt) {
			count++ // Not expired yet, key is valid
		}
	}
	return count
}

// evictLRU removes the least recently used key from the cache (LRU eviction).
// Only considers valid (non-expired) keys for eviction.
// Must be called with lock held.
func (c *Cache) evictLRU() {
	if len(c.lastAccess) == 0 {
		return // Nothing to evict
	}

	// Find the valid (non-expired) key with the oldest access time
	var lruKey string
	var oldestTime time.Time
	first := true

	for key, accessTime := range c.lastAccess {
		// Skip expired keys - they should not affect LRU order
		if c.isExpired(key) {
			continue
		}

		// Only consider valid keys for LRU eviction
		if first || accessTime.Before(oldestTime) {
			lruKey = key
			oldestTime = accessTime
			first = false
		}
	}

	if lruKey == "" {
		return // No valid key found to evict
	}

	// Remove from all maps
	delete(c.data, lruKey)
	delete(c.expires, lruKey)
	delete(c.lastAccess, lruKey)

	// Log deletion to AOF
	if c.aof != nil {
		c.aof.LogDel(lruKey)
	}
}

// Get retrieves a value by key from the cache.
// Returns the value and true if the key exists and is not expired.
// Returns empty string and false if the key doesn't exist or has expired.
// Expired keys are automatically deleted during the Get operation.
// This operation marks the key as recently used (LRU).
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
			// Key expired - delete it from all maps
			delete(c.data, key)
			delete(c.expires, key)
			delete(c.lastAccess, key)
			return "", false
		}
	}

	// Update last access time (mark as recently used for LRU)
	c.lastAccess[key] = time.Now()

	return value, true
}

// Del removes a key-value pair from the cache.
// Also removes the associated expiration entry if it exists.
func (c *Cache) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Remove from all maps
	delete(c.data, key)
	delete(c.expires, key)
	delete(c.lastAccess, key)

	// Log to AOF
	if c.aof != nil {
		c.aof.LogDel(key)
	}
}

// cleanupExpiredLocked removes expired keys from the cache.
// Must be called with lock held.
func (c *Cache) cleanupExpiredLocked() {
	now := time.Now()
	for key, expiresAt := range c.expires {
		// Only check keys with non-zero expiration time
		// Zero time means the key never expires
		if !expiresAt.IsZero() && now.After(expiresAt) {
			// Key has expired - remove it from all maps immediately
			// This ensures expired keys don't affect LRU order
			delete(c.data, key)
			delete(c.expires, key)
			delete(c.lastAccess, key)
		}
	}
}

// Cleanup scans the cache and removes all expired keys.
// This method is called periodically by the background goroutine.
// Keys with zero expiration time (no expiry) are never removed.
// Expired keys are removed immediately to prevent them from affecting LRU order.
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupExpiredLocked()
}

// setInternal is used by AOF replay to set values without logging to AOF.
// This prevents infinite loops during replay.
func (c *Cache) setInternal(key, value string, ttl time.Duration) {
	isNewKey := !c.hasKey(key)
	
	// Clean up expired keys first
	c.cleanupExpiredLocked()
	
	// If we're at the limit and this is a new key, evict the least recently used key
	// Only count valid (non-expired) keys
	if c.maxKeys > 0 && isNewKey && c.countValidKeys() >= c.maxKeys {
		c.evictLRU()
	}

	c.data[key] = value

	if ttl > 0 {
		c.expires[key] = time.Now().Add(ttl)
	} else {
		c.expires[key] = time.Time{}
	}

	// Update last access time
	now := time.Now()
	c.lastAccess[key] = now
}

// delInternal is used by AOF replay to delete values without logging to AOF.
func (c *Cache) delInternal(key string) {
	delete(c.data, key)
	delete(c.expires, key)
	delete(c.lastAccess, key)
}
