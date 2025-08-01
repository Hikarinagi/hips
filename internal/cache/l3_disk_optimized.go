package cache

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// L3DiskAdapterOptimized 优化的L3磁盘缓存适配器
type L3DiskAdapterOptimized struct {
	baseDir     string
	maxSize     int64
	currentSize int64

	// BoltDB索引
	db *bbolt.DB

	// 内存索引缓存（可选，用于快速查找）
	metaCache map[string]*DiskCacheMetadata
	mu        sync.RWMutex

	// 统计信息
	hitCount      int64
	missCount     int64
	evictionCount int64

	// 后台清理
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

var (
	metadataBucket = []byte("metadata")
)

// NewL3DiskAdapterOptimized 创建优化的L3磁盘缓存适配器
func NewL3DiskAdapterOptimized(baseDir string, maxSizeGB int64) (*L3DiskAdapterOptimized, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// 打开BoltDB数据库
	dbPath := filepath.Join(baseDir, "index.db")
	db, err := bbolt.Open(dbPath, 0644, &bbolt.Options{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open index database: %w", err)
	}

	// 创建bucket
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(metadataBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	adapter := &L3DiskAdapterOptimized{
		baseDir:     baseDir,
		maxSize:     maxSizeGB * 1024 * 1024 * 1024,
		db:          db,
		metaCache:   make(map[string]*DiskCacheMetadata),
		stopCleanup: make(chan struct{}),
	}

	// 加载元数据并计算当前大小
	if err := adapter.loadMetadataFromDB(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// 启动后台清理任务
	adapter.cleanupTicker = time.NewTicker(30 * time.Minute)
	go adapter.backgroundCleanup()

	return adapter, nil
}

// Get 从磁盘缓存获取数据
func (d *L3DiskAdapterOptimized) Get(ctx context.Context, key string) (interface{}, error) {
	// 先检查内存缓存
	d.mu.RLock()
	meta, exists := d.metaCache[key]
	d.mu.RUnlock()

	if !exists {
		// 从数据库查找
		var found bool
		err := d.db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket(metadataBucket)
			data := b.Get([]byte(key))
			if data != nil {
				meta = &DiskCacheMetadata{}
				if err := json.Unmarshal(data, meta); err == nil {
					found = true
					// 更新内存缓存
					d.mu.Lock()
					d.metaCache[key] = meta
					d.mu.Unlock()
				}
			}
			return nil
		})

		if err != nil || !found {
			atomic.AddInt64(&d.missCount, 1)
			return nil, fmt.Errorf("key not found in L3 cache")
		}
	}

	filePath := filepath.Join(d.baseDir, meta.FileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 文件丢失，清理元数据
		d.deleteMetadata(key)
		atomic.AddInt64(&d.missCount, 1)
		return nil, fmt.Errorf("cache file missing")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		atomic.AddInt64(&d.missCount, 1)
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	// 异步更新访问信息
	go d.updateAccessTime(key, meta)

	atomic.AddInt64(&d.hitCount, 1)
	cachedImage := CachedImage{
		Data:        data,
		ContentType: meta.ContentType,
		Size:        meta.Size,
		AccessCount: meta.AccessCount,
		CreatedAt:   meta.CreatedAt,
		LastAccess:  meta.LastAccess,
	}

	return cachedImage, nil
}

// Set 设置磁盘缓存数据
func (d *L3DiskAdapterOptimized) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	var data []byte
	var contentType string
	var isOriginal bool

	switch v := value.(type) {
	case []byte:
		data = v
		contentType = "application/octet-stream"
		isOriginal = filepath.Base(key) == "raw_"+key
	case CachedImage:
		data = v.Data
		contentType = v.ContentType
		isOriginal = filepath.Base(key) == "raw_"+key
	default:
		return fmt.Errorf("unsupported value type for disk cache")
	}

	size := int64(len(data))

	// 检查磁盘空间，必要时清理
	for atomic.LoadInt64(&d.currentSize)+size > d.maxSize {
		if err := d.evictOldest(); err != nil {
			return fmt.Errorf("failed to evict old files: %w", err)
		}
	}

	fileName := d.generateFileName(key)
	filePath := filepath.Join(d.baseDir, fileName)

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}
	now := time.Now()
	meta := &DiskCacheMetadata{
		Key:         key,
		FileName:    fileName,
		Size:        size,
		CreatedAt:   now,
		LastAccess:  now,
		AccessCount: 1,
		ContentType: contentType,
		IsOriginal:  isOriginal,
	}

	// 保存到数据库和内存缓存
	if err := d.saveMetadata(key, meta); err != nil {
		// 如果保存元数据失败，删除文件
		os.Remove(filePath)
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	atomic.AddInt64(&d.currentSize, size)

	return nil
}

// Delete 删除缓存项
func (d *L3DiskAdapterOptimized) Delete(ctx context.Context, key string) error {
	d.mu.RLock()
	meta, exists := d.metaCache[key]
	d.mu.RUnlock()

	if !exists {
		// 尝试从数据库查找
		err := d.db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket(metadataBucket)
			data := b.Get([]byte(key))
			if data != nil {
				meta = &DiskCacheMetadata{}
				return json.Unmarshal(data, meta)
			}
			return nil
		})
		if err != nil || meta == nil {
			return nil // 不存在就不需要删除
		}
	}

	// 删除文件
	filePath := filepath.Join(d.baseDir, meta.FileName)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	// 删除元数据
	d.deleteMetadata(key)
	atomic.AddInt64(&d.currentSize, -meta.Size)

	return nil
}

