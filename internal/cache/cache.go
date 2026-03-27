package cache

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// entry holds a cached value and its freshness windows.
// - expiresAt: fresh until this time
// - staleUntil: serve stale until this time (stale-while-revalidate)
type entry[V any] struct {
	value      V
	expiresAt  time.Time
	staleUntil time.Time
	hasValue   bool

	// lruElement points to the key's node in the LRU list.
	lruElement *list.Element
}

// Cache provides TTL caching with:
// - stampede protection (singleflight)
// - stale-while-revalidate
// - bounded size with LRU-ish eviction
type Cache[K comparable, V any] struct {
	mutex sync.RWMutex

	// core storage
	items map[K]*entry[V]

	// expiration behavior
	ttl            time.Duration
	staleExtension time.Duration

	// size/eviction
	maxItems int
	lruList  *list.List          // Front = most recent, Back = least recent
	lruKeys  map[*list.Element]K // reverse lookup to remove keys on eviction

	// stampede protection
	singleFlight singleflight.Group
}

// Config controls cache behavior.
type Config struct {
	TTL            time.Duration
	StaleExtension time.Duration
	MaxItems       int // 0 means unbounded (not recommended)
}

// New creates a new Cache.
func New[K comparable, V any](cfg Config) *Cache[K, V] {
	return &Cache[K, V]{
		items:          make(map[K]*entry[V]),
		ttl:            cfg.TTL,
		staleExtension: cfg.StaleExtension,
		maxItems:       cfg.MaxItems,
		lruList:        list.New(),
		lruKeys:        make(map[*list.Element]K),
	}
}

// GetOrLoad returns a cached value if present and valid; otherwise loads it.
// Returns: (value, servedFromCache, error)
//
// Behavior:
// 1) If fresh, return immediately.
// 2) If stale-but-allowed, return stale immediately and refresh asynchronously.
// 3) If missing/expired beyond stale window, singleflight-load synchronously.
func (cache *Cache[K, V]) GetOrLoad(
	key K,
	now time.Time,
	loader func() (V, error),
) (V, bool, error) {
	var zeroValue V

	// Fast lookup
	cache.mutex.RLock()
	cacheEntry, hasEntry := cache.items[key]
	cache.mutex.RUnlock()

	// Fresh hit
	if hasEntry && cacheEntry.hasValue && now.Before(cacheEntry.expiresAt) {
		cache.touch(key)
		return cacheEntry.value, true, nil
	}

	// Stale hit: serve stale and refresh in background
	if hasEntry && cacheEntry.hasValue && now.Before(cacheEntry.staleUntil) {
		cache.touch(key)
		go cache.refreshAsync(key, loader)
		return cacheEntry.value, true, nil
	}

	// Cold miss / too stale: synchronous singleflight load
	singleFlightKey := fmt.Sprint(key)

	loadedAny, loadError, _ := cache.singleFlight.Do(singleFlightKey, func() (any, error) {
		loadedValue, err := loader()
		if err != nil {
			return nil, err
		}
		cache.set(key, loadedValue, time.Now())
		return loadedValue, nil
	})

	if loadError != nil {
		return zeroValue, false, loadError
	}

	return loadedAny.(V), false, nil
}

// Delete removes a key from the cache (if present).
func (cache *Cache[K, V]) Delete(key K) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cacheEntry, hasEntry := cache.items[key]
	if !hasEntry {
		return
	}

	cache.removeLRULocked(key, cacheEntry)
	delete(cache.items, key)
}

// Purge clears the entire cache.
func (cache *Cache[K, V]) Purge() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.items = make(map[K]*entry[V])
	cache.lruList.Init()
	cache.lruKeys = make(map[*list.Element]K)
}

// Len returns the number of items currently stored.
func (cache *Cache[K, V]) Len() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return len(cache.items)
}

func (cache *Cache[K, V]) refreshAsync(key K, loader func() (V, error)) {
	singleFlightKey := fmt.Sprint(key)

	_, _, _ = cache.singleFlight.Do(singleFlightKey, func() (any, error) {
		loadedValue, err := loader()
		if err != nil {
			return nil, err
		}
		cache.set(key, loadedValue, time.Now())
		return nil, nil
	})
}

func (cache *Cache[K, V]) set(key K, value V, now time.Time) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// If key already exists, update in place
	if existingEntry, hasExisting := cache.items[key]; hasExisting {
		existingEntry.value = value
		existingEntry.expiresAt = now.Add(cache.ttl)
		existingEntry.staleUntil = now.Add(cache.ttl + cache.staleExtension)
		existingEntry.hasValue = true

		cache.touchLocked(key, existingEntry)
		return
	}

	// Evict if we're bounded and full
	if cache.maxItems > 0 && len(cache.items) >= cache.maxItems {
		cache.evictOneLocked()
	}

	newEntry := &entry[V]{
		value:      value,
		expiresAt:  now.Add(cache.ttl),
		staleUntil: now.Add(cache.ttl + cache.staleExtension),
		hasValue:   true,
	}

	// Add to LRU front
	element := cache.lruList.PushFront(key)
	cache.lruKeys[element] = key
	newEntry.lruElement = element

	cache.items[key] = newEntry
}

func (cache *Cache[K, V]) touch(key K) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cacheEntry, hasEntry := cache.items[key]
	if !hasEntry {
		return
	}
	cache.touchLocked(key, cacheEntry)
}

func (cache *Cache[K, V]) touchLocked(key K, cacheEntry *entry[V]) {
	if cacheEntry.lruElement == nil {
		// Shouldn't happen for normal inserts, but keep it safe.
		element := cache.lruList.PushFront(key)
		cache.lruKeys[element] = key
		cacheEntry.lruElement = element
		return
	}

	cache.lruList.MoveToFront(cacheEntry.lruElement)
}

func (cache *Cache[K, V]) evictOneLocked() {
	leastRecent := cache.lruList.Back()
	if leastRecent == nil {
		return
	}

	evictKey, ok := cache.lruKeys[leastRecent]
	if !ok {
		// fallback safety
		cache.lruList.Remove(leastRecent)
		return
	}

	// remove from maps
	cache.lruList.Remove(leastRecent)
	delete(cache.lruKeys, leastRecent)
	delete(cache.items, evictKey)
}

func (cache *Cache[K, V]) removeLRULocked(key K, cacheEntry *entry[V]) {
	if cacheEntry.lruElement == nil {
		return
	}
	cache.lruList.Remove(cacheEntry.lruElement)
	delete(cache.lruKeys, cacheEntry.lruElement)
	cacheEntry.lruElement = nil
}