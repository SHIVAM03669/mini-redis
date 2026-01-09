# Mini Redis

A lightweight, in-memory key-value cache server inspired by Redis, built with Go. This project demonstrates core caching concepts including thread-safe operations, TTL (Time-To-Live) support, and automatic expiration cleanup.

## Features

- **Thread-Safe Operations**: All cache operations use `sync.RWMutex` for concurrent access
- **TTL Support**: Keys can be set with optional expiration times
- **Automatic Cleanup**: Background goroutine removes expired keys every second
- **RESTful API**: Clean HTTP endpoints with JSON support
- **Lazy Expiration**: Expired keys are also removed on access (GET operations)
- **Append-Only File (AOF)**: Every write operation is logged to disk for crash recovery
- **Snapshot (RDB-style)**: Periodic snapshots prevent infinite AOF growth
- **LRU Eviction**: Least Recently Used keys are evicted when memory limit is reached
- **Memory Limits**: Configurable maximum number of keys to prevent unlimited memory usage
- **Durability**: Data survives server crashes and restarts

## Architecture

### Core Components

#### 1. Cache Structure
The `Cache` struct maintains two maps:
- **`data`**: Stores the actual key-value pairs (`map[string]string`)
- **`expires`**: Tracks expiration times for each key (`map[string]time.Time`)
- **`mu`**: Read-write mutex (`sync.RWMutex`) for thread-safe concurrent access

#### 2. Thread Safety
- **Write Operations** (`Set`, `Del`): Use `Lock()` for exclusive access
- **Read Operations** (`Get`): Uses `Lock()` (not `RLock()`) because it may delete expired keys
- All operations are protected by mutex to prevent race conditions

#### 3. Expiration Mechanism
- **TTL Storage**: When a key is set with TTL > 0, expiration time is calculated as `time.Now().Add(ttl)`
- **No Expiry**: Keys with TTL = 0 have `time.Time{}` (zero time) stored, which never expires
- **Expiration Check**: Uses `time.Time.IsZero()` to distinguish between expired and non-expiring keys

#### 4. Cleanup Strategy
Two mechanisms ensure expired keys are removed:

1. **Proactive Cleanup**: Background goroutine runs every second, scanning all keys and removing expired ones
2. **Lazy Cleanup**: `Get` operations check expiration and delete expired keys on-the-fly

This dual approach ensures:
- Expired keys don't accumulate in memory
- Expired keys are removed even if never accessed again
- Immediate cleanup when accessing expired keys

#### 5. HTTP API Layer
- **Request Parsing**: JSON bodies for POST endpoints, query parameters for GET
- **Validation**: Input validation with appropriate HTTP status codes
- **Error Handling**: Clear error messages for invalid requests

### Data Flow

```
Client Request
    ↓
HTTP Handler (setHandler/getHandler/delHandler)
    ↓
Request Validation & Parsing
    ↓
Cache Operation (Set/Get/Del)
    ↓
Mutex Lock → Operation → Mutex Unlock
    ↓
Response to Client

Background Cleaner (runs every 1 second)
    ↓
cache.cleanup()
    ↓
Scan expires map → Delete expired keys
```

## API Endpoints

### Health Check
```bash
GET /
```
Returns server status.

### Set Key
```bash
POST /set
```
Stores a key-value pair in the cache.

**Request Body (JSON):**
```json
{
  "key": "mykey",
  "value": "myvalue",
  "ttl": 60
}
```
- `key` (required): The cache key
- `value` (required): The value to store
- `ttl` (optional): Time-to-live in seconds. If omitted, key never expires.

**Response:**
```
OK key set
```

### Get Key
```bash
GET /get?key=<key>
```
Retrieves a value by key.

**Query Parameters:**
- `key` (required): The cache key to retrieve

**Response:**
- Success: Returns the value
- Not Found: `404 Key not found`

### Delete Key
```bash
POST /del
```
Deletes a key from the cache.

**Request Body (JSON):**
```json
{
  "key": "mykey"
}
```

**Response:**
```
OK Key Deleted
```

## Usage Examples

### Using curl

#### 1. Set a key without expiration
```bash
curl -X POST http://localhost:8080/set \
  -H "Content-Type: application/json" \
  -d '{"key": "username", "value": "alice"}'
```

