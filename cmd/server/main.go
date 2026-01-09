// Package main implements a mini Redis-like in-memory cache server with HTTP API.
// Features include thread-safe operations, TTL support, automatic expiration cleanup,
// and Append-Only File (AOF) persistence for crash recovery.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mini-redis/internal/cache"
)

// Global cache instance shared across all HTTP handlers
var cacheInstance *cache.Cache

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
	// Determine AOF file path (default: data/appendonly.aof)
	aofPath := "data/appendonly.aof"
	if len(os.Args) > 1 {
		aofPath = os.Args[1]
	}

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(aofPath), 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize cache with AOF persistence
	var err error
	cacheInstance, err = cache.NewCache(aofPath)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheInstance.Close()

	fmt.Printf("Cache initialized with AOF: %s\n", aofPath)

	// Start background cleaner goroutine that runs every second
	// This proactively removes expired keys, simulating real cache behavior
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cacheInstance.Cleanup()
		}
	}()

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down gracefully...")
		if err := cacheInstance.Close(); err != nil {
			log.Printf("Error closing cache: %v", err)
		}
		os.Exit(0)
	}()

	// Register HTTP route handlers
	http.HandleFunc("/", healthHandler)    // Health check endpoint
	http.HandleFunc("/set", setHandler)   // POST: Set a key-value pair
	http.HandleFunc("/get", getHandler)   // GET: Retrieve a value by key
	http.HandleFunc("/del", delHandler)   // POST: Delete a key

	fmt.Println("Server running on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
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
	cacheInstance.Set(req.Key, req.Value, ttl)
	fmt.Fprintln(w, "OK key set")
}

// getHandler handles GET requests to retrieve a value by key.
// Expected query parameter: ?key=<key>
func getHandler(w http.ResponseWriter, r *http.Request) {
	// Extract key from query parameter
	key := r.URL.Query().Get("key")

	// Retrieve value from cache (automatically checks expiration)
	value, ok := cacheInstance.Get(key)
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
	cacheInstance.Del(req.Key)
	fmt.Fprintln(w, "OK Key Deleted")
}
