package main

import (
	"log"

	"github.com/davidbyttow/govips/v2/vips"

	"hips/internal/cache"
	"hips/internal/config"
	"hips/internal/handler"
	"hips/internal/server"
	"hips/internal/service"
)

func init() {
	vips.Startup(nil)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	cacheService := cache.NewMemoryCache(cfg.Cache.TTL, cfg.Cache.Cleanup)

	storageService, err := service.NewR2StorageService(&cfg.R2, cacheService)
	if err != nil {
		log.Fatal("Failed to create storage service:", err)
	}

	imageService := service.NewImageService(storageService, cacheService, cfg.Concurrent)

	healthHandler := handler.NewHealthHandler(cacheService)
	healthHandler.SetImageService(imageService)
	imageHandler := handler.NewImageHandler(imageService, healthHandler)

	srv := server.NewServer(imageHandler, healthHandler)

	log.Printf("Using R2 endpoint: %s", cfg.R2.Endpoint)
	log.Printf("Using R2 bucket: %s", cfg.R2.Bucket)
	log.Printf("Concurrent processing enabled: %v", cfg.Concurrent.EnableAsync)
	log.Printf("Max workers: %d", cfg.Concurrent.MaxWorkers)
	log.Printf("Max queue size: %d", cfg.Concurrent.MaxQueueSize)

	defer func() {
		log.Println("Shutting down image service...")
		if err := imageService.Close(); err != nil {
			log.Printf("Error closing image service: %v", err)
		}
		cleanup()
	}()

	if err := srv.Start(cfg.Server.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func cleanup() {
	vips.Shutdown()
}
