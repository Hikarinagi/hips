package cache

import (
	"context"
	"time"
)

// CacheLevel 缓存层级枚举
type CacheLevel int

const (
	L1Memory CacheLevel = iota + 1 // L1: 内存缓存
	L2Redis                        // L2: Redis缓存
	L3Disk                         // L3: 磁盘缓存
	L4CDN                          // L4: CDN缓存
)

func (l CacheLevel) String() string {
	switch l {
	case L1Memory:
		return "1"
	case L2Redis:
		return "2"
	case L3Disk:
		return "3"
	case L4CDN:
		return "4"
	default:
		return "Unknown"
	}
}

// CacheHitInfo 缓存命中信息
type CacheHitInfo struct {
	Level        CacheLevel    `json:"level"`
	Hit          bool          `json:"hit"`
	RetrieveTime time.Duration `json:"retrieve_time"`
	Size         int64         `json:"size"`
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Level         CacheLevel `json:"level"`
	Items         int64      `json:"items"`
	UsedMemory    int64      `json:"used_memory"`
	MaxMemory     int64      `json:"max_memory"`
	HitRatio      float64    `json:"hit_ratio"`
	EvictionCount int64      `json:"eviction_count"`
}

// CacheService 基础缓存服务接口
type CacheService interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, duration time.Duration)
	ItemCount() int
}

// LayeredCacheService 分层缓存服务接口
type LayeredCacheService interface {
	// Get 从缓存中获取数据，返回数据和命中信息
	Get(ctx context.Context, key string) (interface{}, *CacheHitInfo, error)

	// Set 设置缓存数据到合适的层级
	Set(ctx context.Context, key string, value interface{}, duration time.Duration) error

	// GetStats 获取各层缓存统计信息
	GetStats() map[CacheLevel]CacheStats

	// Evict 手动清除缓存
	Evict(ctx context.Context, key string) error

	// Clear 清空指定层级缓存
	Clear(ctx context.Context, level CacheLevel) error

	// Close 关闭缓存服务
	Close() error
}

// CacheAdapter 单层缓存适配器接口
type CacheAdapter interface {
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}, duration time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
	Stats() CacheStats
	Close() error
}
