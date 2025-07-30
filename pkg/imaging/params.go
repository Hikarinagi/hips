package imaging

type ImageParams struct {
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Quality int     `json:"quality"`
	Format  string  `json:"format"`
	Crop    string  `json:"crop"`
	Gravity string  `json:"gravity"`
	Blur    float64 `json:"blur"`
}

const (
	DefaultQuality = 85
	MaxWidth       = 2048
	MaxHeight      = 2048
	MaxBlur        = 100.0

	FormatJPEG = "jpeg"
	FormatPNG  = "png"
	FormatWebP = "webp"
	FormatAVIF = "avif"

	CropFit  = "fit"
	CropFill = "fill"
	CropCrop = "crop"

	GravityCenter = "center"
	GravityNorth  = "north"
	GravitySouth  = "south"
	GravityEast   = "east"
	GravityWest   = "west"
)

func (p ImageParams) IsValidFormat() bool {
	switch p.Format {
	case FormatJPEG, FormatPNG, FormatWebP, FormatAVIF:
		return true
	default:
		return false
	}
}

func (p ImageParams) IsValidCrop() bool {
	switch p.Crop {
	case CropFit, CropFill, CropCrop:
		return true
	default:
		return false
	}
}

func (p ImageParams) IsValidSize() bool {
	return (p.Width == 0 || (p.Width > 0 && p.Width <= MaxWidth)) &&
		(p.Height == 0 || (p.Height > 0 && p.Height <= MaxHeight))
}

func (p ImageParams) IsValidQuality() bool {
	return p.Quality > 0 && p.Quality <= 100
}

func (p ImageParams) IsValidBlur() bool {
	return p.Blur >= 0 && p.Blur <= MaxBlur
}
