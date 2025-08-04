package concurrent

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"hips/pkg/imaging"
)

type ProcessTask struct {
	ImageData  []byte
	Params     imaging.ImageParams
	ResultChan chan ProcessResult
	ErrorChan  chan error
	Context    context.Context
}

// 使用 imaging.ProcessResult 替代重复定义
type ProcessResult = imaging.ProcessResult

type ConcurrentImageProcessor struct {
	workerPool    chan struct{}
	taskQueue     chan ProcessTask
	maxWorkers    int
	maxQueueSize  int
	activeWorkers int32
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup

	// 温和的GC策略
	processedCount int64
	lastGCTime     int64
}

type ProcessorConfig struct {
	MaxWorkers   int
	MaxQueueSize int
	BufferSize   int
}

func NewConcurrentImageProcessor(config ProcessorConfig) *ConcurrentImageProcessor {
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU() * 2
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = config.MaxWorkers * 10
	}
	if config.BufferSize <= 0 {
		config.BufferSize = config.MaxWorkers
	}

	ctx, cancel := context.WithCancel(context.Background())

	processor := &ConcurrentImageProcessor{
		workerPool:   make(chan struct{}, config.MaxWorkers),
		taskQueue:    make(chan ProcessTask, config.MaxQueueSize),
		maxWorkers:   config.MaxWorkers,
		maxQueueSize: config.MaxQueueSize,
		ctx:          ctx,
		cancel:       cancel,
	}

	processor.startWorkers()
	return processor
}

func (p *ConcurrentImageProcessor) startWorkers() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func (p *ConcurrentImageProcessor) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case task := <-p.taskQueue:
			p.processTask(task, id)
		case <-p.ctx.Done():
			return
		}
	}
}

// tryGentleGC 温和的GC策略：仅在特定条件下触发GC
func (p *ConcurrentImageProcessor) tryGentleGC() {
	// 每处理100个任务，或者距离上次GC超过5分钟才考虑GC
	count := atomic.AddInt64(&p.processedCount, 1)
	now := time.Now().Unix()

	if count%100 == 0 || (now-atomic.LoadInt64(&p.lastGCTime) > 300) {
		// 检查当前活跃worker数量，避免在高负载时GC
		if atomic.LoadInt32(&p.activeWorkers) <= int32(p.maxWorkers/2) {
			runtime.GC()
			atomic.StoreInt64(&p.lastGCTime, now)
		}
	}
}

func (p *ConcurrentImageProcessor) processTask(task ProcessTask, workerID int) {
	atomic.AddInt32(&p.activeWorkers, 1)
	defer func() {
		atomic.AddInt32(&p.activeWorkers, -1)
		task.ImageData = nil
		p.tryGentleGC()
	}()

	select {
	case <-task.Context.Done():
		task.ErrorChan <- task.Context.Err()
		return
	default:
	}

	result, err := imaging.ProcessImageWithTiming(task.ImageData, task.Params)
	if err != nil {
		select {
		case task.ErrorChan <- err:
		case <-task.Context.Done():
		case <-p.ctx.Done():
		}
		return
	}

	select {
	case task.ResultChan <- result:
	case <-task.Context.Done():
	case <-p.ctx.Done():
	}
}

func (p *ConcurrentImageProcessor) ProcessAsync(ctx context.Context, imageData []byte, params imaging.ImageParams) (ProcessResult, error) {
	resultChan := make(chan ProcessResult, 1)
	errorChan := make(chan error, 1)

	task := ProcessTask{
		ImageData:  imageData,
		Params:     params,
		ResultChan: resultChan,
		ErrorChan:  errorChan,
		Context:    ctx,
	}

	select {
	case p.taskQueue <- task:
	case <-ctx.Done():
		return ProcessResult{}, ctx.Err()
	case <-p.ctx.Done():
		return ProcessResult{}, context.Canceled
	default:
		return p.processSynchronously(imageData, params)
	}
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errorChan:
		return ProcessResult{}, err
	case <-ctx.Done():
		return ProcessResult{}, ctx.Err()
	case <-p.ctx.Done():
		return ProcessResult{}, context.Canceled
	}
}

func (p *ConcurrentImageProcessor) processSynchronously(imageData []byte, params imaging.ImageParams) (ProcessResult, error) {
	return imaging.ProcessImageWithTiming(imageData, params)
}

func (p *ConcurrentImageProcessor) GetStats() ProcessorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return ProcessorStats{
		MaxWorkers:    p.maxWorkers,
		QueueLength:   len(p.taskQueue),
		MaxQueueSize:  p.maxQueueSize,
		ActiveWorkers: int(atomic.LoadInt32(&p.activeWorkers)),
	}
}

type ProcessorStats struct {
	MaxWorkers    int
	QueueLength   int
	MaxQueueSize  int
	ActiveWorkers int
}

func (p *ConcurrentImageProcessor) Close() error {
	p.cancel()
	p.wg.Wait()
	close(p.taskQueue)
	close(p.workerPool)
	return nil
}

func (p *ConcurrentImageProcessor) Resize(newMaxWorkers int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if newMaxWorkers <= 0 {
		newMaxWorkers = runtime.NumCPU() * 2
	}

	if newMaxWorkers > p.maxWorkers {
		for i := p.maxWorkers; i < newMaxWorkers; i++ {
			p.wg.Add(1)
			go p.worker(i)
		}
	}

	p.maxWorkers = newMaxWorkers
}
