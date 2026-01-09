package cache

// LRU (Least Recently Used) eviction policy implementation.
//
// The cache uses LRU eviction when maxKeys is set:
// - Each key tracks its last access time in the lastAccess map
// - Get() operations update the last access time (marking the key as recently used)
// - Set() operations also update the last access time
// - When the cache is full and a new key is added, the key with the oldest
//   last access time is evicted
//
// This ensures that frequently accessed keys stay in the cache while
// rarely used keys are removed first when memory is limited.
