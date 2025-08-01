package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// MultiLevelCache 多层缓存管理器
type MultiLevelCache struct {
	// 缓存层级
	l1Memory CacheAdapter
	l2Redis  CacheAdapter
	l3Disk   CacheAdapter

	// 缓存策略
	strategy CacheStrategy

	// 配置
	config *MultiCacheConfig

	// 统计和监控
	stats map[CacheLevel]*LevelStats
	mu    sync.RWMutex
}

// MultiCacheConfig 多层缓存配置
type MultiCacheConfig struct {
	L1MaxMemoryMB int64 `json:"l1_max_memory_mb"`
	L2MaxMemoryMB int64 `json:"l2_max_memory_mb"`
	L3MaxDiskGB   int64 `json:"l3_max_disk_gb"`

	L1Enabled bool `json:"l1_enabled"`
	L2Enabled bool `json:"l2_enabled"`
	L3Enabled bool `json:"l3_enabled"`

	RedisAddr     string `json:"redis_addr"`
	RedisPassword string `json:"redis_password"`
	RedisDB       int    `json:"redis_db"`

	DiskCacheDir   string `json:"disk_cache_dir"`
	L3UseOptimized bool   `json:"l3_use_optimized"` // 是否使用优化的BoltDB索引

	PromoteThreshold int64         `json:"promote_threshold"`
	DemoteThreshold  int64         `json:"demote_threshold"`
	SyncInterval     time.Duration `json:"sync_interval"`
}

// LevelStats 层级统计
type LevelStats struct {
	Gets         int64     `json:"gets"`
	Sets         int64     `json:"sets"`
	Hits         int64     `json:"hits"`
	Misses       int64     `json:"misses"`
	Promotions   int64     `json:"promotions"` // 从下级提升的次数
	Demotions    int64     `json:"demotions"`  // 降级到下级的次数
	LastSyncTime time.Time `json:"last_sync_time"`
}

// NewMultiLevelCache 创建多层缓存管理器
func NewMultiLevelCache(config *MultiCacheConfig) (*MultiLevelCache, error) {
	cache := &MultiLevelCache{
		config:   config,
		strategy: NewIntelligentCacheStrategy(),
		stats:    make(map[CacheLevel]*LevelStats),
	}

	// 初始化统计
	cache.stats[L1Memory] = &LevelStats{}
	cache.stats[L2Redis] = &LevelStats{}
	cache.stats[L3Disk] = &LevelStats{}

	// 初始化L1内存缓存
	if config.L1Enabled {
		cache.l1Memory = NewL1MemoryAdapter(config.L1MaxMemoryMB)
	}

	// 初始化L2 Redis缓存
	if config.L2Enabled {
		l2Cache, err := NewL2RedisAdapter(config.RedisAddr, config.RedisPassword, config.RedisDB, config.L2MaxMemoryMB)
		if err != nil {
			return nil, fmt.Errorf("failed to create L2 Redis cache: %w", err)
		}
		cache.l2Redis = l2Cache
	}

	// 初始化L3磁盘缓存
	if config.L3Enabled {
		var l3Cache CacheAdapter
		var err error

		if config.L3UseOptimized {
			// 使用优化的BoltDB索引实现
			l3Cache, err = NewL3DiskAdapterOptimized(config.DiskCacheDir, config.L3MaxDiskGB)
			if err != nil {
				return nil, fmt.Errorf("failed to create optimized L3 disk cache: %w", err)
			}
			log.Printf("L3 disk cache initialized with BoltDB optimization (max: %dGB)", config.L3MaxDiskGB)
		} else {
			// 使用原始的JSON索引实现（向后兼容）
			l3Cache, err = NewL3DiskAdapter(config.DiskCacheDir, config.L3MaxDiskGB)
			if err != nil {
				return nil, fmt.Errorf("failed to create L3 disk cache: %w", err)
			}
			log.Printf("L3 disk cache initialized with JSON index (max: %dGB)", config.L3MaxDiskGB)
		}

		cache.l3Disk = l3Cache
	}

	// 启动后台同步任务
	if config.SyncInterval > 0 {
		go cache.backgroundSync(config.SyncInterval)
	}

	return cache, nil
}

