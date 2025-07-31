package service

import (
	"context"

	"hips/pkg/concurrent"
	"hips/pkg/imaging"
)

type StorageService interface {
	GetImage(imagePath string) ([]byte, error)
}

type ImageService interface {
	ProcessImageRequest(imagePath string, params imaging.ImageParams) ([]byte, string, error)
	ProcessImageRequestWithContext(ctx context.Context, imagePath string, params imaging.ImageParams) ([]byte, string, error)
	ProcessBatch(ctx context.Context, request BatchProcessRequest) (BatchProcessResult, error)
	Close() error
	GetProcessorStats() concurrent.ProcessorStats
	GetMetrics() concurrent.Metrics
	AdjustWorkers(newWorkerCount int) error
}
