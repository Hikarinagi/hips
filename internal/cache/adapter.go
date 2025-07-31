package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// L1MemoryAdapter L1内存缓存适配器
type L1MemoryAdapter struct {
	data       map[string]*L1CacheItem
	lruList    *LRUList
	maxMemory  int64
	usedMemory int64

	// 统计信息
	hitCount      int64
	missCount     int64
	evictionCount int64

	mu sync.RWMutex
}

// L1CacheItem L1缓存项
type L1CacheItem struct {
	Key         string
	Value       interface{}
	Size        int64
	CreatedAt   time.Time
	LastAccess  time.Time
	AccessCount int64
	lruNode     *LRUNode
}

// LRUList LRU双向链表
type LRUList struct {
	head *LRUNode
	tail *LRUNode
}

// LRUNode LRU节点
type LRUNode struct {
	key  string
	prev *LRUNode
	next *LRUNode
}

// NewL1MemoryAdapter 创建L1内存缓存适配器
func NewL1MemoryAdapter(maxMemoryMB int64) *L1MemoryAdapter {
	adapter := &L1MemoryAdapter{
		data:      make(map[string]*L1CacheItem),
		lruList:   NewLRUList(),
		maxMemory: maxMemoryMB * 1024 * 1024, // 转换为字节
	}
	return adapter
}

// NewLRUList 创建LRU链表
func NewLRUList() *LRUList {
	head := &LRUNode{}
	tail := &LRUNode{}
	head.next = tail
	tail.prev = head

	return &LRUList{
		head: head,
		tail: tail,
	}
}

// Get 从L1缓存获取数据
func (l *L1MemoryAdapter) Get(ctx context.Context, key string) (interface{}, error) {
	l.mu.RLock()
	item, exists := l.data[key]
	l.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&l.missCount, 1)
		return nil, fmt.Errorf("key not found in L1 cache")
	}

	// 更新访问信息
	now := time.Now()
	atomic.AddInt64(&item.AccessCount, 1)
	item.LastAccess = now
	atomic.AddInt64(&l.hitCount, 1)

	// 移动到LRU链表头部
	l.mu.Lock()
	l.lruList.MoveToHead(item.lruNode)
	l.mu.Unlock()

	return item.Value, nil
}

// Set 设置L1缓存数据
func (l *L1MemoryAdapter) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	size := l.calculateSize(value)

	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否已存在
	if existingItem, exists := l.data[key]; exists {
		// 更新现有项
		l.usedMemory -= existingItem.Size
		existingItem.Value = value
		existingItem.Size = size
		existingItem.LastAccess = time.Now()
		l.usedMemory += size
		l.lruList.MoveToHead(existingItem.lruNode)
		return nil
	}

	// 检查内存限制，必要时驱逐
	for l.usedMemory+size > l.maxMemory && len(l.data) > 0 {
		if err := l.evictLRU(); err != nil {
			return fmt.Errorf("failed to evict item: %w", err)
		}
	}

	// 创建新项
	now := time.Now()
	node := &LRUNode{key: key}
	item := &L1CacheItem{
		Key:         key,
		Value:       value,
		Size:        size,
		CreatedAt:   now,
		LastAccess:  now,
		AccessCount: 1,
		lruNode:     node,
	}

	l.data[key] = item
	l.usedMemory += size
	l.lruList.AddToHead(node)

	return nil
}

// Delete 删除缓存项
func (l *L1MemoryAdapter) Delete(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	item, exists := l.data[key]
	if !exists {
		return nil // 不存在也不算错误
	}

	delete(l.data, key)
	l.usedMemory -= item.Size
	l.lruList.Remove(item.lruNode)

	return nil
}

// Clear 清空缓存
func (l *L1MemoryAdapter) Clear(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.data = make(map[string]*L1CacheItem)
	l.lruList = NewLRUList()
	l.usedMemory = 0

	return nil
}

// Stats 获取统计信息
func (l *L1MemoryAdapter) Stats() CacheStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	hitCount := atomic.LoadInt64(&l.hitCount)
	missCount := atomic.LoadInt64(&l.missCount)
	total := hitCount + missCount

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hitCount) / float64(total)
	}

	return CacheStats{
		Level:         L1Memory,
		Items:         int64(len(l.data)),
		UsedMemory:    l.usedMemory,
		MaxMemory:     l.maxMemory,
		HitRatio:      hitRatio,
		EvictionCount: atomic.LoadInt64(&l.evictionCount),
	}
}

// Close 关闭缓存
func (l *L1MemoryAdapter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.data = nil
	l.lruList = nil

	return nil
}

// evictLRU 驱逐最近最少使用的项
func (l *L1MemoryAdapter) evictLRU() error {
	if l.lruList.tail.prev == l.lruList.head {
		return fmt.Errorf("no items to evict")
	}

	// 获取尾部节点（最旧的）
	nodeToRemove := l.lruList.tail.prev
	key := nodeToRemove.key

	item, exists := l.data[key]
	if !exists {
		return fmt.Errorf("inconsistent state: node exists but item missing")
	}

	// 移除项
	delete(l.data, key)
	l.usedMemory -= item.Size
	l.lruList.Remove(nodeToRemove)
	atomic.AddInt64(&l.evictionCount, 1)

	return nil
}

// calculateSize 计算数据大小
func (l *L1MemoryAdapter) calculateSize(value interface{}) int64 {
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

// LRU链表操作方法

// AddToHead 添加节点到头部
func (l *LRUList) AddToHead(node *LRUNode) {
	node.prev = l.head
	node.next = l.head.next
	l.head.next.prev = node
	l.head.next = node
}

// Remove 移除节点
func (l *LRUList) Remove(node *LRUNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// MoveToHead 移动节点到头部
func (l *LRUList) MoveToHead(node *LRUNode) {
	l.Remove(node)
	l.AddToHead(node)
}
