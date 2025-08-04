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

	"github.com/h2non/bimg"
)

// ConfigureVips 配置libvips的并发和缓存参数
func ConfigureVips(concurrency, cacheSize, cacheMemMB int) {
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
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
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	oldSize := int(C.vips_cache_get_size())
	bimg.VipsCacheDropAll()
	newSize := int(C.vips_cache_get_size())
	cleared := oldSize - newSize

	log.Printf("libvips cache cleared: %d -> %d items (%d cleared)", oldSize, newSize, cleared)

	return cleared
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

	StopAutoCleanup()

	if intervalMinutes <= 0 {
		intervalMinutes = 30
	}
	if thresholdPercent <= 0 {
		thresholdPercent = 0.8
	}

	cleanupTicker = time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	cleanupStopChan = make(chan struct{})

	go func() {
		for {
			select {
			case <-cleanupTicker.C:
				cacheSize := int(C.vips_cache_get_size())
				cacheMax := int(C.vips_cache_get_max())

				if cacheMax > 0 {
					usagePercent := float64(cacheSize) / float64(cacheMax)
					// 温和的清理策略：70%开始清理，或者满了就必须清理
					if usagePercent >= 0.7 || cacheSize >= cacheMax {
						cleared := ClearVipsCache()
						log.Printf("Auto-cleared libvips cache: %d items (usage was %.1f%%)",
							cleared, usagePercent*100)
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