// Clear 清空缓存
func (d *L3DiskAdapterOptimized) Clear(ctx context.Context) error {
	// 清空数据库中的所有元数据
	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(metadataBucket)
		c := b.Cursor()

		// 删除所有键值对
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to clear database: %w", err)
	}

	// 清空内存缓存
	d.mu.Lock()
	defer d.mu.Unlock()

	// 删除所有文件
	for _, meta := range d.metaCache {
		filePath := filepath.Join(d.baseDir, meta.FileName)
		os.Remove(filePath) // 忽略错误，因为文件可能已经不存在
	}

	// 重置内存状态
	d.metaCache = make(map[string]*DiskCacheMetadata)
	atomic.StoreInt64(&d.currentSize, 0)

	return nil
}

// Stats 获取统计信息
func (d *L3DiskAdapterOptimized) Stats() CacheStats {
	hitCount := atomic.LoadInt64(&d.hitCount)
	missCount := atomic.LoadInt64(&d.missCount)
	total := hitCount + missCount

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hitCount) / float64(total)
	}

	d.mu.RLock()
	itemCount := int64(len(d.metaCache))
	d.mu.RUnlock()

	return CacheStats{
		Level:         L3Disk,
		Items:         itemCount,
		UsedMemory:    atomic.LoadInt64(&d.currentSize),
		MaxMemory:     d.maxSize,
		HitRatio:      hitRatio,
		EvictionCount: atomic.LoadInt64(&d.evictionCount),
	}
}

// Close 关闭缓存
func (d *L3DiskAdapterOptimized) Close() error {
	close(d.stopCleanup)
	if d.cleanupTicker != nil {
		d.cleanupTicker.Stop()
	}

	return d.db.Close()
}

// generateFileName 生成文件名
func (d *L3DiskAdapterOptimized) generateFileName(key string) string {
	hash := md5.Sum([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)
	return filepath.Join(hashStr[:2], hashStr[2:4], hashStr)
}

// saveMetadata 保存元数据到数据库
func (d *L3DiskAdapterOptimized) saveMetadata(key string, meta *DiskCacheMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	// 保存到数据库
	err = d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(metadataBucket)
		return b.Put([]byte(key), data)
	})
	if err != nil {
		return err
	}

	// 更新内存缓存
	d.mu.Lock()
	d.metaCache[key] = meta
	d.mu.Unlock()

	return nil
}

// deleteMetadata 删除元数据
func (d *L3DiskAdapterOptimized) deleteMetadata(key string) error {
	// 从数据库删除
	err := d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(metadataBucket)
		return b.Delete([]byte(key))
	})

	// 从内存缓存删除
	d.mu.Lock()
	delete(d.metaCache, key)
	d.mu.Unlock()

	return err
}

// updateAccessTime 更新访问时间
func (d *L3DiskAdapterOptimized) updateAccessTime(key string, meta *DiskCacheMetadata) {
	now := time.Now()
	meta.LastAccess = now
	meta.AccessCount++

	// 批量更新，减少数据库写入频率
	d.saveMetadata(key, meta)
}

// loadMetadataFromDB 从数据库加载元数据
func (d *L3DiskAdapterOptimized) loadMetadataFromDB() error {
	return d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(metadataBucket)
		c := b.Cursor()

		var currentSize int64
		metaCache := make(map[string]*DiskCacheMetadata)

		for k, v := c.First(); k != nil; k, v = c.Next() {
			meta := &DiskCacheMetadata{}
			if err := json.Unmarshal(v, meta); err != nil {
				continue // 跳过损坏的数据
			}

			// 验证文件是否存在
			filePath := filepath.Join(d.baseDir, meta.FileName)
			if stat, err := os.Stat(filePath); err == nil {
				metaCache[string(k)] = meta
				currentSize += stat.Size()
			}
		}

		d.mu.Lock()
		d.metaCache = metaCache
		d.mu.Unlock()
		atomic.StoreInt64(&d.currentSize, currentSize)

		return nil
	})
}

// evictOldest 驱逐最旧的文件
func (d *L3DiskAdapterOptimized) evictOldest() error {
	d.mu.RLock()
	var oldestKey string
	var oldestTime time.Time = time.Now()

	for key, meta := range d.metaCache {
		if meta.LastAccess.Before(oldestTime) {
			oldestTime = meta.LastAccess
			oldestKey = key
		}
	}
	d.mu.RUnlock()

	if oldestKey == "" {
		return fmt.Errorf("no files to evict")
	}

	// 删除最旧的文件
	return d.Delete(context.Background(), oldestKey)
}

// backgroundCleanup 后台清理任务
func (d *L3DiskAdapterOptimized) backgroundCleanup() {
	for {
		select {
		case <-d.cleanupTicker.C:
			d.performCleanup()
		case <-d.stopCleanup:
			return
		}
	}
}

// performCleanup 执行清理
func (d *L3DiskAdapterOptimized) performCleanup() {
	now := time.Now()
	expiredKeys := make([]string, 0)

	d.mu.RLock()
	for key, meta := range d.metaCache {
		if now.Sub(meta.LastAccess) > 7*24*time.Hour {
			expiredKeys = append(expiredKeys, key)
		}
	}
	d.mu.RUnlock()

	// 删除过期项
	for _, key := range expiredKeys {
		d.Delete(context.Background(), key)
		atomic.AddInt64(&d.evictionCount, 1)
	}

	// 如果仍然超过限制，继续清理
	for atomic.LoadInt64(&d.currentSize) > d.maxSize*90/100 {
		if err := d.evictOldest(); err != nil {
			break
		}
	}
}
