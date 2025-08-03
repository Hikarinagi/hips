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

// HandleCacheClear 清理缓存的端点
func (h *HealthHandler) HandleCacheClear(c *gin.Context) {
	result := gin.H{
		"timestamp": time.Now().Unix(),
		"status":    "success",
	}

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 清理libvips缓存
	clearedVips := imaging.ClearVipsCache()
	result["libvips_cleared"] = clearedVips

	// 强制GC
	runtime.GC()
	runtime.GC() // 两次GC

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	result["memory_before_mb"] = int64(memBefore.Sys) / (1024 * 1024)
	result["memory_after_mb"] = int64(memAfter.Sys) / (1024 * 1024)
	result["memory_freed_mb"] = int64(memBefore.Sys-memAfter.Sys) / (1024 * 1024)
	result["gc_forced"] = true

	c.JSON(http.StatusOK, result)
}

// HandleForceGC 强制内存回收的端点
func (h *HealthHandler) HandleForceGC(c *gin.Context) {
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 强制GC和内存释放
	runtime.GC()
	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	result := gin.H{
		"timestamp":        time.Now().Unix(),
		"status":           "success",
		"memory_before_mb": int64(memBefore.Sys) / (1024 * 1024),
		"memory_after_mb":  int64(memAfter.Sys) / (1024 * 1024),
		"freed_mb":         int64(memBefore.Sys-memAfter.Sys) / (1024 * 1024),
		"gc_runs_before":   memBefore.NumGC,
		"gc_runs_after":    memAfter.NumGC,
	}

	c.JSON(http.StatusOK, result)
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

		// 更准确的内存统计
		allocMB := int64(memStats.Alloc) / (1024 * 1024)           // 当前分配的Go堆内存
		totalAllocMB := int64(memStats.TotalAlloc) / (1024 * 1024) // 累计分配的内存
		sysMB := int64(memStats.Sys) / (1024 * 1024)               // 从OS获取的总内存
		heapMB := int64(memStats.HeapInuse) / (1024 * 1024)        // 正在使用的堆内存
		stackMB := int64(memStats.StackInuse) / (1024 * 1024)      // 栈内存

		response["metrics"] = gin.H{
			"processed_images":    metrics.ProcessedImages,
			"avg_process_time_ms": metrics.AvgProcessTime.Milliseconds(),
			"memory_usage_mb":     metrics.MemoryUsage / (1024 * 1024),
			"runtime_memory_mb":   allocMB,
			"cpu_usage":           metrics.CPUUsage,
			"error_count":         metrics.ErrorCount,
			"success_rate":        metrics.SuccessRate * 100,
		}

		// 详细内存分析
		response["detailed_memory"] = gin.H{
			"go_alloc_mb":       allocMB,      // 当前Go堆分配
			"go_total_alloc_mb": totalAllocMB, // 累计分配
			"go_sys_mb":         sysMB,        // 从OS获取的内存
			"go_heap_mb":        heapMB,       // 堆内存使用
			"go_stack_mb":       stackMB,      // 栈内存使用
			"gc_runs":           memStats.NumGC,
			"goroutines":        runtime.NumGoroutine(),
		}

		// 检查多层缓存统计
		if multiCacheService, ok := h.imageService.(interface {
			GetMultiCacheStats() map[cache.CacheLevel]cache.CacheStats
		}); ok {
			cacheStats := multiCacheService.GetMultiCacheStats()
			if len(cacheStats) > 0 {
				multiCacheInfo := make(map[string]gin.H)
				l1Memory := int64(0)
				for level, stat := range cacheStats {
					if level == cache.L1Memory {
						l1Memory = stat.UsedMemory
					}
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

				l1MemoryMB := l1Memory / (1024 * 1024)
				goSysMemoryMB := sysMB
				unaccountedMemoryMB := goSysMemoryMB - l1MemoryMB

				// 检查可能的内存泄漏
				potentialLeak := false
				leakSources := []string{}

				vipsInfo := response["libvips"].(map[string]interface{})
				cacheSize, _ := vipsInfo["cache_size"].(int)
				cacheMax, _ := vipsInfo["cache_max"].(int)

				// 检查libvips缓存是否需要清理
				if cacheMax > 0 && cacheSize >= cacheMax {
					leakSources = append(leakSources, "libvips缓存已满，需要清理")
				}

				// 真正的内存泄漏检测
				goroutineCount := runtime.NumGoroutine()
				heapObjects := memStats.HeapObjects

				// 检查goroutine泄漏
				if goroutineCount > 1000 {
					potentialLeak = true
					leakSources = append(leakSources, "goroutine泄漏")
				}

				// 检查堆对象异常增长
				if heapObjects > 100000 {
					potentialLeak = true
					leakSources = append(leakSources, "堆对象过多")
				}

				// 检查内存未释放给OS（可能的内存碎片）
				heapReleasedMB := int64(memStats.HeapReleased) / (1024 * 1024)
				if goSysMemoryMB > 200 && heapReleasedMB < goSysMemoryMB/4 {
					potentialLeak = true
					leakSources = append(leakSources, "内存未释放给OS")
				}

				// 检查GC效率
				if memStats.NumGC > 0 {
					avgAllocBetweenGC := totalAllocMB / int64(memStats.NumGC)
					if avgAllocBetweenGC > 100 {
						leakSources = append(leakSources, "GC效率低")
					}
				}

				response["memory_analysis"] = gin.H{
					"go_sys_memory_mb":      goSysMemoryMB,
					"l1_cache_memory_mb":    l1MemoryMB,
					"unaccounted_memory_mb": unaccountedMemoryMB,
					"potential_leak":        potentialLeak,
					"leak_sources":          leakSources,
					"warning":               "libvips缓存异常，可能存在内存泄漏",
					"note":                  "使用Go Sys内存(包含CGO)进行分析",
				}
			}
		} else if h.cache != nil {
			response["cache"] = gin.H{
				"type":  "single_level",
				"items": h.cache.ItemCount(),
			}
		}
	}

	c.JSON(http.StatusOK, response)
}
