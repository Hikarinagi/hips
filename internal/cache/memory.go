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
	Data        []byte    `json:"-"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	AccessCount int64     `json:"access_count"`
	CreatedAt   time.Time `json:"created_at"`
	LastAccess  time.Time `json:"last_access"`
}

// 计算实际内存占用（包括结构体开销）
func (c *CachedImage) MemoryFootprint() int64 {
	size := int64(len(c.Data))
	size += int64(len(c.ContentType))
	size += 128 // 结构体字段和元数据开销
	return size
}

// 计算任意值的内存大小
func CalculateValueSize(value interface{}) int64 {
	switch v := value.(type) {
	case []byte:
		return int64(len(v))
	case string:
		return int64(len(v))
	case CachedImage:
		return v.MemoryFootprint()
	case *CachedImage:
		return v.MemoryFootprint()
	default:
		return 2048 // 接口开销估算
	}
}
