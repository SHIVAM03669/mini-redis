// Package main implements a mini Redis-like in-memory cache server with HTTP API.
// Features include thread-safe operations, TTL support, and automatic expiration cleanup.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Cache represents an in-memory key-value store with expiration support.
// It uses a read-write mutex for thread-safe concurrent access.
type Cache struct {
	data    map[string]string   // Main storage: key -> value mapping
	expires map[string]time.Time // Expiration tracking: key -> expiration time
	mu      sync.RWMutex         // Read-write mutex for thread-safe operations
}

// NewCache creates and returns a new Cache instance with initialized maps.
func NewCache() *Cache {
	return &Cache{
		data:    make(map[string]string),
		expires: make(map[string]time.Time),
	}
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
}

// cleanup scans the cache and removes all expired keys.
// This method is called periodically by the background goroutine.
// Keys with zero expiration time (no expiry) are never removed.
func (c *Cache) cleanup() {
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

// Global cache instance shared across all HTTP handlers
var cache = NewCache()

// SetRequest represents the JSON payload for the /set endpoint
type SetRequest struct {
	Key   string `json:"key"`             // Required: the cache key
	Value string `json:"value"`           // Required: the value to store
	TTL   *int   `json:"ttl,omitempty"`  // Optional: time-to-live in seconds
}

// DelRequest represents the JSON payload for the /del endpoint
type DelRequest struct {
	Key string `json:"key"` // Required: the key to delete
}

// main initializes the cache server and starts the HTTP server.
// It also launches a background goroutine that periodically cleans up expired keys.
func main() {
	// Start background cleaner goroutine that runs every second
	// This proactively removes expired keys, simulating real cache behavior
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cache.cleanup()
		}
	}()

	// Register HTTP route handlers
	http.HandleFunc("/", healthHandler)    // Health check endpoint
	http.HandleFunc("/set", setHandler)   // POST: Set a key-value pair
	http.HandleFunc("/get", getHandler)   // GET: Retrieve a value by key
	http.HandleFunc("/del", delHandler)   // POST: Delete a key

	fmt.Println("Server running on http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}

// healthHandler responds to health check requests.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Mini Redis Server Running")
}

// setHandler handles POST requests to set a key-value pair in the cache.
// Expected JSON body: {"key": "string", "value": "string", "ttl": int (optional)}
func setHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode JSON request body
	var req SetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Key == "" || req.Value == "" {
		http.Error(w, "Missing key or value", http.StatusBadRequest)
		return
	}

	// Parse optional TTL (time-to-live in seconds)
	var ttl time.Duration
	if req.TTL != nil {
		if *req.TTL < 0 {
			http.Error(w, "Invalid TTL (must be a non-negative integer in seconds)", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(*req.TTL) * time.Second
	}

	// Store the key-value pair in the cache
	cache.Set(req.Key, req.Value, ttl)
	fmt.Fprintln(w, "OK key set")
}

// getHandler handles GET requests to retrieve a value by key.
// Expected query parameter: ?key=<key>
func getHandler(w http.ResponseWriter, r *http.Request) {
	// Extract key from query parameter
	key := r.URL.Query().Get("key")

	// Retrieve value from cache (automatically checks expiration)
	value, ok := cache.Get(key)
	if !ok {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	// Return the value
	fmt.Fprintln(w, value)
}

// delHandler handles POST requests to delete a key from the cache.
// Expected JSON body: {"key": "string"}
func delHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode JSON request body
	var req DelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required field
	if req.Key == "" {
		http.Error(w, "Missing key", http.StatusBadRequest)
		return
	}

	// Delete the key from the cache
	cache.Del(req.Key)
	fmt.Fprintln(w, "OK Key Deleted")
}
