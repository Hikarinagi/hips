package service

import (
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"hips/internal/cache"
	"hips/internal/config"
)

type R2StorageService struct {
	s3Client *s3.S3
	bucket   string
	cache    cache.CacheService
}

func NewR2StorageService(cfg *config.R2Config, cacheService cache.CacheService) (*R2StorageService, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String("auto"),
		Endpoint:         aws.String(cfg.Endpoint),
		S3ForcePathStyle: aws.Bool(true),
		Credentials:      credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &R2StorageService{
		s3Client: s3.New(sess),
		bucket:   cfg.Bucket,
		cache:    cacheService,
	}, nil
}

func (s *R2StorageService) GetImage(imagePath string) ([]byte, error) {
	cacheKey := "raw_" + imagePath
	if cached, found := s.cache.Get(cacheKey); found {
		return cached.([]byte), nil
	}

	result, err := s.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(imagePath),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get image from R2: %w", err)
	}
	defer result.Body.Close()

	imageData, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	s.cache.Set(cacheKey, imageData, 1*time.Hour)

	return imageData, nil
}
