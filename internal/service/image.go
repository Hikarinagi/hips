package service

import (
	"sync"

	"hips/internal/cache"
	"hips/internal/config"
	"hips/pkg/imaging"
)

type ImageServiceImpl struct {
	storageService StorageService
	cache          cache.CacheService
	mu             sync.RWMutex
}

func NewImageService(storageService StorageService, cacheService cache.CacheService) *ImageServiceImpl {
	return &ImageServiceImpl{
		storageService: storageService,
		cache:          cacheService,
	}
}

func (s *ImageServiceImpl) ProcessImageRequest(imagePath string, params imaging.ImageParams) ([]byte, string, error) {
	cacheKey := imaging.GenerateCacheKey(imagePath, params)
	if cached, found := s.cache.Get(cacheKey); found {
		cachedImage := cached.(cache.CachedImage)
		return cachedImage.Data, cachedImage.ContentType, nil
	}

	imageData, err := s.storageService.GetImage(imagePath)
	if err != nil {
		return nil, "", err
	}

	processedData, contentType, err := imaging.ProcessImage(imageData, params)
	if err != nil {
		return nil, "", err
	}

	cachedImage := cache.CachedImage{
		Data:        processedData,
		ContentType: contentType,
	}
	s.cache.Set(cacheKey, cachedImage, config.CacheTTL)

	return processedData, contentType, nil
}