// Get 从多层缓存获取数据
func (m *MultiLevelCache) Get(ctx context.Context, key string) (interface{}, *CacheHitInfo, error) {
	start := time.Now()

	// 依次检查各个层级
	levels := []struct {
		adapter CacheAdapter
		level   CacheLevel
		enabled bool
	}{
		{m.l1Memory, L1Memory, m.config.L1Enabled},
		{m.l2Redis, L2Redis, m.config.L2Enabled},
		{m.l3Disk, L3Disk, m.config.L3Enabled},
	}

	for _, l := range levels {
		if !l.enabled || l.adapter == nil {
			continue
		}

		// 更新统计
		m.mu.Lock()
		m.stats[l.level].Gets++
		m.mu.Unlock()

		// 尝试获取数据
		value, err := l.adapter.Get(ctx, key)
		if err == nil {
			// 命中！
			retrieveTime := time.Since(start)

			// 更新统计
			m.mu.Lock()
			m.stats[l.level].Hits++
			m.mu.Unlock()

			// 更新访问信息
			size := m.calculateSize(value)
			metadata := &CacheMetadata{
				Size:        size,
				AccessCount: 1,
				CreatedAt:   time.Now(),
				LastAccess:  time.Now(),
			}
			m.strategy.UpdateAccess(l.level, key, metadata)

			// 异步提升到更高层级
			go m.promoteToHigherLevels(ctx, key, value, l.level)

			hitInfo := &CacheHitInfo{
				Level:        l.level,
				Hit:          true,
				RetrieveTime: retrieveTime,
				Size:         size,
			}

			return value, hitInfo, nil
		}

		// 未命中
		m.mu.Lock()
		m.stats[l.level].Misses++
		m.mu.Unlock()
	}

	// 所有层级都未命中
	hitInfo := &CacheHitInfo{
		Level:        L4CDN, // 表示需要从源获取
		Hit:          false,
		RetrieveTime: time.Since(start),
		Size:         0,
	}

	return nil, hitInfo, fmt.Errorf("cache miss on all levels")
}

// Set 设置多层缓存数据
func (m *MultiLevelCache) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	size := m.calculateSize(value)
	metadata := &CacheMetadata{
		Size:        size,
		AccessCount: 1,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
	}

	// 智能决定缓存层级
	levels := []struct {
		adapter CacheAdapter
		level   CacheLevel
		enabled bool
	}{
		{m.l1Memory, L1Memory, m.config.L1Enabled},
		{m.l2Redis, L2Redis, m.config.L2Enabled},
		{m.l3Disk, L3Disk, m.config.L3Enabled},
	}

	var errors []error
	cacheSet := false

	for _, l := range levels {
		if !l.enabled || l.adapter == nil {
			continue
		}

		// 使用策略判断是否应该缓存到这个层级
		if m.strategy.ShouldCache(l.level, key, value, metadata) {
			err := l.adapter.Set(ctx, key, value, duration)
			if err != nil {
				errors = append(errors, fmt.Errorf("L%d set failed: %w", int(l.level), err))
				log.Printf("Failed to set cache on level %s: %v", l.level.String(), err)
			} else {
				// 更新统计
				m.mu.Lock()
				m.stats[l.level].Sets++
				m.mu.Unlock()

				cacheSet = true
				log.Printf("Cached key %s on level %s (size: %d bytes)", key, l.level.String(), size)
			}
		}
	}

	if !cacheSet && len(errors) > 0 {
		return fmt.Errorf("failed to cache on any level: %v", errors)
	}

	return nil
}

// GetStats 获取各层缓存统计信息
func (m *MultiLevelCache) GetStats() map[CacheLevel]CacheStats {
	result := make(map[CacheLevel]CacheStats)

	adapters := map[CacheLevel]CacheAdapter{
		L1Memory: m.l1Memory,
		L2Redis:  m.l2Redis,
		L3Disk:   m.l3Disk,
	}

	for level, adapter := range adapters {
		if adapter != nil {
			stats := adapter.Stats()

			// 添加我们自己的统计信息
			m.mu.RLock()
			if levelStats, exists := m.stats[level]; exists {
				// 可以扩展统计信息
				_ = levelStats
			}
			m.mu.RUnlock()

			result[level] = stats
		}
	}

	return result
}

