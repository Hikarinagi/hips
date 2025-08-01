package imaging

import (
	"fmt"
	"time"

	"github.com/h2non/bimg"
)

type ProcessResult struct {
	Data          []byte        `json:"-"`
	ContentType   string        `json:"content_type"`
	ProcessTime   time.Duration `json:"process_time"`
	ResizeSkipped bool          `json:"resize_skipped"`
}

func ProcessImage(imageData []byte, params ImageParams) ([]byte, string, error) {
	result, err := processImageInternal(imageData, params)
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
}

func ProcessImageWithTiming(imageData []byte, params ImageParams) (ProcessResult, error) {
	start := time.Now()
	result, err := processImageInternal(imageData, params)
	if err != nil {
		return ProcessResult{}, err
	}
	result.ProcessTime = time.Since(start)
	return result, nil
}

// processImageInternal 图片处理核心
func processImageInternal(imageData []byte, params ImageParams) (ProcessResult, error) {
	size, err := bimg.Size(imageData)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("failed to get image size: %w", err)
	}

	originalWidth := size.Width
	originalHeight := size.Height

	if params.Width == 0 && params.Height == 0 {
		params.Width = originalWidth
		params.Height = originalHeight
	}

	targetWidth, targetHeight := CalculateTargetSize(
		originalWidth, originalHeight,
		params.Width, params.Height,
		params.Crop,
	)

	shouldResize := true

	// 判断是否需要处理
	if params.Width > 0 && params.Height == 0 {
		if params.Width >= originalWidth {
			shouldResize = false
		}
	} else if params.Width == 0 && params.Height > 0 {
		if params.Height >= originalHeight {
			shouldResize = false
		}
	} else if params.Width > 0 && params.Height > 0 {
		switch params.Crop {
		case CropFit:
			if params.Width >= originalWidth || params.Height >= originalHeight {
				shouldResize = false
			}
		case CropFill, CropCrop:
			if params.Width >= originalWidth && params.Height >= originalHeight {
				shouldResize = false
			}
		}
	}

	// 构建 bimg 选项
	options := bimg.Options{
		Quality:       params.Quality,
		StripMetadata: true,
	}

	// 设置输出格式和性能优化
	var contentType string
	switch params.Format {
	case FormatWebP:
		options.Type = bimg.WEBP
		contentType = "image/webp"
		options.Speed = 6
	case FormatAVIF:
		options.Type = bimg.AVIF
		contentType = "image/avif"
		options.Speed = 7
	case FormatPNG:
		options.Type = bimg.PNG
		contentType = "image/png"
		options.Speed = 6
	default:
		options.Type = bimg.JPEG
		contentType = "image/jpeg"
	}

	if shouldResize {
		options.Width = targetWidth
		options.Height = targetHeight

		switch params.Crop {
		case CropFit:
			options.Force = false
		case CropFill:
			options.Crop = true
			options.Force = true
		case CropCrop:
			options.Crop = true
			options.Gravity = bimg.GravitySmart
			options.Force = true
		default:
			options.Force = false
		}
	}

	if params.Blur > 0 {
		options.GaussianBlur = bimg.GaussianBlur{
			Sigma: params.Blur,
		}
	}

	outputData, err := bimg.NewImage(imageData).Process(options)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("failed to process image: %w", err)
	}

	return ProcessResult{
		Data:          outputData,
		ContentType:   contentType,
		ResizeSkipped: !shouldResize,
	}, nil
}
