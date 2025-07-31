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
		"status":      "healthy",
		"timestamp":   time.Now().Unix(),
		"cache_items": h.cache.ItemCount(),
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
	}

	c.JSON(http.StatusOK, response)
}
