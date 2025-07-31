package service

import (
	"fmt"
	"io"
	"net"
	"net/http"
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

func NewR2StorageService(cfg *config.R2Config, networkCfg *config.NetworkConfig, cacheService cache.CacheService) (*R2StorageService, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			// 连接池配置
			MaxIdleConns:        networkCfg.MaxIdleConns,
			MaxIdleConnsPerHost: networkCfg.MaxIdleConnsPerHost,
			MaxConnsPerHost:     networkCfg.MaxConnsPerHost,

			// 连接超时配置
			DialContext: (&net.Dialer{
				Timeout:   networkCfg.DialTimeout,
				KeepAlive: networkCfg.KeepAlive,
			}).DialContext,

			// 长连接配置
			IdleConnTimeout:       networkCfg.IdleConnTimeout,
			TLSHandshakeTimeout:   10 * time.Second, // TLS握手超时
			ExpectContinueTimeout: 1 * time.Second,  // Expect Continue超时

			// 压缩配置
			DisableCompression: networkCfg.DisableCompression,
		},
		Timeout: networkCfg.RequestTimeout,
	}

	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String("auto"),
		Endpoint:         aws.String(cfg.Endpoint),
		S3ForcePathStyle: aws.Bool(true),
		Credentials:      credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, ""),
		HTTPClient:       httpClient, // 使用优化的HTTP客户端
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
	result, err := s.GetImageWithTiming(imagePath)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (s *R2StorageService) GetImageWithTiming(imagePath string) (StorageResult, error) {
	start := time.Now()
	cacheKey := "raw_" + imagePath

	if cached, found := s.cache.Get(cacheKey); found {
		return StorageResult{
			Data:        cached.([]byte),
			NetworkTime: time.Since(start),
			CacheHit:    true,
		}, nil
	}

	networkStart := time.Now()
	result, err := s.s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(imagePath),
	})
	if err != nil {
		return StorageResult{}, fmt.Errorf("failed to get image from R2: %w", err)
	}
	defer result.Body.Close()

	imageData, err := io.ReadAll(result.Body)
	networkTime := time.Since(networkStart)

	if err != nil {
		return StorageResult{}, fmt.Errorf("failed to read image data: %w", err)
	}

	s.cache.Set(cacheKey, imageData, 1*time.Hour)

	return StorageResult{
		Data:        imageData,
		NetworkTime: networkTime,
		CacheHit:    false,
	}, nil
}
