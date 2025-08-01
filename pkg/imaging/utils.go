package imaging

import (
	"crypto/md5"
	"fmt"
	"net/url"
	"strconv"
)

// ParseImageParams 解析URL参数为图片处理参数
func ParseImageParams(query url.Values) ImageParams {
	params := ImageParams{
		Quality: DefaultQuality,
		Format:  FormatJPEG,
		Crop:    CropFit,
		Gravity: GravityCenter,
		Blur:    0.0,
	}

	if w := query.Get("w"); w != "" {
		if width, err := strconv.Atoi(w); err == nil && width > 0 && width <= MaxWidth {
			params.Width = width
		}
	}

	if h := query.Get("h"); h != "" {
		if height, err := strconv.Atoi(h); err == nil && height > 0 && height <= MaxHeight {
			params.Height = height
		}
	}

	if q := query.Get("q"); q != "" {
		if quality, err := strconv.Atoi(q); err == nil && quality > 0 && quality <= 100 {
			params.Quality = quality
		}
	}

	if f := query.Get("f"); f != "" {
		if f == FormatWebP || f == FormatJPEG || f == FormatPNG || f == FormatAVIF {
			params.Format = f
		}
	}

	if c := query.Get("crop"); c != "" {
		if c == CropFit || c == CropFill || c == CropCrop {
			params.Crop = c
		}
	}

	if g := query.Get("gravity"); g != "" {
		params.Gravity = g
	}

	if b := query.Get("blur"); b != "" {
		if blur, err := strconv.ParseFloat(b, 64); err == nil && blur >= 0 && blur <= MaxBlur {
			params.Blur = blur
		}
	}

	return params
}

func GenerateCacheKey(imagePath string, params ImageParams) string {
	key := fmt.Sprintf("%s_%d_%d_%d_%s_%s_%s_%.1f",
		imagePath, params.Width, params.Height, params.Quality,
		params.Format, params.Crop, params.Gravity, params.Blur)

	hash := md5.Sum([]byte(key))
	return fmt.Sprintf("%x", hash)
}

func CalculateTargetSize(origW, origH, targetW, targetH int, crop string) (int, int) {
	if targetW == 0 && targetH == 0 {
		return origW, origH
	}

	if targetW == 0 {
		ratio := float64(targetH) / float64(origH)
		return int(float64(origW) * ratio), targetH
	}

	if targetH == 0 {
		ratio := float64(targetW) / float64(origW)
		return targetW, int(float64(origH) * ratio)
	}

	switch crop {
	case CropFill:
		scaleX := float64(targetW) / float64(origW)
		scaleY := float64(targetH) / float64(origH)
		scale := scaleX
		if scaleY > scaleX {
			scale = scaleY
		}
		return int(float64(origW) * scale), int(float64(origH) * scale)

	case CropCrop:
		return targetW, targetH

	default:
		scaleX := float64(targetW) / float64(origW)
		scaleY := float64(targetH) / float64(origH)
		scale := scaleX
		if scaleY < scaleX {
			scale = scaleY
		}
		return int(float64(origW) * scale), int(float64(origH) * scale)
	}
}