#### 2. Set a key with 60-second TTL
```bash
curl -X POST http://localhost:8080/set \
  -H "Content-Type: application/json" \
  -d '{"key": "session", "value": "abc123", "ttl": 60}'
```

#### 3. Get a key
```bash
curl http://localhost:8080/get?key=username
```

#### 4. Get a key (expired example)
```bash
# Wait 60+ seconds after setting with TTL, then:
curl http://localhost:8080/get?key=session
# Returns: 404 Key not found
```

#### 5. Delete a key
```bash
curl -X POST http://localhost:8080/del \
  -H "Content-Type: application/json" \
  -d '{"key": "username"}'
```

#### 6. Health check
```bash
curl http://localhost:8080/
```

### Complete Workflow Example

```bash
# 1. Set a key with 30-second TTL
curl -X POST http://localhost:8080/set \
  -H "Content-Type: application/json" \
  -d '{"key": "temp", "value": "data", "ttl": 30}'

# 2. Immediately retrieve it (should work)
curl http://localhost:8080/get?key=temp
# Output: data

# 3. Wait 30+ seconds, then try again (will be expired)
curl http://localhost:8080/get?key=temp
# Output: Key not found
```

## Running the Server

### Prerequisites
- Go 1.21 or later

### Installation & Run

```bash
# Clone or navigate to the project directory
cd mini-redis

# Run the server (default: unlimited keys)
go run ./cmd/server

# Run with custom paths and memory limit
go run ./cmd/server [aofPath] [snapshotPath] [maxKeys]

# Example: Limit to 1000 keys
go run ./cmd/server data/appendonly.aof data/dump.rdb 1000

# Using environment variable for maxKeys
MAX_KEYS=500 go run ./cmd/server
```

The server will start on `http://localhost:8080`

### Build Executable

```bash
# Build
go build -o mini-redis.exe ./cmd/server

# Run
./mini-redis.exe

# Run with memory limit
./mini-redis.exe data/appendonly.aof data/dump.rdb 1000
```

## Implementation Details

### Concurrency Model
- Uses `sync.RWMutex` for fine-grained locking
- Write operations (Set, Del) acquire exclusive lock
- Read operations (Get) acquire lock (not read lock) because they may modify state (delete expired keys)
- Background cleaner acquires exclusive lock during cleanup

### Memory Management
- Expired keys are automatically removed from both `data` and `expires` maps
- No memory leaks: all keys are properly cleaned up
- Background goroutine prevents unbounded growth of expired entries

### Error Handling
- Invalid JSON: Returns `400 Bad Request`
- Missing required fields: Returns `400 Bad Request`
- Invalid method: Returns `405 Method Not Allowed`
- Key not found: Returns `404 Not Found`

## Project Structure

```
mini-redis/
├── cmd/
│   └── server/
│       └── main.go          # Main server application
├── internal/
│   └── cache/
│       ├── cache.go         # Core cache implementation
│       ├── aof.go            # Append-Only File persistence
│       ├── snapshot.go      # Snapshot (RDB-style) persistence
│       └── lru.go           # LRU eviction policy documentation
├── data/
│   ├── appendonly.aof       # AOF file (created at runtime)
│   └── dump.rdb             # Snapshot file (created at runtime)
├── go.mod                    # Go module definition
└── README.md                 # This file
```

## Failure Testing

This section demonstrates how to test data durability and prove that the cache survives server crashes.

### Durability Test: Crash Recovery

The following test proves that data persists across server restarts:

#### Step 1: Start the Server

```bash
# Start the server
go run ./cmd/server

# Or use the built executable
./mini-redis.exe
```

You should see:
```
Cache initialized with AOF: data/appendonly.aof, Snapshot: data/dump.rdb, MaxKeys: unlimited
Snapshot manager started (interval: 5m0s)
Server running on http://localhost:8080
```

#### Step 2: Set Multiple Keys

**Option A: Use the provided test script (Recommended)**

```bash
# PowerShell (Windows)
.\test-durability.ps1

# Bash (Linux/Mac)
chmod +x test-durability.sh
./test-durability.sh
```

**Option B: Manual commands**

```bash
# Set 10 keys with various values
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key1", "value": "value1"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key2", "value": "value2"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key3", "value": "value3"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key4", "value": "value4"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key5", "value": "value5"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key6", "value": "value6"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key7", "value": "value7"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key8", "value": "value8"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key9", "value": "value9"}'
curl -X POST http://localhost:8080/set -H "Content-Type: application/json" -d '{"key": "key10", "value": "value10"}'
```

