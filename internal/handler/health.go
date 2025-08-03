package handler

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"hips/internal/cache"
	"hips/internal/service"
	"hips/pkg/imaging"
)

type HealthHandler struct {
	cache        cache.CacheService
	imageService service.ImageService
}

func NewHealthHandler(cacheService cache.CacheService) *HealthHandler {
	return &HealthHandler{
		cache: cacheService,
	}
}

func (h *HealthHandler) SetImageService(imageService service.ImageService) {
	h.imageService = imageService
}

func (h *HealthHandler) HandleHealth(c *gin.Context) {
	response := gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
	}

	if h.imageService != nil {
		stats := h.imageService.GetProcessorStats()
		metrics := h.imageService.GetMetrics()

		response["processor"] = gin.H{
			"max_workers":     stats.MaxWorkers,
			"queue_length":    stats.QueueLength,
			"max_queue_size":  stats.MaxQueueSize,
			"active_workers":  stats.ActiveWorkers,
			"queue_usage_pct": float64(stats.QueueLength) / float64(stats.MaxQueueSize) * 100,
		}

		// 添加libvips配置信息
		response["libvips"] = imaging.GetVipsInfo()

		// 获取实际内存使用情况
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		runtimeMemoryMB := int64(memStats.Alloc) / (1024 * 1024)

		response["metrics"] = gin.H{
			"processed_images":    metrics.ProcessedImages,
			"avg_process_time_ms": metrics.AvgProcessTime.Milliseconds(),
			"memory_usage_mb":     metrics.MemoryUsage / (1024 * 1024),
			"runtime_memory_mb":   runtimeMemoryMB,
			"cpu_usage":           metrics.CPUUsage,
			"error_count":         metrics.ErrorCount,
			"success_rate":        metrics.SuccessRate * 100,
		}

		// 检查多层缓存统计
		if multiCacheService, ok := h.imageService.(interface {
			GetMultiCacheStats() map[cache.CacheLevel]cache.CacheStats
		}); ok {
			cacheStats := multiCacheService.GetMultiCacheStats()
			if len(cacheStats) > 0 {
				multiCacheInfo := make(map[string]gin.H)
				totalCacheMemory := int64(0)
				for level, stat := range cacheStats {
					totalCacheMemory += stat.UsedMemory
					multiCacheInfo[level.String()] = gin.H{
						"items":          stat.Items,
						"used_memory_mb": stat.UsedMemory / (1024 * 1024),
						"max_memory_mb":  stat.MaxMemory / (1024 * 1024),
						"hit_ratio_pct":  stat.HitRatio * 100,
						"eviction_count": stat.EvictionCount,
						"usage_pct":      float64(stat.UsedMemory) / float64(stat.MaxMemory) * 100,
					}
				}
				response["cache"] = multiCacheInfo
				totalCacheMemoryMB := totalCacheMemory / (1024 * 1024)
				response["total_cache_memory_mb"] = totalCacheMemoryMB

				// 内存泄漏检测：比较运行时内存和缓存内存
				runtimeMemoryMB := int64(memStats.Alloc) / (1024 * 1024)
				unaccountedMemoryMB := runtimeMemoryMB - totalCacheMemoryMB
				response["memory_analysis"] = gin.H{
					"runtime_memory_mb":     runtimeMemoryMB,
					"cache_memory_mb":       totalCacheMemoryMB,
					"unaccounted_memory_mb": unaccountedMemoryMB,
					"potential_leak":        unaccountedMemoryMB > 500, // 超过500MB认为可能有泄漏
				}
			}
		} else if h.cache != nil {
			// 传统单层缓存统计
			response["cache"] = gin.H{
				"type":  "single_level",
				"items": h.cache.ItemCount(),
			}
		}
	}

	c.JSON(http.StatusOK, response)
}
