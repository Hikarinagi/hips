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

	imageService := service.NewImageService(storageService, cacheService)

	healthHandler := handler.NewHealthHandler(cacheService)
	imageHandler := handler.NewImageHandler(imageService, healthHandler)

	srv := server.NewServer(imageHandler, healthHandler)

	log.Printf("Using R2 endpoint: %s", cfg.R2.Endpoint)
	log.Printf("Using R2 bucket: %s", cfg.R2.Bucket)

	if err := srv.Start(cfg.Server.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}

	cleanup()
}

func cleanup() {
	vips.Shutdown()
}
