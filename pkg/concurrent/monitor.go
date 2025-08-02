package concurrent

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"
)

type ResourceMonitor struct {
	processor *ConcurrentImageProcessor
	metrics   *MetricsCollector
	autoTuner *AutoTuner
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type MetricsCollector struct {
	processedImages  int64
	totalProcessTime time.Duration
	avgProcessTime   time.Duration
	memoryUsage      int64
	cpuUsage         float64
	errorCount       int64
	successCount     int64
	mu               sync.RWMutex
}

type AutoTuner struct {
	processor       *ConcurrentImageProcessor
	metrics         *MetricsCollector
	lastAdjustment  time.Time
	adjustInterval  time.Duration
	targetCPUUsage  float64
	targetQueueSize float64
	mu              sync.RWMutex
}

type MonitorConfig struct {
	MonitorInterval  time.Duration
	AutoTuneInterval time.Duration
	TargetCPUUsage   float64
	TargetQueueSize  float64
	EnableAutoTuning bool
}

func NewResourceMonitor(processor *ConcurrentImageProcessor, config MonitorConfig) *ResourceMonitor {
	if config.MonitorInterval <= 0 {
		config.MonitorInterval = 30 * time.Second
	}
	if config.AutoTuneInterval <= 0 {
		config.AutoTuneInterval = 5 * time.Minute
	}
	if config.TargetCPUUsage <= 0 {
		config.TargetCPUUsage = 0.8
	}
	if config.TargetQueueSize <= 0 {
		config.TargetQueueSize = 0.7
	}

	ctx, cancel := context.WithCancel(context.Background())

	metrics := &MetricsCollector{}
	autoTuner := &AutoTuner{
		processor:       processor,
		metrics:         metrics,
		adjustInterval:  config.AutoTuneInterval,
		targetCPUUsage:  config.TargetCPUUsage,
		targetQueueSize: config.TargetQueueSize,
	}

	monitor := &ResourceMonitor{
		processor: processor,
		metrics:   metrics,
		autoTuner: autoTuner,
		ctx:       ctx,
		cancel:    cancel,
	}

	monitor.wg.Add(1)
	go monitor.startMonitoring(config.MonitorInterval)

	if config.EnableAutoTuning {
		monitor.wg.Add(1)
		go monitor.startAutoTuning(config.AutoTuneInterval)
	}

	return monitor
}

func (rm *ResourceMonitor) startMonitoring(interval time.Duration) {
	defer rm.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.collectMetrics()
		case <-rm.ctx.Done():
			return
		}
	}
}

func (rm *ResourceMonitor) startAutoTuning(interval time.Duration) {
	defer rm.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.autoTuner.tune()
		case <-rm.ctx.Done():
			return
		}
	}
}

func (rm *ResourceMonitor) collectMetrics() {
	rm.metrics.mu.Lock()
	defer rm.metrics.mu.Unlock()

	stats := rm.processor.GetStats()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	rm.metrics.memoryUsage = int64(memStats.Alloc)

	numCPU := runtime.NumCPU()
	activeWorkers := float64(stats.ActiveWorkers)
	maxWorkers := float64(stats.MaxWorkers)

	if maxWorkers > 0 {
		rm.metrics.cpuUsage = (activeWorkers / maxWorkers) * float64(numCPU)
	}

	// 注释掉定时输出，避免日志过多
	// log.Printf("Resource Monitor - Active Workers: %d/%d, Queue: %d/%d, Memory: %dMB, CPU Usage: %.2f",
	//	stats.ActiveWorkers, stats.MaxWorkers,
	//	stats.QueueLength, stats.MaxQueueSize,
	//	rm.metrics.memoryUsage/(1024*1024),
	//	rm.metrics.cpuUsage)
}

