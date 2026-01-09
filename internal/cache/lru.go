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
// TTL + LRU Coordination:
// - Expired keys are removed immediately when detected (in Get(), Set(), Cleanup())
// - Expired keys are NOT considered when counting keys for maxKeys limit
// - Expired keys are NOT considered when finding the LRU candidate for eviction
// - Only valid (non-expired) keys affect LRU order
// - This ensures state consistency: expired keys don't interfere with LRU eviction
//
// This ensures that frequently accessed keys stay in the cache while
// rarely used keys are removed first when memory is limited.
