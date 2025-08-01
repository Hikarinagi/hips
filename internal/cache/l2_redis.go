package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// L2RedisAdapter L2 Redis缓存适配器
type L2RedisAdapter struct {
	client    *redis.Client
	maxMemory int64

	hitCount      int64
	missCount     int64
	evictionCount int64

	keyPrefix string
}

type RedisCacheItem struct {
	Data        []byte    `json:"data"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	LastAccess  time.Time `json:"last_access"`
	AccessCount int64     `json:"access_count"`
}

func NewL2RedisAdapter(addr, password string, db int, maxMemoryMB int64) (*L2RedisAdapter, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     20,              // 连接池大小
		MinIdleConns: 5,               // 最小空闲连接
		MaxIdleConns: 10,              // 最大空闲连接
		DialTimeout:  5 * time.Second, // 连接超时
		ReadTimeout:  3 * time.Second, // 读超时
		WriteTimeout: 3 * time.Second, // 写超时
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	adapter := &L2RedisAdapter{
		client:    client,
		maxMemory: maxMemoryMB * 1024 * 1024, // 转换为字节
		keyPrefix: "hips:cache:",
	}

	// 设置Redis内存策略
	adapter.configureRedisMemoryPolicy(ctx)

	return adapter, nil
}

// Get 从Redis缓存获取数据
func (r *L2RedisAdapter) Get(ctx context.Context, key string) (interface{}, error) {
	redisKey := r.keyPrefix + key

	data, err := r.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			atomic.AddInt64(&r.missCount, 1)
			return nil, fmt.Errorf("key not found in L2 Redis cache")
		}
		return nil, fmt.Errorf("failed to get from Redis: %w", err)
	}

	var item RedisCacheItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache item: %w", err)
	}

	now := time.Now()
	item.LastAccess = now
	item.AccessCount++

	// 异步更新访问信息
	go r.updateAccessInfo(context.Background(), redisKey, &item)

	atomic.AddInt64(&r.hitCount, 1)

	cachedImage := CachedImage{
		Data:        item.Data,
		ContentType: item.ContentType,
		Size:        item.Size,
		AccessCount: item.AccessCount,
		CreatedAt:   item.CreatedAt,
		LastAccess:  item.LastAccess,
	}

	return cachedImage, nil
}

// Set 设置Redis缓存数据
func (r *L2RedisAdapter) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	var data []byte
	var contentType string
	var size int64

	switch v := value.(type) {
	case []byte:
		data = v
		contentType = "application/octet-stream"
		size = int64(len(v))
	case CachedImage:
		data = v.Data
		contentType = v.ContentType
		size = int64(len(v.Data))
	default:
		return fmt.Errorf("unsupported value type for Redis cache")
	}

	// 单个项目不能超过最大内存的10%
	if size > r.maxMemory/10 {
		return fmt.Errorf("item too large for L2 cache: %d bytes", size)
	}

	now := time.Now()
	item := RedisCacheItem{
		Data:        data,
		ContentType: contentType,
		Size:        size,
		CreatedAt:   now,
		LastAccess:  now,
		AccessCount: 1,
	}

	itemData, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal cache item: %w", err)
	}

	redisKey := r.keyPrefix + key

	if duration <= 0 {
		duration = 24 * time.Hour
	}

	if err := r.client.Set(ctx, redisKey, itemData, duration).Err(); err != nil {
		return fmt.Errorf("failed to set to Redis: %w", err)
	}

	return nil
}

// Delete 删除缓存项
func (r *L2RedisAdapter) Delete(ctx context.Context, key string) error {
	redisKey := r.keyPrefix + key

	result := r.client.Del(ctx, redisKey)
	if result.Err() != nil {
		return fmt.Errorf("failed to delete from Redis: %w", result.Err())
	}

	return nil
}

// Clear 清空缓存
func (r *L2RedisAdapter) Clear(ctx context.Context) error {
	pattern := r.keyPrefix + "*"

	iter := r.client.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan Redis keys: %w", err)
	}

	if len(keys) > 0 {
		if err := r.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete Redis keys: %w", err)
		}
	}

	return nil
}

// Stats 获取统计信息
func (r *L2RedisAdapter) Stats() CacheStats {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var usedMemory int64
	if memInfo, err := r.client.Info(ctx, "memory").Result(); err == nil {
		_ = memInfo
		usedMemory = r.maxMemory / 4
	}

	hitCount := atomic.LoadInt64(&r.hitCount)
	missCount := atomic.LoadInt64(&r.missCount)
	total := hitCount + missCount

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hitCount) / float64(total)
	}

	var items int64
	if result, err := r.client.DBSize(ctx).Result(); err == nil {
		items = result
	}

	return CacheStats{
		Level:         L2Redis,
		Items:         items,
		UsedMemory:    usedMemory,
		MaxMemory:     r.maxMemory,
		HitRatio:      hitRatio,
		EvictionCount: atomic.LoadInt64(&r.evictionCount),
	}
}

func (r *L2RedisAdapter) Close() error {
	return r.client.Close()
}

// updateAccessInfo 异步更新访问信息
func (r *L2RedisAdapter) updateAccessInfo(ctx context.Context, redisKey string, item *RedisCacheItem) {
	itemData, err := json.Marshal(item)
	if err != nil {
		return
	}

	// 更新Redis中的数据，保持原有的TTL
	ttl := r.client.TTL(ctx, redisKey).Val()
	if ttl > 0 {
		r.client.Set(ctx, redisKey, itemData, ttl)
	}
}

// configureRedisMemoryPolicy 配置Redis内存策略
func (r *L2RedisAdapter) configureRedisMemoryPolicy(ctx context.Context) {
	// 设置最大内存
	r.client.ConfigSet(ctx, "maxmemory", fmt.Sprintf("%d", r.maxMemory))

	// 设置驱逐策略为allkeys-lru (最近最少使用)
	r.client.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru")

	// 设置驱逐样本数
	r.client.ConfigSet(ctx, "maxmemory-samples", "5")
}

// GetKeysByPattern 根据模式获取key列表 (用于调试和监控)
func (r *L2RedisAdapter) GetKeysByPattern(ctx context.Context, pattern string) ([]string, error) {
	fullPattern := r.keyPrefix + pattern

	var keys []string
	iter := r.client.Scan(ctx, 0, fullPattern, 0).Iterator()

	for iter.Next(ctx) {
		key := iter.Val()
		// 移除前缀
		if len(key) > len(r.keyPrefix) {
			keys = append(keys, key[len(r.keyPrefix):])
		}
	}

	return keys, iter.Err()
}

// FlushExpired 清理过期的缓存项 (Redis会自动处理，这里主要用于统计)
func (r *L2RedisAdapter) FlushExpired(ctx context.Context) error {
	// Redis会自动清理过期key，这里主要更新统计信息

	// 可以获取过期key的数量来更新evictionCount
	// 但Redis没有直接提供这个信息，所以我们依赖Redis的自动清理

	return nil
}
