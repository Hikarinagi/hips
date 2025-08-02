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

	// GetEvictionCandidate 获取驱逐候选项
	GetEvictionCandidate(level CacheLevel, currentSize int64, maxSize int64) []string

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
		// L1: 小文件 + 高频访问
		if metadata.Size <= s.l1SizeThreshold {
			if exists && stats.Count >= s.hotThreshold {
				return true
			}
			// 新文件默认先进L1尝试
			return true
		}
		return false

	case L2Redis:
		// L2: 中等大小文件 + 中频访问
		if metadata.Size <= s.l2SizeThreshold {
			// 超过L1阈值的文件优先进入L2
			if metadata.Size > s.l1SizeThreshold {
				return true
			}
			// 高频访问文件
			if exists && stats.Count >= 3 {
				return true
			}
			// 从L1驱逐的热点文件
			return metadata.AccessCount > 2
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

// UpdateAccess 更新访问统计
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

	// 维护最近访问模式（滑动窗口）
	stats.AccessPattern = append(stats.AccessPattern, now)
	if len(stats.AccessPattern) > 10 {
		stats.AccessPattern = stats.AccessPattern[1:]
	}

	// 计算热度分数
	metadata.HotScore = s.calculateHotScore(stats)
}

// GetEvictionCandidate 获取驱逐候选
func (s *IntelligentCacheStrategy) GetEvictionCandidate(level CacheLevel, currentSize int64, maxSize int64) []string {
	if currentSize <= maxSize {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 根据不同层级使用不同的驱逐策略
	switch level {
	case L1Memory:
		return s.getLRUCandidates() // L1使用LRU
	case L2Redis:
		return s.getLFUCandidates() // L2使用LFU
	case L3Disk:
		return s.getSizeCandidates() // L3优先驱逐大文件
	default:
		return s.getLRUCandidates()
	}
}

// calculateHotScore 计算热度分数
func (s *IntelligentCacheStrategy) calculateHotScore(stats *AccessStats) float64 {
	if stats.Count == 0 {
		return 0
	}

	// 基础分数：访问次数
	score := float64(stats.Count)

	// 时间衰减：最近访问的权重更高
	timeSinceLastAccess := time.Since(stats.LastAccess).Hours()
	timeDecay := 1.0 / (1.0 + timeSinceLastAccess/24.0) // 按天衰减

	// 访问频率：计算访问模式
	var frequency float64 = 1.0
	if len(stats.AccessPattern) > 1 {
		totalTime := stats.AccessPattern[len(stats.AccessPattern)-1].Sub(stats.AccessPattern[0])
		if totalTime.Hours() > 0 {
			frequency = float64(len(stats.AccessPattern)) / totalTime.Hours()
		}
	}

	return score * timeDecay * (1.0 + frequency)
}

// getLRUCandidates 获取LRU候选
func (s *IntelligentCacheStrategy) getLRUCandidates() []string {
	type candidate struct {
		key        string
		lastAccess time.Time
	}

	var candidates []candidate
	for key, stats := range s.accessStats {
		candidates = append(candidates, candidate{
			key:        key,
			lastAccess: stats.LastAccess,
		})
	}

	// 按最后访问时间排序
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].lastAccess.After(candidates[j].lastAccess) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// 返回最旧的条目
	result := make([]string, 0, len(candidates)/2)
	for i := 0; i < len(candidates)/2 && i < len(candidates); i++ {
		result = append(result, candidates[i].key)
	}

	return result
}

// getLFUCandidates 获取LFU候选
func (s *IntelligentCacheStrategy) getLFUCandidates() []string {
	type candidate struct {
		key   string
		count int64
	}

	var candidates []candidate
	for key, stats := range s.accessStats {
		candidates = append(candidates, candidate{
			key:   key,
			count: stats.Count,
		})
	}

	// 按访问次数排序
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].count > candidates[j].count {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// 返回访问次数最少的条目
	result := make([]string, 0, len(candidates)/2)
	for i := 0; i < len(candidates)/2 && i < len(candidates); i++ {
		result = append(result, candidates[i].key)
	}

	return result
}

// getSizeCandidates 获取按大小驱逐的候选
func (s *IntelligentCacheStrategy) getSizeCandidates() []string {
	// 磁盘缓存优先驱逐老旧的大文件
	return s.getLRUCandidates()
}
