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

func (l *L1MemoryAdapter) Get(ctx context.Context, key string) (interface{}, error) {
	l.mu.RLock()
	item, exists := l.data[key]
	l.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&l.missCount, 1)
		return nil, fmt.Errorf("key not found in L1 cache")
	}

	now := time.Now()
	atomic.AddInt64(&item.AccessCount, 1)
	item.LastAccess = now
	atomic.AddInt64(&l.hitCount, 1)

	l.mu.Lock()
	l.lruList.MoveToHead(item.lruNode)
	l.mu.Unlock()

	return item.Value, nil
}

func (l *L1MemoryAdapter) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	size := CalculateValueSize(value)

	l.mu.Lock()
	defer l.mu.Unlock()

	if existingItem, exists := l.data[key]; exists {
		l.usedMemory -= existingItem.Size
		existingItem.Value = value
		existingItem.Size = size
		existingItem.LastAccess = time.Now()
		l.usedMemory += size
		l.lruList.MoveToHead(existingItem.lruNode)
		return nil
	}

	// 内存不足时驱逐LRU项
	for l.usedMemory+size > l.maxMemory && len(l.data) > 0 {
		if err := l.evictLRU(); err != nil {
			return fmt.Errorf("failed to evict item: %w", err)
		}
	}

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

func (l *L1MemoryAdapter) Delete(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	item, exists := l.data[key]
	if !exists {
		return nil
	}

	delete(l.data, key)
	l.usedMemory -= item.Size
	l.lruList.Remove(item.lruNode)

	return nil
}

func (l *L1MemoryAdapter) Clear(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.data = make(map[string]*L1CacheItem)
	l.lruList = NewLRUList()
	l.usedMemory = 0

	return nil
}

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

func (l *L1MemoryAdapter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.data = nil
	l.lruList = nil

	return nil
}

func (l *L1MemoryAdapter) evictLRU() error {
	if l.lruList.tail.prev == l.lruList.head {
		return fmt.Errorf("no items to evict")
	}

	nodeToRemove := l.lruList.tail.prev
	key := nodeToRemove.key

	item, exists := l.data[key]
	if !exists {
		return fmt.Errorf("inconsistent state: node exists but item missing")
	}

	delete(l.data, key)
	l.usedMemory -= item.Size
	l.lruList.Remove(nodeToRemove)
	atomic.AddInt64(&l.evictionCount, 1)

	return nil
}

func (l *LRUList) AddToHead(node *LRUNode) {
	node.prev = l.head
	node.next = l.head.next
	l.head.next.prev = node
	l.head.next = node
}

func (l *LRUList) Remove(node *LRUNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

func (l *LRUList) MoveToHead(node *LRUNode) {
	l.Remove(node)
	l.AddToHead(node)
}