// Evict 手动清除缓存
func (m *MultiLevelCache) Evict(ctx context.Context, key string) error {
	var errors []error

	adapters := []CacheAdapter{m.l1Memory, m.l2Redis, m.l3Disk}
	for _, adapter := range adapters {
		if adapter != nil {
			if err := adapter.Delete(ctx, key); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("eviction errors: %v", errors)
	}

	return nil
}

// Clear 清空指定层级缓存
func (m *MultiLevelCache) Clear(ctx context.Context, level CacheLevel) error {
	var adapter CacheAdapter

	switch level {
	case L1Memory:
		adapter = m.l1Memory
	case L2Redis:
		adapter = m.l2Redis
	case L3Disk:
		adapter = m.l3Disk
	default:
		return fmt.Errorf("unsupported cache level: %v", level)
	}

	if adapter == nil {
		return fmt.Errorf("cache level %v is not enabled", level)
	}

	return adapter.Clear(ctx)
}

// Close 关闭缓存服务
func (m *MultiLevelCache) Close() error {
	var errors []error

	adapters := []CacheAdapter{m.l1Memory, m.l2Redis, m.l3Disk}
	for _, adapter := range adapters {
		if adapter != nil {
			if err := adapter.Close(); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("close errors: %v", errors)
	}

	return nil
}

// promoteToHigherLevels 提升数据到更高层级
func (m *MultiLevelCache) promoteToHigherLevels(ctx context.Context, key string, value interface{}, currentLevel CacheLevel) {
	size := m.calculateSize(value)
	metadata := &CacheMetadata{
		Size:        size,
		AccessCount: 2, // 已经被访问过一次
		LastAccess:  time.Now(),
	}

	// 根据当前层级决定提升策略
	switch currentLevel {
	case L3Disk:
		// 从L3提升到L2
		if m.config.L2Enabled && m.l2Redis != nil && m.strategy.ShouldCache(L2Redis, key, value, metadata) {
			if err := m.l2Redis.Set(ctx, key, value, time.Hour); err == nil {
				m.mu.Lock()
				m.stats[L2Redis].Promotions++
				m.mu.Unlock()
				log.Printf("Promoted key %s from L3 to L2", key)
			}
		}

		// 从L3提升到L1
		if m.config.L1Enabled && m.l1Memory != nil && m.strategy.ShouldCache(L1Memory, key, value, metadata) {
			if err := m.l1Memory.Set(ctx, key, value, time.Hour); err == nil {
				m.mu.Lock()
				m.stats[L1Memory].Promotions++
				m.mu.Unlock()
				log.Printf("Promoted key %s from L3 to L1", key)
			}
		}

	case L2Redis:
		// 从L2提升到L1
		if m.config.L1Enabled && m.l1Memory != nil && m.strategy.ShouldCache(L1Memory, key, value, metadata) {
			if err := m.l1Memory.Set(ctx, key, value, time.Hour); err == nil {
				m.mu.Lock()
				m.stats[L1Memory].Promotions++
				m.mu.Unlock()
				log.Printf("Promoted key %s from L2 to L1", key)
			}
		}
	}
}

// backgroundSync 后台同步任务
func (m *MultiLevelCache) backgroundSync(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		m.performSync()
	}
}

// performSync 执行同步操作
func (m *MultiLevelCache) performSync() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for level, stats := range m.stats {
		stats.LastSyncTime = now

		log.Printf("Cache Level %s Stats - Gets: %d, Hits: %d, Hit Ratio: %.2f%%, Promotions: %d",
			level.String(),
			stats.Gets,
			stats.Hits,
			float64(stats.Hits)/float64(stats.Gets)*100,
			stats.Promotions)
	}
}

// calculateSize 计算数据大小
func (m *MultiLevelCache) calculateSize(value interface{}) int64 {
	switch v := value.(type) {
	case []byte:
		return int64(len(v))
	case string:
		return int64(len(v))
	case CachedImage:
		return int64(len(v.Data))
	default:
		// 粗略估算
		return 1024
	}
}