func (at *AutoTuner) tune() {
	at.mu.Lock()
	defer at.mu.Unlock()

	if time.Since(at.lastAdjustment) < at.adjustInterval {
		return
	}

	stats := at.processor.GetStats()

	at.metrics.mu.RLock()
	currentCPU := at.metrics.cpuUsage
	at.metrics.mu.RUnlock()

	queueUsage := float64(stats.QueueLength) / float64(stats.MaxQueueSize)

	var shouldAdjust bool
	var newWorkerCount int

	if currentCPU > at.targetCPUUsage && queueUsage < at.targetQueueSize {
		newWorkerCount = int(float64(stats.MaxWorkers) * 0.9)
		shouldAdjust = true
		log.Printf("Auto-tuner: Decreasing workers due to high CPU usage (%.2f > %.2f)",
			currentCPU, at.targetCPUUsage)
	} else if currentCPU < at.targetCPUUsage*0.6 && queueUsage > at.targetQueueSize {
		newWorkerCount = int(float64(stats.MaxWorkers) * 1.1)
		shouldAdjust = true
		log.Printf("Auto-tuner: Increasing workers due to low CPU usage (%.2f < %.2f) and high queue usage (%.2f > %.2f)",
			currentCPU, at.targetCPUUsage*0.6, queueUsage, at.targetQueueSize)
	}

	if shouldAdjust {
		minWorkers := runtime.NumCPU()
		maxWorkers := runtime.NumCPU() * 4

		if newWorkerCount < minWorkers {
			newWorkerCount = minWorkers
		} else if newWorkerCount > maxWorkers {
			newWorkerCount = maxWorkers
		}

		if newWorkerCount != stats.MaxWorkers {
			log.Printf("Auto-tuner: Adjusting workers from %d to %d", stats.MaxWorkers, newWorkerCount)
			at.processor.Resize(newWorkerCount)
			at.lastAdjustment = time.Now()
		}
	}
}

func (mc *MetricsCollector) RecordProcessing(duration time.Duration, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.processedImages++
	mc.totalProcessTime += duration

	if mc.processedImages > 0 {
		mc.avgProcessTime = mc.totalProcessTime / time.Duration(mc.processedImages)
	}

	if success {
		mc.successCount++
	} else {
		mc.errorCount++
	}
}

func (mc *MetricsCollector) GetMetrics() Metrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	successRate := float64(0)
	if mc.processedImages > 0 {
		successRate = float64(mc.successCount) / float64(mc.processedImages)
	}

	return Metrics{
		ProcessedImages: mc.processedImages,
		AvgProcessTime:  mc.avgProcessTime,
		MemoryUsage:     mc.memoryUsage,
		CPUUsage:        mc.cpuUsage,
		ErrorCount:      mc.errorCount,
		SuccessRate:     successRate,
	}
}

type Metrics struct {
	ProcessedImages int64         `json:"processed_images"`
	AvgProcessTime  time.Duration `json:"avg_process_time"`
	MemoryUsage     int64         `json:"memory_usage"`
	CPUUsage        float64       `json:"cpu_usage"`
	ErrorCount      int64         `json:"error_count"`
	SuccessRate     float64       `json:"success_rate"`
}

func (rm *ResourceMonitor) GetMetrics() Metrics {
	return rm.metrics.GetMetrics()
}

func (rm *ResourceMonitor) RecordProcessing(duration time.Duration, success bool) {
	rm.metrics.RecordProcessing(duration, success)
}

func (rm *ResourceMonitor) Close() error {
	rm.cancel()
	rm.wg.Wait()
	return nil
}

func (rm *ResourceMonitor) ForceAdjustWorkers(newWorkerCount int) error {
	if newWorkerCount <= 0 {
		return ErrInvalidWorkerCount
	}

	log.Printf("Manual adjustment: Setting workers to %d", newWorkerCount)
	rm.processor.Resize(newWorkerCount)

	rm.autoTuner.mu.Lock()
	rm.autoTuner.lastAdjustment = time.Now()
	rm.autoTuner.mu.Unlock()

	return nil
}
