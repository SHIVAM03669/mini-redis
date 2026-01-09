package cache

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SnapshotEntry represents a single key-value pair with expiration info in a snapshot.
type SnapshotEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"` // Zero time means no expiration
}

// Snapshot represents the full cache state saved to disk.
type Snapshot struct {
	Version   string          `json:"version"`   // Snapshot format version
	Timestamp time.Time       `json:"timestamp"` // When snapshot was created
	Entries   []SnapshotEntry `json:"entries"`   // All key-value pairs
}

// SaveSnapshot saves the current cache state to disk as a snapshot.
// This creates a point-in-time backup of all data.
func (c *Cache) SaveSnapshot(snapshotPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create snapshot structure
	snapshot := Snapshot{
		Version:   "1.0",
		Timestamp: time.Now(),
		Entries:   make([]SnapshotEntry, 0, len(c.data)),
	}

	// Copy all non-expired entries to snapshot
	now := time.Now()
	for key, value := range c.data {
		expiresAt, hasExpiry := c.expires[key]
		
		// Skip expired keys
		if hasExpiry && !expiresAt.IsZero() && now.After(expiresAt) {
			continue
		}

		entry := SnapshotEntry{
			Key:   key,
			Value: value,
		}

		// Include expiration time if it exists
		if hasExpiry && !expiresAt.IsZero() {
			entry.ExpiresAt = expiresAt
		}

		snapshot.Entries = append(snapshot.Entries, entry)
	}

	// Write snapshot to temporary file first (atomic write)
	tmpPath := snapshotPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()

	// Encode snapshot as JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		os.Remove(tmpPath) // Clean up on error
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := file.Sync(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync snapshot: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close snapshot file: %w", err)
	}

	// Atomically replace old snapshot with new one
	if err := os.Rename(tmpPath, snapshotPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename snapshot file: %w", err)
	}

	return nil
}

// LoadSnapshot loads a snapshot from disk and restores the cache state.
// Returns true if snapshot was loaded, false if snapshot doesn't exist.
func (c *Cache) LoadSnapshot(snapshotPath string) (bool, error) {
	// Check if snapshot file exists
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		return false, nil // No snapshot exists, that's okay
	}

	// Check if file is empty
	fileInfo, err := os.Stat(snapshotPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat snapshot file: %w", err)
	}
	if fileInfo.Size() == 0 {
		// Empty file, treat as no snapshot
		return false, nil
	}

	// Open snapshot file
	file, err := os.Open(snapshotPath)
	if err != nil {
		return false, fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer file.Close()

	// Decode snapshot
	var snapshot Snapshot
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&snapshot); err != nil {
		// If file is empty or corrupted, treat as no snapshot (don't fail startup)
		// This can happen if a previous snapshot write was interrupted
		return false, nil
	}

	// Restore cache state (without logging to AOF)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear existing data
	c.data = make(map[string]string)
	c.expires = make(map[string]time.Time)
	c.lastAccess = make(map[string]time.Time)

	// Restore entries
	now := time.Now()
	for _, entry := range snapshot.Entries {
		// Skip entries that are already expired
		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			continue
		}

		c.data[entry.Key] = entry.Value

		if !entry.ExpiresAt.IsZero() {
			c.expires[entry.Key] = entry.ExpiresAt
		} else {
			c.expires[entry.Key] = time.Time{} // No expiration
		}

		// Set last access time to current time (keys loaded from snapshot are considered recently accessed)
		c.lastAccess[entry.Key] = now
	}

	return true, nil
}

// ClearAOF truncates the AOF file to zero length.
// This is called after creating a snapshot to prevent infinite growth.
func (c *Cache) ClearAOF() error {
	if c.aof == nil {
		return nil
	}

	c.aof.mu.Lock()
	defer c.aof.mu.Unlock()

	// Flush any pending writes
	if c.aof.writer != nil {
		if err := c.aof.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush AOF before clearing: %w", err)
		}
	}

	// Close the current file
	if c.aof.file != nil {
		if err := c.aof.file.Close(); err != nil {
			return fmt.Errorf("failed to close AOF before clearing: %w", err)
		}
	}

	// Truncate the file to zero length and reopen in append mode
	file, err := os.OpenFile(c.aof.filePath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to truncate AOF file: %w", err)
	}
	file.Close() // Close the truncated file

	// Reopen in append mode for future writes
	file, err = os.OpenFile(c.aof.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen AOF file: %w", err)
	}

	// Recreate writer
	c.aof.file = file
	c.aof.writer = bufio.NewWriter(file)

	return nil
}

// CreateSnapshotAndClearAOF creates a snapshot and then clears the AOF file.
// This is the main method to call for periodic snapshots.
func (c *Cache) CreateSnapshotAndClearAOF(snapshotPath string) error {
	// Save snapshot
	if err := c.SaveSnapshot(snapshotPath); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Clear AOF after successful snapshot
	if err := c.ClearAOF(); err != nil {
		return fmt.Errorf("failed to clear AOF after snapshot: %w", err)
	}

	return nil
}

// SnapshotManager manages periodic snapshot creation.
type SnapshotManager struct {
	cache        *Cache
	snapshotPath string
	interval     time.Duration
	mu           sync.Mutex
	stopChan     chan struct{}
	running      bool
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(cache *Cache, snapshotPath string, interval time.Duration) *SnapshotManager {
	return &SnapshotManager{
		cache:        cache,
		snapshotPath: snapshotPath,
		interval:     interval,
		stopChan:     make(chan struct{}),
	}
}

// Start begins periodic snapshot creation in a background goroutine.
func (sm *SnapshotManager) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.running {
		return fmt.Errorf("snapshot manager is already running")
	}

	sm.running = true
	go sm.run()

	return nil
}

// Stop stops the periodic snapshot creation.
func (sm *SnapshotManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.running {
		return
	}

	close(sm.stopChan)
	sm.running = false
}

// run executes the periodic snapshot creation loop.
func (sm *SnapshotManager) run() {
	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := sm.cache.CreateSnapshotAndClearAOF(sm.snapshotPath); err != nil {
				fmt.Printf("Error creating snapshot: %v\n", err)
			} else {
				fmt.Printf("Snapshot created successfully at %s\n", sm.snapshotPath)
			}
		case <-sm.stopChan:
			return
		}
	}
}
