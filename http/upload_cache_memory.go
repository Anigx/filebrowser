package fbhttp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

const uploadCacheTTL = 3 * time.Minute

var errUploadLeaseHeld = errors.New("another upload or resource mutation is in progress")

// UploadCache tracks active uploads. Every filesystem operation that can change
// an upload path must run under WithLease. This is deliberately one lease for
// the cache rather than a path-only mutex: a directory mutation overlaps every
// descendant upload and must not race a PATCH which has already checked its
// path but has not opened it yet.
type UploadCache interface {
	Register(filePath string, fileSize int64, objectID string, remove func() error)
	Complete(filePath string)
	Get(filePath string) (uploadCacheEntry, error)
	Touch(filePath string)
	InvalidatePathPrefix(path string)
	WithLease(context.Context, func() error) error
	Close()
}

type uploadCacheEntry struct {
	size     int64
	objectID string
	remove   func() error
}

// memoryUploadEntry is retained as an internal compatibility alias for cache
// tests and callers which previously inspected the in-memory value.
type memoryUploadEntry = uploadCacheEntry

type memoryUploadCache struct {
	cache *ttlcache.Cache[string, uploadCacheEntry]
	lease sync.Mutex
}

func newMemoryUploadCache() *memoryUploadCache {
	cache := ttlcache.New[string, uploadCacheEntry]()
	cache.OnEviction(func(_ context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, uploadCacheEntry]) {
		if reason == ttlcache.EvictionReasonExpired {
			fmt.Printf("deleting incomplete upload file: \"%s\"\n", item.Key())
			if remove := item.Value().remove; remove != nil {
				if err := remove(); err != nil {
					fmt.Printf("failed to delete incomplete upload file %q: %v\n", item.Key(), err)
				}
			}
		}
	})
	go cache.Start()
	return &memoryUploadCache{cache: cache}
}

func (c *memoryUploadCache) Register(filePath string, fileSize int64, objectID string, remove func() error) {
	c.cache.Set(filePath, uploadCacheEntry{size: fileSize, objectID: objectID, remove: remove}, uploadCacheTTL)
}
func (c *memoryUploadCache) Complete(filePath string) { c.cache.Delete(filePath) }
func (c *memoryUploadCache) Get(filePath string) (uploadCacheEntry, error) {
	item := c.cache.Get(filePath)
	if item == nil {
		return uploadCacheEntry{}, fmt.Errorf("no active upload found for the given path")
	}
	return item.Value(), nil
}
func (c *memoryUploadCache) Touch(filePath string) { c.cache.Touch(filePath) }
func (c *memoryUploadCache) InvalidatePathPrefix(path string) {
	prefix := strings.TrimRight(path, "/")
	for _, key := range c.cache.Keys() {
		if key == prefix || strings.HasPrefix(key, prefix+"/") {
			c.cache.Delete(key)
		}
	}
}
func (c *memoryUploadCache) WithLease(_ context.Context, fn func() error) error {
	c.lease.Lock()
	defer c.lease.Unlock()
	return fn()
}
func (c *memoryUploadCache) Close() { c.cache.Stop() }

func NewUploadCache(redisURL string) (UploadCache, error) {
	if redisURL != "" {
		return newRedisUploadCache(redisURL)
	}
	return newMemoryUploadCache(), nil
}
