package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"hips/internal/cache"
	"hips/internal/service"
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

	if h.cache != nil {
		response["cache_items"] = h.cache.ItemCount()
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

		response["metrics"] = gin.H{
			"processed_images":    metrics.ProcessedImages,
			"avg_process_time_ms": metrics.AvgProcessTime.Milliseconds(),
			"memory_usage_mb":     metrics.MemoryUsage / (1024 * 1024),
			"cpu_usage":           metrics.CPUUsage,
			"error_count":         metrics.ErrorCount,
			"success_rate":        metrics.SuccessRate * 100,
		}

		if multiCacheService, ok := h.imageService.(interface {
			GetMultiCacheStats() map[cache.CacheLevel]cache.CacheStats
		}); ok {
			cacheStats := multiCacheService.GetMultiCacheStats()
			if len(cacheStats) > 0 {
				multiCacheInfo := make(map[string]gin.H)
				for level, stat := range cacheStats {
					multiCacheInfo[level.String()] = gin.H{
						"items":          stat.Items,
						"used_memory_mb": stat.UsedMemory / (1024 * 1024),
						"max_memory_mb":  stat.MaxMemory / (1024 * 1024),
						"hit_ratio_pct":  stat.HitRatio * 100,
						"eviction_count": stat.EvictionCount,
						"usage_pct":      float64(stat.UsedMemory) / float64(stat.MaxMemory) * 100,
					}
				}
				response["multi_cache"] = multiCacheInfo
			}
		}
	}

	c.JSON(http.StatusOK, response)
}
