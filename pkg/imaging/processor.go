package imaging

import (
	"fmt"
	"math"

	"github.com/davidbyttow/govips/v2/vips"
)

// ProcessImage 处理图片
func ProcessImage(imageData []byte, params ImageParams) ([]byte, string, error) {
	image, err := vips.NewImageFromBuffer(imageData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load image: %w", err)
	}
	defer image.Close()

	originalWidth := image.Width()
	originalHeight := image.Height()

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

	// 如果只指定了宽度
	if params.Width > 0 && params.Height == 0 {
		if params.Width >= originalWidth {
			shouldResize = false
		}
	} else if params.Width == 0 && params.Height > 0 {
		// 如果只指定了高度
		if params.Height >= originalHeight {
			shouldResize = false
		}
	} else if params.Width > 0 && params.Height > 0 {
		// 如果同时指定了宽高，根据裁剪模式判断
		switch params.Crop {
		case CropFit:
			// fit模式：只要任一维度大于等于原图，就不需要处理
			if params.Width >= originalWidth || params.Height >= originalHeight {
				shouldResize = false
			}
		case CropFill, CropCrop:
			// fill/crop模式：两个维度都大于等于原图才跳过
			if params.Width >= originalWidth && params.Height >= originalHeight {
				shouldResize = false
			}
		}
	}

	if shouldResize {
		switch params.Crop {
		case CropFit:
			scale := float64(targetWidth) / float64(originalWidth)
			err = image.Resize(scale, vips.KernelLanczos3)
		case CropFill:
			scaleX := float64(targetWidth) / float64(originalWidth)
			scaleY := float64(targetHeight) / float64(originalHeight)
			scale := math.Max(scaleX, scaleY)

			err = image.Resize(scale, vips.KernelLanczos3)
			if err == nil {
				currentWidth := image.Width()
				currentHeight := image.Height()
				left := (currentWidth - targetWidth) / 2
				top := (currentHeight - targetHeight) / 2
				err = image.ExtractArea(left, top, targetWidth, targetHeight)
			}
		case CropCrop:
			err = image.SmartCrop(targetWidth, targetHeight, vips.InterestingAttention)
		default:
			scale := float64(targetWidth) / float64(originalWidth)
			err = image.Resize(scale, vips.KernelLanczos3)
		}

		if err != nil {
			return nil, "", fmt.Errorf("failed to process image: %w", err)
		}
	}

	if params.Blur > 0 {
		err = image.GaussianBlur(params.Blur)
		if err != nil {
			return nil, "", fmt.Errorf("failed to blur image: %w", err)
		}
	}

	var outputData []byte
	var contentType string

	switch params.Format {
	case FormatWebP:
		webpParams := vips.NewWebpExportParams()
		webpParams.Quality = params.Quality
		webpParams.StripMetadata = true
		outputData, _, err = image.ExportWebp(webpParams)
		contentType = "image/webp"
	case FormatAVIF:
		avifParams := vips.NewAvifExportParams()
		avifParams.Quality = params.Quality
		avifParams.StripMetadata = true
		avifParams.Effort = 1
		avifParams.Lossless = false
		outputData, _, err = image.ExportAvif(avifParams)
		contentType = "image/avif"
	case FormatPNG:
		pngParams := vips.NewPngExportParams()
		pngParams.StripMetadata = true
		outputData, _, err = image.ExportPng(pngParams)
		contentType = "image/png"
	default:
		jpegParams := vips.NewJpegExportParams()
		jpegParams.Quality = params.Quality
		jpegParams.StripMetadata = true
		outputData, _, err = image.ExportJpeg(jpegParams)
		contentType = "image/jpeg"
	}

	if err != nil {
		return nil, "", fmt.Errorf("failed to export image: %w", err)
	}

	return outputData, contentType, nil
}
