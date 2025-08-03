package service

import (
	"context"
	"log"
	"sync"
	"time"

	"hips/internal/cache"
	"hips/internal/config"
	"hips/pkg/concurrent"
	"hips/pkg/imaging"
)

type ImageServiceImpl struct {
	storageService StorageService
	cache          cache.CacheService
	multiCache     cache.LayeredCacheService
	processor      *concurrent.ConcurrentImageProcessor
	monitor        *concurrent.ResourceMonitor
	enableAsync    bool
	taskTimeout    time.Duration
}

// 创建处理器和监控器（公共逻辑）
func createProcessorAndMonitor(concurrentConfig config.ConcurrentConfig) (*concurrent.ConcurrentImageProcessor, *concurrent.ResourceMonitor) {
	processorConfig := concurrent.ProcessorConfig{
		MaxWorkers:   concurrentConfig.MaxWorkers,
		MaxQueueSize: concurrentConfig.MaxQueueSize,
		BufferSize:   concurrentConfig.BufferSize,
	}

	processor := concurrent.NewConcurrentImageProcessor(processorConfig)

	monitorConfig := concurrent.MonitorConfig{
		MonitorInterval:  30 * time.Second,
		AutoTuneInterval: 5 * time.Minute,
		TargetCPUUsage:   0.8,
		TargetQueueSize:  0.7,
		EnableAutoTuning: true,
	}
	monitor := concurrent.NewResourceMonitor(processor, monitorConfig)

	return processor, monitor
}

func NewImageService(storageService StorageService, cacheService cache.CacheService, concurrentConfig config.ConcurrentConfig) *ImageServiceImpl {
	processor, monitor := createProcessorAndMonitor(concurrentConfig)

	return &ImageServiceImpl{
		storageService: storageService,
		cache:          cacheService,
		processor:      processor,
		monitor:        monitor,
		enableAsync:    concurrentConfig.EnableAsync,
		taskTimeout:    concurrentConfig.TaskTimeout,
	}
}

func NewImageServiceWithMultiCache(storageService StorageService, multiCache cache.LayeredCacheService, concurrentConfig config.ConcurrentConfig) *ImageServiceImpl {
	processor, monitor := createProcessorAndMonitor(concurrentConfig)

	return &ImageServiceImpl{
		storageService: storageService,
		multiCache:     multiCache,
		processor:      processor,
		monitor:        monitor,
		enableAsync:    concurrentConfig.EnableAsync,
		taskTimeout:    concurrentConfig.TaskTimeout,
	}
}

func (s *ImageServiceImpl) ProcessImageRequest(imagePath string, params imaging.ImageParams) (ProcessResult, error) {
	totalStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), s.taskTimeout)
	defer cancel()

	cacheKey := imaging.GenerateCacheKey(imagePath, params)

	if s.multiCache != nil {
		cached, cacheInfo, err := s.multiCache.Get(ctx, cacheKey)
		if err == nil && cached != nil {
			var cachedImage cache.CachedImage
			switch v := cached.(type) {
			case cache.CachedImage:
				cachedImage = v
			case []byte:
				cachedImage = cache.CachedImage{
					Data:        v,
					ContentType: "image/jpeg", // 默认类型
				}
			}

			return ProcessResult{
				Data:        cachedImage.Data,
				ContentType: cachedImage.ContentType,
				CacheInfo:   cacheInfo,
				Timings: ProcessTimings{
					NetworkTime:    0,
					ProcessingTime: 0,
					TotalTime:      time.Since(totalStart),
					CacheHit:       true,
					ResizeSkipped:  false,
				},
			}, nil
		}
	} else if s.cache != nil {
		if cached, found := s.cache.Get(cacheKey); found {
			cachedImage := cached.(cache.CachedImage)
			return ProcessResult{
				Data:        cachedImage.Data,
				ContentType: cachedImage.ContentType,
				Timings: ProcessTimings{
					NetworkTime:    0,
					ProcessingTime: 0,
					TotalTime:      time.Since(totalStart),
					CacheHit:       true,
					ResizeSkipped:  false,
				},
			}, nil
		}
	}

	storageResult, err := s.storageService.GetImageWithTiming(imagePath)
	if err != nil {
		return ProcessResult{}, err
	}

	var processResult imaging.ProcessResult
	if s.enableAsync && s.processor != nil {
		processResult, err = s.processor.ProcessAsync(ctx, storageResult.Data, params)
		if err != nil {
			return ProcessResult{}, err
		}
	} else {
		// 同步处理降级
		processResult, err = imaging.ProcessImageWithTiming(storageResult.Data, params)
		if err != nil {
			return ProcessResult{}, err
		}
	}

	cachedImage := cache.CachedImage{
		Data:        processResult.Data,
		ContentType: processResult.ContentType,
		Size:        int64(len(processResult.Data)),
		AccessCount: 1,
		CreatedAt:   time.Now(),
		LastAccess:  time.Now(),
	}

	if s.multiCache != nil {
		if err := s.multiCache.Set(ctx, cacheKey, cachedImage, config.CacheTTL); err != nil {
			log.Printf("Failed to set cache: %v", err)
		}
	} else if s.cache != nil {
		s.cache.Set(cacheKey, cachedImage, config.CacheTTL)
	}

	if s.monitor != nil {
		s.monitor.RecordProcessing(processResult.ProcessTime, true)
	}

	return ProcessResult{
		Data:        processResult.Data,
		ContentType: processResult.ContentType,
		Timings: ProcessTimings{
			NetworkTime:    storageResult.NetworkTime,
			ProcessingTime: processResult.ProcessTime,
			TotalTime:      time.Since(totalStart),
			CacheHit:       storageResult.CacheHit,
			ResizeSkipped:  processResult.ResizeSkipped,
		},
		CacheInfo: &cache.CacheHitInfo{
			Level: cache.L4CDN,
			Hit:   false,
		},
	}, nil
}

