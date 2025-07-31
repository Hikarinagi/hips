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

	var storageService service.StorageService
	var imageService service.ImageService

	if cfg.MultiCache.L1Enabled || cfg.MultiCache.L2Enabled || cfg.MultiCache.L3Enabled {
		cacheConfig := &cache.MultiCacheConfig{
			L1Enabled:        cfg.MultiCache.L1Enabled,
			L2Enabled:        cfg.MultiCache.L2Enabled,
			L3Enabled:        cfg.MultiCache.L3Enabled,
			L1MaxMemoryMB:    cfg.MultiCache.L1MaxMemoryMB,
			L2MaxMemoryMB:    cfg.MultiCache.L2MaxMemoryMB,
			L3MaxDiskGB:      cfg.MultiCache.L3MaxDiskGB,
			RedisAddr:        cfg.MultiCache.RedisAddr,
			RedisPassword:    cfg.MultiCache.RedisPassword,
			RedisDB:          cfg.MultiCache.RedisDB,
			DiskCacheDir:     cfg.MultiCache.DiskCacheDir,
			PromoteThreshold: cfg.MultiCache.PromoteThreshold,
			DemoteThreshold:  cfg.MultiCache.DemoteThreshold,
			SyncInterval:     cfg.MultiCache.SyncInterval,
		}
		multiCache, err := cache.NewMultiLevelCache(cacheConfig)
		if err != nil {
			log.Printf("Failed to create multi-level cache, falling back to simple cache: %v", err)
			storageService, err = service.NewR2StorageService(&cfg.R2, &cfg.Network, cacheService)
			if err != nil {
				log.Fatal("Failed to create storage service:", err)
			}
			imageService = service.NewImageService(storageService, cacheService, cfg.Concurrent)
		} else {
			storageService, err = service.NewR2StorageServiceWithMultiCache(&cfg.R2, &cfg.Network, multiCache)
			if err != nil {
				log.Fatal("Failed to create storage service with multi-cache:", err)
			}
			imageService = service.NewImageServiceWithMultiCache(storageService, multiCache, cfg.Concurrent)
			log.Printf("Multi-level cache enabled - L1: %v, L2: %v, L3: %v",
				cfg.MultiCache.L1Enabled, cfg.MultiCache.L2Enabled, cfg.MultiCache.L3Enabled)
		}
	} else {
		storageService, err = service.NewR2StorageService(&cfg.R2, &cfg.Network, cacheService)
		if err != nil {
			log.Fatal("Failed to create storage service:", err)
		}
		imageService = service.NewImageService(storageService, cacheService, cfg.Concurrent)
		log.Println("Using traditional single-level cache")
	}

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
