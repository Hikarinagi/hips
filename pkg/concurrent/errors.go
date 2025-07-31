package concurrent

import "errors"

var (
	ErrQueueFull          = errors.New("task queue is full")
	ErrWorkerPoolClosed   = errors.New("worker pool is closed")
	ErrInvalidWorkerCount = errors.New("invalid worker count")
	ErrTaskTimeout        = errors.New("task execution timeout")
	ErrProcessorBusy      = errors.New("processor is busy")
	ErrInvalidTask        = errors.New("invalid task")
	ErrContextCanceled    = errors.New("context was canceled")
)
