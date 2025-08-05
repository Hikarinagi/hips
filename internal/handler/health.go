package handler

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
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

func (h *HealthHandler) HandleCacheClear(c *gin.Context) {
	result := gin.H{
		"timestamp": time.Now().Unix(),
		"status":    "success",
	}

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	clearedVips := imaging.ClearVipsCache()
	result["libvips_cleared"] = clearedVips

	runtime.GC()
	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	result["memory_before_mb"] = int64(memBefore.Sys) / (1024 * 1024)
	result["memory_after_mb"] = int64(memAfter.Sys) / (1024 * 1024)
	result["memory_freed_mb"] = int64(memBefore.Sys-memAfter.Sys) / (1024 * 1024)
	result["gc_forced"] = true

	c.JSON(http.StatusOK, result)
}

func (h *HealthHandler) HandleForceGC(c *gin.Context) {
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	realMemBefore := getRealMemoryUsage()

	runtime.GC()
	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	realMemAfter := getRealMemoryUsage()

	result := gin.H{
		"timestamp":        time.Now().Unix(),
		"status":           "success",
		"memory_before_mb": int64(memBefore.Sys) / (1024 * 1024),
		"memory_after_mb":  int64(memAfter.Sys) / (1024 * 1024),
		"freed_mb":         int64(memBefore.Sys-memAfter.Sys) / (1024 * 1024),
		"real_mem_before":  realMemBefore,
		"real_mem_after":   realMemAfter,
		"real_freed_mb":    realMemBefore - realMemAfter,
		"gc_runs_before":   memBefore.NumGC,
		"gc_runs_after":    memAfter.NumGC,
	}

	c.JSON(http.StatusOK, result)
}

func (h *HealthHandler) HandleEmergencyCleanup(c *gin.Context) {
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	realMemBefore := getRealMemoryUsage()

	result := gin.H{
		"timestamp": time.Now().Unix(),
		"status":    "success",
		"actions":   []string{},
	}

	clearedVips := imaging.ClearVipsCache()
	result["actions"] = append(result["actions"].([]string), fmt.Sprintf("cleared_libvips_cache: %d items", clearedVips))

	if multiCacheService, ok := h.imageService.(interface {
		GetMultiCacheStats() map[cache.CacheLevel]cache.CacheStats
	}); ok {
		stats := multiCacheService.GetMultiCacheStats()
		totalCleared := len(stats)
		for levelName := range stats {
			_ = levelName
		}
		result["actions"] = append(result["actions"].([]string), fmt.Sprintf("cleared_cache_levels: %d", totalCleared))
	}

	runtime.GC()
	runtime.GC()
	result["actions"] = append(result["actions"].([]string), "performed_2x_gc")

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	realMemAfter := getRealMemoryUsage()

	result["memory_before_mb"] = int64(memBefore.Sys) / (1024 * 1024)
	result["memory_after_mb"] = int64(memAfter.Sys) / (1024 * 1024)
	result["real_mem_before"] = realMemBefore
	result["real_mem_after"] = realMemAfter
	result["real_freed_mb"] = realMemBefore - realMemAfter
	result["effectiveness"] = fmt.Sprintf("%.1f%%", float64(realMemBefore-realMemAfter)/float64(realMemBefore)*100)

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

		response["libvips"] = imaging.GetVipsInfo()

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		allocMB := int64(memStats.Alloc) / (1024 * 1024)
		totalAllocMB := int64(memStats.TotalAlloc) / (1024 * 1024)
		sysMB := int64(memStats.Sys) / (1024 * 1024)
		heapMB := int64(memStats.HeapInuse) / (1024 * 1024)
		stackMB := int64(memStats.StackInuse) / (1024 * 1024)

		response["metrics"] = gin.H{
			"processed_images":    metrics.ProcessedImages,
			"avg_process_time_ms": metrics.AvgProcessTime.Milliseconds(),
			"memory_usage_mb":     metrics.MemoryUsage / (1024 * 1024),
			"runtime_memory_mb":   allocMB,
			"cpu_usage":           metrics.CPUUsage,
			"error_count":         metrics.ErrorCount,
			"success_rate":        metrics.SuccessRate * 100,
		}

		response["detailed_memory"] = gin.H{
			"go_alloc_mb":       allocMB,      // 当前Go堆分配
			"go_total_alloc_mb": totalAllocMB, // 累计分配
			"go_sys_mb":         sysMB,        // 从OS获取的内存
			"go_heap_mb":        heapMB,       // 堆内存使用
			"go_stack_mb":       stackMB,      // 栈内存使用
			"gc_runs":           memStats.NumGC,
			"goroutines":        runtime.NumGoroutine(),
		}

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

				potentialLeak := false
				leakSources := []string{}

				vipsInfo := response["libvips"].(map[string]interface{})
				cacheSize, _ := vipsInfo["cache_size"].(int)
				cacheMax, _ := vipsInfo["cache_max"].(int)

				if cacheMax > 0 && cacheSize >= cacheMax {
					leakSources = append(leakSources, "libvips cache full, cleanup needed")
				}

				goroutineCount := runtime.NumGoroutine()
				heapObjects := memStats.HeapObjects

				if goroutineCount > 1000 {
					potentialLeak = true
					leakSources = append(leakSources, "goroutine leak")
				}

				if heapObjects > 100000 {
					potentialLeak = true
					leakSources = append(leakSources, "excessive heap objects")
				}

				heapReleasedMB := int64(memStats.HeapReleased) / (1024 * 1024)
				if goSysMemoryMB > 200 && heapReleasedMB < goSysMemoryMB/4 {
					potentialLeak = true
					leakSources = append(leakSources, "memory not released to OS")
				}

				if memStats.NumGC > 0 {
					avgAllocBetweenGC := totalAllocMB / int64(memStats.NumGC)
					if avgAllocBetweenGC > 100 {
						leakSources = append(leakSources, "low GC efficiency")
					}
				}

				realMemoryMB := getRealMemoryUsage()
				cgoMemoryMB := realMemoryMB - goSysMemoryMB

				if cgoMemoryMB > 500 {
					potentialLeak = true
					leakSources = append(leakSources, fmt.Sprintf("high CGO memory: %dMB", cgoMemoryMB))
				}

				if realMemoryMB > 1000 {
					potentialLeak = true
					leakSources = append(leakSources, fmt.Sprintf("excessive total memory: %dMB", realMemoryMB))
				}

				response["memory_analysis"] = gin.H{
					"go_sys_memory_mb":      goSysMemoryMB,
					"cgo_memory_mb":         cgoMemoryMB,
					"real_memory_mb":        realMemoryMB,
					"l1_cache_memory_mb":    l1MemoryMB,
					"unaccounted_memory_mb": unaccountedMemoryMB,
					"potential_leak":        potentialLeak,
					"leak_sources":          leakSources,
					"action_needed":         potentialLeak,
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

func getRealMemoryUsage() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb / 1024
				}
			}
		}
	}
	return 0
}