Or use PowerShell inline:

```powershell
# PowerShell script to set 10 keys
1..10 | ForEach-Object {
    $body = @{key="key$_"; value="value$_"} | ConvertTo-Json
    Invoke-RestMethod -Uri "http://localhost:8080/set" -Method Post -Body $body -ContentType "application/json"
    Write-Host "Set key$_"
}
```

#### Step 3: Verify Keys Exist

```bash
# Verify all keys are stored
curl http://localhost:8080/get?key=key1
curl http://localhost:8080/get?key=key5
curl http://localhost:8080/get?key=key10
```

All should return their respective values.

#### Step 4: Kill the Server

Press `CTRL+C` in the terminal where the server is running, or forcefully terminate the process:

```bash
# Find and kill the process (if needed)
# Windows PowerShell:
Get-Process | Where-Object {$_.ProcessName -like "*go*"} | Stop-Process -Force

# Or simply press CTRL+C in the server terminal
```

The server will gracefully shut down and flush all pending writes to disk.

#### Step 5: Restart the Server

```bash
# Start the server again
go run ./cmd/server
```

You should see:
```
Loaded snapshot from data/dump.rdb
Cache initialized with AOF: data/appendonly.aof, Snapshot: data/dump.rdb, MaxKeys: unlimited
Snapshot manager started (interval: 5m0s)
Server running on http://localhost:8080
```

Notice: `Loaded snapshot from data/dump.rdb` indicates data was restored from disk.

#### Step 6: Verify Keys Still Exist

**Option A: Use the test script again**

```bash
# Run the same test script - it will verify all keys exist
.\test-durability.ps1    # PowerShell
./test-durability.sh     # Bash
```

**Option B: Manual verification**

```bash
# Check that all keys are still present after restart
curl http://localhost:8080/get?key=key1
curl http://localhost:8080/get?key=key2
curl http://localhost:8080/get?key=key3
curl http://localhost:8080/get?key=key4
curl http://localhost:8080/get?key=key5
curl http://localhost:8080/get?key=key6
curl http://localhost:8080/get?key=key7
curl http://localhost:8080/get?key=key8
curl http://localhost:8080/get?key=key9
curl http://localhost:8080/get?key=key10
```

**Expected Result**: All keys should return their values, proving that data survived the crash and restart.

### Quick Test Summary

1. **Start server**: `go run ./cmd/server`
2. **Run test script**: `.\test-durability.ps1` (or `./test-durability.sh`)
3. **Kill server**: Press `CTRL+C`
4. **Restart server**: `go run ./cmd/server`
5. **Run test script again**: `.\test-durability.ps1` (or `./test-durability.sh`)

If all keys are found in step 5, durability is proven! ✓

### How It Works

1. **AOF (Append-Only File)**: Every `SET` and `DEL` operation is immediately written to `data/appendonly.aof`
2. **Snapshot**: Every 5 minutes, a full snapshot is saved to `data/dump.rdb` and the AOF is cleared
3. **Recovery**: On startup, the server:
   - Loads the snapshot (if exists) to restore the base state
   - Replays the AOF file to apply any operations after the snapshot
   - Result: Complete data recovery

### Testing with Memory Limits

You can also test durability with memory limits:

```bash
# Start server with max 5 keys
go run ./cmd/server data/appendonly.aof data/dump.rdb 5

# Set 10 keys (5 will be evicted due to LRU)
# ... set keys ...

# Kill and restart
# Only the last 5 keys should be present (LRU eviction)
```

### Testing Snapshot Creation

To test snapshot creation without waiting 5 minutes:

1. Modify `cmd/server/main.go` to use a shorter interval (e.g., 30 seconds) for testing
2. Set some keys
3. Wait for snapshot creation (check `data/dump.rdb` file modification time)
4. Verify AOF is cleared (check `data/appendonly.aof` is empty or small)
5. Restart server and verify data recovery

## Future Enhancements

Potential improvements:
- Multiple data types (not just strings)
- Pub/Sub functionality
- Clustering support
- Metrics and monitoring
- Configuration file support
- AOF rewrite optimization

## License

This is an educational project for learning Go and caching concepts.

