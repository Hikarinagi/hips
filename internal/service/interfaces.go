package service

import (
	"context"
	"time"

	"hips/internal/cache"
	"hips/pkg/concurrent"
	"hips/pkg/imaging"
)

// ProcessTimings 包含各个处理阶段的耗时
type ProcessTimings struct {
	NetworkTime    time.Duration `json:"network_time"`    // 网络获取时间
	ProcessingTime time.Duration `json:"processing_time"` // 图片处理时间
	TotalTime      time.Duration `json:"total_time"`      // 总耗时
	CacheHit       bool          `json:"cache_hit"`       // 是否命中缓存
	ResizeSkipped  bool          `json:"resize_skipped"`  // 是否跳过resize
}

// ProcessResult 包含处理结果和详细时间信息
type ProcessResult struct {
	Data        []byte              `json:"-"`
	ContentType string              `json:"content_type"`
	Timings     ProcessTimings      `json:"timings"`
	CacheInfo   *cache.CacheHitInfo `json:"cache_info,omitempty"` // 多层缓存命中信息
}

// StorageResult 包含存储获取结果和网络耗时
type StorageResult struct {
	Data        []byte        `json:"-"`
	NetworkTime time.Duration `json:"network_time"`
	CacheHit    bool          `json:"cache_hit"`
}

type StorageService interface {
	GetImage(imagePath string) ([]byte, error)
	GetImageWithTiming(imagePath string) (StorageResult, error)
}

type ImageService interface {
	ProcessImageRequest(imagePath string, params imaging.ImageParams) (ProcessResult, error)
	ProcessBatch(ctx context.Context, request BatchProcessRequest) (BatchProcessResult, error)
	Close() error
	GetProcessorStats() concurrent.ProcessorStats
	GetMetrics() concurrent.Metrics
	AdjustWorkers(newWorkerCount int) error
}
