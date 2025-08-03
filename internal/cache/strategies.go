package cache

import (
	"strings"
	"sync"
	"time"
)

// CacheStrategy 缓存策略接口
type CacheStrategy interface {
	// ShouldCache 判断是否应该缓存到该层级
	ShouldCache(level CacheLevel, key string, value interface{}, metadata *CacheMetadata) bool

	// UpdateAccess 更新访问信息
	UpdateAccess(level CacheLevel, key string, metadata *CacheMetadata)
}

// CacheMetadata 缓存元数据
type CacheMetadata struct {
	Size        int64     `json:"size"`
	AccessCount int64     `json:"access_count"`
	CreatedAt   time.Time `json:"created_at"`
	LastAccess  time.Time `json:"last_access"`
	Priority    int       `json:"priority"`
	HotScore    float64   `json:"hot_score"` // 热度分数
}

// IntelligentCacheStrategy 智能缓存策略
type IntelligentCacheStrategy struct {
	// 访问统计
	accessStats map[string]*AccessStats
	mu          sync.RWMutex

	// 配置参数
	l1SizeThreshold int64 // L1缓存大小阈值
	l2SizeThreshold int64 // L2缓存大小阈值
	hotThreshold    int64 // 热度阈值
}

// AccessStats 访问统计
type AccessStats struct {
	Count         int64       `json:"count"`
	LastAccess    time.Time   `json:"last_access"`
	FirstAccess   time.Time   `json:"first_access"`
	AccessPattern []time.Time `json:"-"` // 最近访问模式
}

// NewIntelligentCacheStrategy 创建缓存策略
func NewIntelligentCacheStrategy() *IntelligentCacheStrategy {
	return &IntelligentCacheStrategy{
		accessStats:     make(map[string]*AccessStats),
		l1SizeThreshold: 200 * 1024,      // 200KB以下进L1（小文件快速缓存）
		l2SizeThreshold: 2 * 1024 * 1024, // 2MB以下进L2（中等文件）
		hotThreshold:    10,              // 访问10次以上算热点
	}
}

// ShouldCache 智能判断缓存层级
func (s *IntelligentCacheStrategy) ShouldCache(level CacheLevel, key string, value interface{}, metadata *CacheMetadata) bool {
	s.mu.RLock()
	stats, exists := s.accessStats[key]
	s.mu.RUnlock()

	switch level {
	case L1Memory:
		// L1: 严格控制，只存储小文件且已验证是热点文件
		if metadata.Size <= s.l1SizeThreshold {
			// 必须是热点文件才能进入L1
			if exists && stats.Count >= s.hotThreshold {
				return true
			}
			// 或者是从L2提升的高频访问文件
			if metadata.AccessCount >= 3 {
				return true
			}
		}
		return false

	case L2Redis:
		// L2: 作为主要缓存层，承载大部分文件
		if metadata.Size <= s.l2SizeThreshold {
			// 新文件优先进入L2（除非是大文件）
			if !exists {
				return true
			}
			// 超过L1阈值的文件直接进入L2
			if metadata.Size > s.l1SizeThreshold {
				return true
			}
			// 有一定访问量的文件
			if exists && stats.Count >= 1 {
				return true
			}
			// 从L1驱逐的热点文件
			return metadata.AccessCount > 1
		}
		return false

	case L3Disk:
		// L3: 大文件或原图
		if metadata.Size > s.l2SizeThreshold {
			return true
		}
		// 原图优先进入L3（key包含"raw_"前缀）
		if strings.Contains(key, "raw_") {
			return true
		}
		// 从L2驱逐但仍有访问的文件
		return exists && stats.Count > 1

	default:
		return false
	}
}

func (s *IntelligentCacheStrategy) UpdateAccess(level CacheLevel, key string, metadata *CacheMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	stats, exists := s.accessStats[key]
	if !exists {
		stats = &AccessStats{
			FirstAccess:   now,
			AccessPattern: make([]time.Time, 0, 10),
		}
		s.accessStats[key] = stats
	}

	stats.Count++
	stats.LastAccess = now

	// 维护访问模式（滑动窗口）
	stats.AccessPattern = append(stats.AccessPattern, now)
	if len(stats.AccessPattern) > 10 {
		stats.AccessPattern = stats.AccessPattern[1:]
	}

	metadata.HotScore = s.calculateHotScore(stats)
}

func (s *IntelligentCacheStrategy) calculateHotScore(stats *AccessStats) float64 {
	if stats.Count == 0 {
		return 0
	}

	score := float64(stats.Count)

	timeSinceLastAccess := time.Since(stats.LastAccess).Hours()
	timeDecay := 1.0 / (1.0 + timeSinceLastAccess/24.0)

	var frequency float64 = 1.0
	if len(stats.AccessPattern) > 1 {
		totalTime := stats.AccessPattern[len(stats.AccessPattern)-1].Sub(stats.AccessPattern[0])
		if totalTime.Hours() > 0 {
			frequency = float64(len(stats.AccessPattern)) / totalTime.Hours()
		}
	}

	return score * timeDecay * (1.0 + frequency)
}
