package imaging

/*
#cgo pkg-config: vips
#include <vips/vips.h>
*/
import "C"
import (
	"log"
	"runtime"
	"sync"
	"time"
)

// ConfigureVips 配置libvips的并发和缓存参数
func ConfigureVips(concurrency, cacheSize, cacheMemMB int) {
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() // 使用所有CPU核心
	}
	C.vips_concurrency_set(C.int(concurrency))

	if cacheSize <= 0 {
		cacheSize = 300
	}
	C.vips_cache_set_max(C.int(cacheSize))

	if cacheMemMB <= 0 {
		cacheMemMB = 512
	}
	C.vips_cache_set_max_mem(C.size_t(cacheMemMB * 1024 * 1024))

	log.Printf("libvips configured - concurrency: %d, cache_max: %d, cache_size: %d",
		concurrency, cacheSize, cacheMemMB)
}

// GetVipsInfo 获取libvips当前配置信息
func GetVipsInfo() map[string]interface{} {
	return map[string]interface{}{
		"concurrency": int(C.vips_concurrency_get()),
		"cache_max":   int(C.vips_cache_get_max()),
		"cache_size":  int(C.vips_cache_get_size()),
	}
}

// ClearVipsCache 清理libvips缓存
func ClearVipsCache() int {
	oldSize := int(C.vips_cache_get_size())
	// 使用vips_cache_set_max(0)然后恢复的方式来清理缓存
	oldMax := int(C.vips_cache_get_max())
	C.vips_cache_set_max(0)             // 清空缓存
	C.vips_cache_set_max(C.int(oldMax)) // 恢复原来的大小
	newSize := int(C.vips_cache_get_size())
	log.Printf("libvips cache cleared: %d -> %d items", oldSize, newSize)
	return oldSize - newSize
}

// SetVipsCacheMax 动态调整libvips缓存大小
func SetVipsCacheMax(maxItems int) {
	C.vips_cache_set_max(C.int(maxItems))
	log.Printf("libvips cache_max set to: %d", maxItems)
}

var (
	cleanupMutex    sync.Mutex
	cleanupTicker   *time.Ticker
	cleanupStopChan chan struct{}
)

// StartAutoCleanup 启动自动清理libvips缓存
func StartAutoCleanup(intervalMinutes int, thresholdPercent float64) {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	// 停止之前的清理任务
	StopAutoCleanup()

	if intervalMinutes <= 0 {
		intervalMinutes = 30 // 默认30分钟
	}
	if thresholdPercent <= 0 {
		thresholdPercent = 0.8 // 默认80%
	}

	cleanupTicker = time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	cleanupStopChan = make(chan struct{})

	go func() {
		for {
			select {
			case <-cleanupTicker.C:
				// 检查缓存使用率
				cacheSize := int(C.vips_cache_get_size())
				cacheMax := int(C.vips_cache_get_max())

				if cacheMax > 0 {
					usagePercent := float64(cacheSize) / float64(cacheMax)
					if usagePercent >= thresholdPercent {
						cleared := ClearVipsCache()
						log.Printf("Auto-cleared libvips cache: %d items (usage was %.1f%%)",
							cleared, usagePercent*100)

						// 强制GC
						runtime.GC()
						runtime.GC() // 两次GC确保释放
					}
				}

			case <-cleanupStopChan:
				return
			}
		}
	}()

	log.Printf("Started libvips auto-cleanup: interval=%dm, threshold=%.1f%%",
		intervalMinutes, thresholdPercent*100)
}

// StopAutoCleanup 停止自动清理
func StopAutoCleanup() {
	if cleanupTicker != nil {
		cleanupTicker.Stop()
		cleanupTicker = nil
	}

	if cleanupStopChan != nil {
		close(cleanupStopChan)
		cleanupStopChan = nil
	}
}
