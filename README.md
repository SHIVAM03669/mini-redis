# Mini Redis

A lightweight, in-memory key-value cache server inspired by Redis, built with Go. This project demonstrates core caching concepts including thread-safe operations, TTL (Time-To-Live) support, and automatic expiration cleanup.

## Features

- **Thread-Safe Operations**: All cache operations use `sync.RWMutex` for concurrent access
- **TTL Support**: Keys can be set with optional expiration times
- **Automatic Cleanup**: Background goroutine removes expired keys every second
- **RESTful API**: Clean HTTP endpoints with JSON support
- **Lazy Expiration**: Expired keys are also removed on access (GET operations)

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

# Run the server
go run main.go
```

The server will start on `http://localhost:8080`

### Build Executable

```bash
# Build
go build -o mini-redis

# Run
./mini-redis
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
├── main.go      # Main application code
├── go.mod       # Go module definition
└── README.md    # This file
```

## Future Enhancements

Potential improvements:
- Persistence (save to disk)
- Multiple data types (not just strings)
- Pub/Sub functionality
- Clustering support
- Metrics and monitoring
- Configuration file support

## License

This is an educational project for learning Go and caching concepts.

