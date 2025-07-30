package service

import "hips/pkg/imaging"

type StorageService interface {
	GetImage(imagePath string) ([]byte, error)
}

type ImageService interface {
	ProcessImageRequest(imagePath string, params imaging.ImageParams) ([]byte, string, error)
}
