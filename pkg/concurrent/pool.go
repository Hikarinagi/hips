package concurrent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type WorkerPool struct {
	workers        chan *Worker
	taskQueue      chan Task
	maxWorkers     int
	activeCount    int64
	totalTasks     int64
	completedTasks int64
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

type Worker struct {
	id       int
	pool     *WorkerPool
	taskChan chan Task
	quit     chan bool
}

type Task interface {
	Execute(ctx context.Context) error
	GetID() string
	GetPriority() int
}

type ImageProcessingTask struct {
	ID         string
	ImageData  []byte
	Params     interface{}
	ResultChan chan interface{}
	ErrorChan  chan error
	Priority   int
	CreatedAt  time.Time
	Context    context.Context
}

func (t *ImageProcessingTask) Execute(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (t *ImageProcessingTask) GetID() string {
	return t.ID
}

func (t *ImageProcessingTask) GetPriority() int {
	return t.Priority
}

func NewWorkerPool(maxWorkers int, queueSize int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		workers:    make(chan *Worker, maxWorkers),
		taskQueue:  make(chan Task, queueSize),
		maxWorkers: maxWorkers,
		ctx:        ctx,
		cancel:     cancel,
	}

	for i := 0; i < maxWorkers; i++ {
		worker := &Worker{
			id:       i,
			pool:     pool,
			taskChan: make(chan Task),
			quit:     make(chan bool),
		}
		pool.workers <- worker
		pool.wg.Add(1)
		go worker.start()
	}

	return pool
}

func (p *WorkerPool) Submit(task Task) error {
	select {
	case p.taskQueue <- task:
		atomic.AddInt64(&p.totalTasks, 1)
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return ErrQueueFull
	}
}

func (p *WorkerPool) SubmitWithTimeout(task Task, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	select {
	case p.taskQueue <- task:
		atomic.AddInt64(&p.totalTasks, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) start() {
	defer w.pool.wg.Done()

	for {
		w.pool.workers <- w

		select {
		case task := <-w.pool.taskQueue:
			atomic.AddInt64(&w.pool.activeCount, 1)

			err := task.Execute(w.pool.ctx)
			if err != nil {
			}

			atomic.AddInt64(&w.pool.activeCount, -1)
			atomic.AddInt64(&w.pool.completedTasks, 1)

		case <-w.quit:
			return
		case <-w.pool.ctx.Done():
			return
		}
	}
}

func (p *WorkerPool) GetStats() PoolStats {
	return PoolStats{
		MaxWorkers:     p.maxWorkers,
		ActiveWorkers:  int(atomic.LoadInt64(&p.activeCount)),
		QueueLength:    len(p.taskQueue),
		TotalTasks:     atomic.LoadInt64(&p.totalTasks),
		CompletedTasks: atomic.LoadInt64(&p.completedTasks),
	}
}

type PoolStats struct {
	MaxWorkers     int
	ActiveWorkers  int
	QueueLength    int
	TotalTasks     int64
	CompletedTasks int64
}

func (p *WorkerPool) Close() {
	p.cancel()

	for i := 0; i < p.maxWorkers; i++ {
		worker := <-p.workers
		worker.quit <- true
	}

	p.wg.Wait()
	close(p.taskQueue)
	close(p.workers)
}

func (p *WorkerPool) Resize(newSize int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if newSize <= 0 {
		return ErrInvalidWorkerCount
	}

	currentSize := p.maxWorkers

	if newSize > currentSize {
		for i := currentSize; i < newSize; i++ {
			worker := &Worker{
				id:       i,
				pool:     p,
				taskChan: make(chan Task),
				quit:     make(chan bool),
			}
			p.workers <- worker
			p.wg.Add(1)
			go worker.start()
		}
	} else if newSize < currentSize {
		for i := newSize; i < currentSize; i++ {
			worker := <-p.workers
			worker.quit <- true
		}
	}

	p.maxWorkers = newSize
	return nil
}

func (p *WorkerPool) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
}

func (p *WorkerPool) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
}
