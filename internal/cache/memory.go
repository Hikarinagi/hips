package cache

import (
	"time"

	"github.com/patrickmn/go-cache"
)

type MemoryCache struct {
	cache *cache.Cache
}

func NewMemoryCache(defaultExpiration, cleanupInterval time.Duration) *MemoryCache {
	return &MemoryCache{
		cache: cache.New(defaultExpiration, cleanupInterval),
	}
}

func (m *MemoryCache) Get(key string) (interface{}, bool) {
	return m.cache.Get(key)
}

func (m *MemoryCache) Set(key string, value interface{}, duration time.Duration) {
	m.cache.Set(key, value, duration)
}

func (m *MemoryCache) ItemCount() int {
	return m.cache.ItemCount()
}

type CachedImage struct {
	Data        []byte
	ContentType string
}
