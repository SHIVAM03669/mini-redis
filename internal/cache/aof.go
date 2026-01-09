package cache

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// AOF represents the Append-Only File persistence layer.
// Every write operation (SET, DEL) is logged to disk for crash recovery.
type AOF struct {
	file     *os.File
	writer   *bufio.Writer
	filePath string
	mu       sync.Mutex
	cache    *Cache
	enabled  bool
}

// AOFCommand represents a command logged in the AOF file.
type AOFCommand struct {
	Op    string `json:"op"`    // Operation: "SET" or "DEL"
	Key   string `json:"key"`   // Cache key
	Value string `json:"value"` // Value (for SET operations)
	TTL   int    `json:"ttl"`   // TTL in seconds (for SET operations, 0 means no expiry)
}

// NewAOF creates and initializes a new AOF instance.
// If the file exists, it will be opened in append mode.
// If it doesn't exist, it will be created.
func NewAOF(filePath string, cache *Cache) (*AOF, error) {
	// Open file in append mode, create if it doesn't exist
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open AOF file: %w", err)
	}

	aof := &AOF{
		file:     file,
		writer:   bufio.NewWriter(file),
		filePath: filePath,
		cache:    cache,
		enabled:  true,
	}

	return aof, nil
}

// LogSet logs a SET operation to the AOF file.
func (a *AOF) LogSet(key, value string, ttl time.Duration) {
	if !a.enabled {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	ttlSeconds := 0
	if ttl > 0 {
		ttlSeconds = int(ttl.Seconds())
	}

	cmd := AOFCommand{
		Op:    "SET",
		Key:   key,
		Value: value,
		TTL:   ttlSeconds,
	}

	if err := a.writeCommand(cmd); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("AOF write error: %v\n", err)
	}
}

// LogDel logs a DEL operation to the AOF file.
func (a *AOF) LogDel(key string) {
	if !a.enabled {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	cmd := AOFCommand{
		Op:  "DEL",
		Key: key,
	}

	if err := a.writeCommand(cmd); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("AOF write error: %v\n", err)
	}
}

// writeCommand writes a command to the AOF file in JSON format, one per line.
func (a *AOF) writeCommand(cmd AOFCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Write JSON line followed by newline
	if _, err := a.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write to AOF: %w", err)
	}

	if err := a.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("failed to write newline to AOF: %w", err)
	}

	// Flush to ensure data is written to disk immediately
	if err := a.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush AOF: %w", err)
	}

	// Sync to ensure data is persisted to disk
	if err := a.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync AOF: %w", err)
	}

	return nil
}

// Replay reads the AOF file and replays all commands to restore the cache state.
// This is called on startup to recover data from disk.
func (a *AOF) Replay() error {
	// Temporarily disable logging during replay to avoid infinite loops
	a.enabled = false
	defer func() {
		a.enabled = true
	}()

	// Close the write file handle
	if a.file != nil {
		a.writer.Flush()
		a.file.Close()
		a.file = nil
	}

	// Open file for reading
	file, err := os.Open(a.filePath)
	if err != nil {
		// If file doesn't exist or can't be opened, that's okay (first run)
		if os.IsNotExist(err) {
			// Reopen for writing
			return a.reopenForWriting()
		}
		return fmt.Errorf("failed to open AOF file for replay: %w", err)
	}
	defer file.Close()

	// Read and replay commands
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue // Skip empty lines
		}

		var cmd AOFCommand
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			fmt.Printf("Warning: Failed to parse AOF line %d: %v\n", lineNum, err)
			continue
		}

		// Replay the command
		switch cmd.Op {
		case "SET":
			ttl := time.Duration(cmd.TTL) * time.Second
			a.cache.setInternal(cmd.Key, cmd.Value, ttl)
		case "DEL":
			a.cache.delInternal(cmd.Key)
		default:
			fmt.Printf("Warning: Unknown AOF operation '%s' on line %d\n", cmd.Op, lineNum)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading AOF file: %w", err)
	}

	// Reopen file for writing
	return a.reopenForWriting()
}

// reopenForWriting reopens the AOF file in append mode for writing.
func (a *AOF) reopenForWriting() error {
	file, err := os.OpenFile(a.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen AOF file for writing: %w", err)
	}

	a.file = file
	a.writer = bufio.NewWriter(file)
	return nil
}

// Close gracefully closes the AOF file.
func (a *AOF) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.writer != nil {
		if err := a.writer.Flush(); err != nil {
			return err
		}
	}

	if a.file != nil {
		return a.file.Close()
	}

	return nil
}
