package cache

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// L3DiskAdapter L3磁盘缓存适配器
type L3DiskAdapter struct {
	baseDir     string
	maxSize     int64 // 最大磁盘空间（字节）
	currentSize int64 // 当前使用空间

	// 元数据管理
	metadata map[string]*DiskCacheMetadata
	mu       sync.RWMutex

	// 统计信息
	hitCount      int64
	missCount     int64
	evictionCount int64

	// 后台清理
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

// DiskCacheMetadata 磁盘缓存元数据
type DiskCacheMetadata struct {
	Key         string    `json:"key"`
	FileName    string    `json:"file_name"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	LastAccess  time.Time `json:"last_access"`
	AccessCount int64     `json:"access_count"`
	ContentType string    `json:"content_type"`
	IsOriginal  bool      `json:"is_original"` // 是否为原图
}

// NewL3DiskAdapter 创建L3磁盘缓存适配器
func NewL3DiskAdapter(baseDir string, maxSizeGB int64) (*L3DiskAdapter, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	adapter := &L3DiskAdapter{
		baseDir:     baseDir,
		maxSize:     maxSizeGB * 1024 * 1024 * 1024, // 转换为字节
		metadata:    make(map[string]*DiskCacheMetadata),
		stopCleanup: make(chan struct{}),
	}

	// 加载现有元数据
	if err := adapter.loadMetadata(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// 启动后台清理任务
	adapter.cleanupTicker = time.NewTicker(30 * time.Minute)
	go adapter.backgroundCleanup()

	return adapter, nil
}

// Get 从磁盘缓存获取数据
func (d *L3DiskAdapter) Get(ctx context.Context, key string) (interface{}, error) {
	d.mu.RLock()
	meta, exists := d.metadata[key]
	d.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&d.missCount, 1)
		return nil, fmt.Errorf("key not found in L3 cache")
	}

	// 检查文件是否存在
	filePath := filepath.Join(d.baseDir, meta.FileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 文件丢失，清理元数据
		d.mu.Lock()
		delete(d.metadata, key)
		d.currentSize -= meta.Size
		d.mu.Unlock()
		atomic.AddInt64(&d.missCount, 1)
		return nil, fmt.Errorf("cache file missing")
	}

	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		atomic.AddInt64(&d.missCount, 1)
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	// 更新访问信息
	d.mu.Lock()
	meta.LastAccess = time.Now()
	meta.AccessCount++
	d.mu.Unlock()

	atomic.AddInt64(&d.hitCount, 1)

	// 返回缓存的图片数据
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
func (d *L3DiskAdapter) Set(ctx context.Context, key string, value interface{}, duration time.Duration) error {
	var data []byte
	var contentType string
	var isOriginal bool

	switch v := value.(type) {
	case []byte:
		data = v
		contentType = "application/octet-stream"
		isOriginal = strings.Contains(key, "raw_") // 原图缓存标识
	case CachedImage:
		data = v.Data
		contentType = v.ContentType
		isOriginal = strings.Contains(key, "raw_")
	default:
		return fmt.Errorf("unsupported value type for disk cache")
	}

	size := int64(len(data))

	// 检查磁盘空间，必要时清理
	d.mu.Lock()
	for d.currentSize+size > d.maxSize && len(d.metadata) > 0 {
		if err := d.evictOldest(); err != nil {
			d.mu.Unlock()
			return fmt.Errorf("failed to evict old files: %w", err)
		}
	}
	d.mu.Unlock()

	// 生成文件名
	fileName := d.generateFileName(key)
	filePath := filepath.Join(d.baseDir, fileName)

	// 创建目录
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// 更新元数据
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

	d.mu.Lock()
	// 如果已存在，先清理旧文件
	if oldMeta, exists := d.metadata[key]; exists {
		oldPath := filepath.Join(d.baseDir, oldMeta.FileName)
		os.Remove(oldPath)
		d.currentSize -= oldMeta.Size
	}

	d.metadata[key] = meta
	d.currentSize += size
	d.mu.Unlock()

	// 保存元数据
	go d.saveMetadata()

	return nil
}

// Delete 删除缓存项
func (d *L3DiskAdapter) Delete(ctx context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	meta, exists := d.metadata[key]
	if !exists {
		return nil
	}

	// 删除文件
	filePath := filepath.Join(d.baseDir, meta.FileName)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	// 更新元数据
	delete(d.metadata, key)
	d.currentSize -= meta.Size

	return nil
}

// Clear 清空缓存
func (d *L3DiskAdapter) Clear(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 删除所有文件
	for _, meta := range d.metadata {
		filePath := filepath.Join(d.baseDir, meta.FileName)
		os.Remove(filePath)
	}

	// 清空元数据
	d.metadata = make(map[string]*DiskCacheMetadata)
	d.currentSize = 0

	return nil
}

// Stats 获取统计信息
func (d *L3DiskAdapter) Stats() CacheStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	hitCount := atomic.LoadInt64(&d.hitCount)
	missCount := atomic.LoadInt64(&d.missCount)
	total := hitCount + missCount

	var hitRatio float64
	if total > 0 {
		hitRatio = float64(hitCount) / float64(total)
	}

	return CacheStats{
		Level:         L3Disk,
		Items:         int64(len(d.metadata)),
		UsedMemory:    d.currentSize,
		MaxMemory:     d.maxSize,
		HitRatio:      hitRatio,
		EvictionCount: atomic.LoadInt64(&d.evictionCount),
	}
}

// Close 关闭缓存
func (d *L3DiskAdapter) Close() error {
	close(d.stopCleanup)
	if d.cleanupTicker != nil {
		d.cleanupTicker.Stop()
	}

	return d.saveMetadata()
}

// generateFileName 生成文件名
func (d *L3DiskAdapter) generateFileName(key string) string {
	hash := md5.Sum([]byte(key))
	hashStr := fmt.Sprintf("%x", hash)

	// 创建两级目录结构避免单目录文件过多
	return filepath.Join(hashStr[:2], hashStr[2:4], hashStr)
}

// evictOldest 驱逐最旧的文件
func (d *L3DiskAdapter) evictOldest() error {
	var oldestKey string
	var oldestTime time.Time = time.Now()

	// 找到最旧的文件
	for key, meta := range d.metadata {
		if meta.LastAccess.Before(oldestTime) {
			oldestTime = meta.LastAccess
			oldestKey = key
		}
	}

	if oldestKey == "" {
		return fmt.Errorf("no files to evict")
	}

	// 删除最旧的文件
	meta := d.metadata[oldestKey]
	filePath := filepath.Join(d.baseDir, meta.FileName)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	delete(d.metadata, oldestKey)
	d.currentSize -= meta.Size
	atomic.AddInt64(&d.evictionCount, 1)

	return nil
}

// loadMetadata 加载元数据
func (d *L3DiskAdapter) loadMetadata() error {
	metaFile := filepath.Join(d.baseDir, "metadata.json")

	data, err := os.ReadFile(metaFile)
	if os.IsNotExist(err) {
		return nil // 文件不存在是正常的
	}
	if err != nil {
		return fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata map[string]*DiskCacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	// 验证文件存在性并计算当前大小
	validMetadata := make(map[string]*DiskCacheMetadata)
	var currentSize int64

	for key, meta := range metadata {
		filePath := filepath.Join(d.baseDir, meta.FileName)
		if stat, err := os.Stat(filePath); err == nil {
			validMetadata[key] = meta
			currentSize += stat.Size()
		}
	}

	d.metadata = validMetadata
	d.currentSize = currentSize

	return nil
}

// saveMetadata 保存元数据
func (d *L3DiskAdapter) saveMetadata() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	metaFile := filepath.Join(d.baseDir, "metadata.json")

	data, err := json.MarshalIndent(d.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(metaFile, data, 0644)
}

// backgroundCleanup 后台清理任务
func (d *L3DiskAdapter) backgroundCleanup() {
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
func (d *L3DiskAdapter) performCleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	// 找出过期的缓存项（超过7天未访问）
	for key, meta := range d.metadata {
		if now.Sub(meta.LastAccess) > 7*24*time.Hour {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// 删除过期项
	for _, key := range expiredKeys {
		meta := d.metadata[key]
		filePath := filepath.Join(d.baseDir, meta.FileName)
		if err := os.Remove(filePath); err == nil {
			delete(d.metadata, key)
			d.currentSize -= meta.Size
			atomic.AddInt64(&d.evictionCount, 1)
		}
	}

	// 如果仍然超过限制，继续清理
	for d.currentSize > d.maxSize*90/100 && len(d.metadata) > 0 { // 保持90%以下
		d.evictOldest()
	}
}