func (s *ImageServiceImpl) Close() error {
	var err error

	if s.monitor != nil {
		if monitorErr := s.monitor.Close(); monitorErr != nil {
			log.Printf("Error closing monitor: %v", monitorErr)
			err = monitorErr
		}
	}

	if s.processor != nil {
		if processorErr := s.processor.Close(); processorErr != nil {
			log.Printf("Error closing processor: %v", processorErr)
			if err == nil {
				err = processorErr
			}
		}
	}

	return err
}

func (s *ImageServiceImpl) GetProcessorStats() concurrent.ProcessorStats {
	if s.processor != nil {
		return s.processor.GetStats()
	}
	return concurrent.ProcessorStats{}
}

func (s *ImageServiceImpl) GetMetrics() concurrent.Metrics {
	if s.monitor != nil {
		return s.monitor.GetMetrics()
	}
	return concurrent.Metrics{}
}

func (s *ImageServiceImpl) AdjustWorkers(newWorkerCount int) error {
	if s.monitor != nil {
		return s.monitor.ForceAdjustWorkers(newWorkerCount)
	}
	if s.processor != nil {
		s.processor.Resize(newWorkerCount)
		return nil
	}
	return concurrent.ErrWorkerPoolClosed
}

func (s *ImageServiceImpl) GetMultiCacheStats() map[cache.CacheLevel]cache.CacheStats {
	if s.multiCache != nil {
		return s.multiCache.GetStats()
	}
	return make(map[cache.CacheLevel]cache.CacheStats)
}

type BatchProcessRequest struct {
	ImagePath string                `json:"image_path"`
	Variants  []imaging.ImageParams `json:"variants"`
}

type BatchProcessResult struct {
	ImagePath string                      `json:"image_path"`
	Results   map[string]BatchProcessItem `json:"results"`
	Errors    map[string]string           `json:"errors"`
}

type BatchProcessItem struct {
	Data        []byte        `json:"-"`
	ContentType string        `json:"content_type"`
	Size        int           `json:"size"`
	ProcessTime time.Duration `json:"process_time"`
}

func (s *ImageServiceImpl) ProcessBatch(ctx context.Context, request BatchProcessRequest) (BatchProcessResult, error) {
	result := BatchProcessResult{
		ImagePath: request.ImagePath,
		Results:   make(map[string]BatchProcessItem),
		Errors:    make(map[string]string),
	}

	imageData, err := s.storageService.GetImage(request.ImagePath)
	if err != nil {
		return result, err
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, params := range request.Variants {
		wg.Add(1)
		go func(p imaging.ImageParams) {
			defer wg.Done()

			cacheKey := imaging.GenerateCacheKey(request.ImagePath, p)

			if cached, found := s.cache.Get(cacheKey); found {
				cachedImage := cached.(cache.CachedImage)

				mu.Lock()
				result.Results[cacheKey] = BatchProcessItem{
					Data:        cachedImage.Data,
					ContentType: cachedImage.ContentType,
					Size:        len(cachedImage.Data),
					ProcessTime: 0,
				}
				mu.Unlock()
				return
			}

			var processedData []byte
			var contentType string
			var processTime time.Duration

			if s.enableAsync && s.processor != nil {
				processResult, processErr := s.processor.ProcessAsync(ctx, imageData, p)

				if processErr != nil {
					mu.Lock()
					result.Errors[cacheKey] = processErr.Error()
					mu.Unlock()
					return
				}

				processedData = processResult.Data
				contentType = processResult.ContentType
				processTime = processResult.ProcessTime
			} else {
				start := time.Now()
				var processErr error
				processedData, contentType, processErr = imaging.ProcessImage(imageData, p)
				processTime = time.Since(start)

				if processErr != nil {
					mu.Lock()
					result.Errors[cacheKey] = processErr.Error()
					mu.Unlock()
					return
				}
			}

			cachedImage := cache.CachedImage{
				Data:        processedData,
				ContentType: contentType,
			}
			s.cache.Set(cacheKey, cachedImage, config.CacheTTL)

			mu.Lock()
			result.Results[cacheKey] = BatchProcessItem{
				Data:        processedData,
				ContentType: contentType,
				Size:        len(processedData),
				ProcessTime: processTime,
			}
			mu.Unlock()

			if s.monitor != nil {
				s.monitor.RecordProcessing(processTime, true)
			}
		}(params)
	}

	wg.Wait()
	return result, nil
}
