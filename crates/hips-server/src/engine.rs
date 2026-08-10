use hips_core::{Codec, Focus, ImageParams, Interest, Plan, Rgba};
use libvips::ops::{
    Angle, EmbedOptions, Extend, FlattenOptions, ForeignKeep, GammaOptions, Interesting,
    Interpretation, JpegsaveBufferOptions, LinearOptions, PngsaveBufferOptions, SharpenOptions,
    Size, ThumbnailBufferOptions, WebpsaveBufferOptions,
};
use libvips::{ops, VipsApp, VipsImage};

const VIPS_DIM_MAX: i32 = 10_000_000;

#[derive(Debug, thiserror::Error)]
pub enum EngineError {
    #[error("failed to initialize libvips")]
    Init,
    #[error("source image too large: {pixels} pixels")]
    SourceTooLarge { pixels: u64 },
    #[error("unsupported or corrupt image")]
    Decode,
    #[error("image processing failed: {0}")]
    Vips(String),
}

#[derive(Debug, Clone, Copy)]
pub struct EncodeConfig {
    pub webp_effort: i32,
    pub avif_effort: i32,
    pub png_compression: i32,
}

impl Default for EncodeConfig {
    fn default() -> Self {
        EncodeConfig {
            webp_effort: 2,
            avif_effort: 1,
            png_compression: 6,
        }
    }
}

#[derive(Debug)]
pub struct Transformed {
    pub bytes: Vec<u8>,
    pub codec: Codec,
}

pub struct Engine {
    app: VipsApp,
    max_pixels: u64,
    encode: EncodeConfig,
}

impl Engine {
    pub fn init(
        concurrency: i32,
        max_pixels: u64,
        encode: EncodeConfig,
    ) -> Result<Engine, EngineError> {
        let app = VipsApp::new("hips", false).map_err(|_| EngineError::Init)?;
        app.cache_set_max(0);
        app.cache_set_max_mem(0);
        app.concurrency_set(concurrency.max(1));
        Ok(Engine {
            app,
            max_pixels,
            encode,
        })
    }

    pub fn process(
        &self,
        src: &[u8],
        params: &ImageParams,
        codec: Codec,
        focus: Option<Focus>,
    ) -> Result<Transformed, EngineError> {
        let probe = VipsImage::new_from_buffer(src, "").map_err(|_| EngineError::Decode)?;
        let src_w = probe.get_width().max(0) as u64;
        let src_h = probe.get_height().max(0) as u64;
        drop(probe);
        if src_w == 0 || src_h == 0 {
            return Err(EngineError::Decode);
        }
        if src_w * src_h > self.max_pixels {
            return Err(EngineError::SourceTooLarge {
                pixels: src_w * src_h,
            });
        }

        let plan = params.plan(src_w as u32, src_h as u32, focus);
        self.run(src, &plan, params, codec)
            .map_err(|()| self.vips_err())
    }

    fn run(
        &self,
        src: &[u8],
        plan: &Plan,
        params: &ImageParams,
        codec: Codec,
    ) -> Result<Transformed, ()> {
        let mut img = geometry(src, plan).map_err(|_| ())?;

        if params.trim {
            img = trim(&img).map_err(|_| ())?;
        }
        if let Some(v) = params.gamma {
            img = ops::gamma_with_opts(
                &img,
                &GammaOptions {
                    exponent: v.clamp(0.01, 10.0),
                },
            )
            .map_err(|_| ())?;
        }
        if let Some(v) = params.brightness {
            img = brightness(&img, v).map_err(|_| ())?;
        }
        if let Some(v) = params.contrast {
            img = contrast(&img, v).map_err(|_| ())?;
        }
        if let Some(v) = params.saturation {
            img = saturation(&img, v).map_err(|_| ())?;
        }
        if let Some(v) = params.sharpen {
            if v > 0.0 {
                img = ops::sharpen_with_opts(
                    &img,
                    &SharpenOptions {
                        sigma: v.clamp(0.1, 10.0),
                        ..Default::default()
                    },
                )
                .map_err(|_| ())?;
            }
        }
        if params.blur > 0.0 {
            img = ops::gaussblur(&img, params.blur).map_err(|_| ())?;
        }
        if let Some(angle) = rotation(params.rotate) {
            img = ops::rot(&img, angle).map_err(|_| ())?;
        }

        let bytes = encode(
            &img,
            codec,
            params.quality as i32,
            params.background,
            &self.encode,
        )
        .map_err(|_| ())?;

        Ok(Transformed { bytes, codec })
    }

    fn vips_err(&self) -> EngineError {
        let buffer = self.app.error_buffer().unwrap_or("").trim().to_string();
        self.app.error_clear();
        let message = if buffer.is_empty() {
            "libvips operation failed".to_string()
        } else {
            buffer
        };
        tracing::error!(error = %message, "image processing failed");
        EngineError::Vips(message)
    }
}

fn geometry(src: &[u8], plan: &Plan) -> libvips::Result<VipsImage> {
    match *plan {
        Plan::Fit {
            w,
            h,
            allow_upscale,
        } => ops::thumbnail_buffer_with_opts(
            src,
            w.map(|v| v as i32).unwrap_or(VIPS_DIM_MAX),
            &ThumbnailBufferOptions {
                height: h.map(|v| v as i32).unwrap_or(VIPS_DIM_MAX),
                size: size_mode(allow_upscale),
                crop: Interesting::None,
                ..Default::default()
            },
        ),
        Plan::SmartCover {
            w,
            h,
            interest,
            allow_upscale,
        } => ops::thumbnail_buffer_with_opts(
            src,
            w as i32,
            &ThumbnailBufferOptions {
                height: h as i32,
                size: size_mode(allow_upscale),
                crop: match interest {
                    Interest::Centre => Interesting::Centre,
                    Interest::Attention => Interesting::Attention,
                },
                ..Default::default()
            },
        ),
        Plan::DirectionalCover {
            resize_w,
            resize_h,
            x,
            y,
            w,
            h,
        } => {
            let resized = ops::thumbnail_buffer_with_opts(
                src,
                resize_w as i32,
                &ThumbnailBufferOptions {
                    height: resize_h as i32,
                    size: Size::Both,
                    crop: Interesting::None,
                    ..Default::default()
                },
            )?;
            let aw = resized.get_width();
            let ah = resized.get_height();
            let cw = (w as i32).min(aw);
            let ch = (h as i32).min(ah);
            let cx = (x as i32).clamp(0, (aw - cw).max(0));
            let cy = (y as i32).clamp(0, (ah - ch).max(0));
            ops::extract_area(&resized, cx, cy, cw, ch)
        }
        Plan::Pad {
            canvas_w,
            canvas_h,
            allow_upscale,
            ..
        } => {
            let fitted = ops::thumbnail_buffer_with_opts(
                src,
                canvas_w as i32,
                &ThumbnailBufferOptions {
                    height: canvas_h as i32,
                    size: size_mode(allow_upscale),
                    crop: Interesting::None,
                    ..Default::default()
                },
            )?;
            let aw = fitted.get_width();
            let ah = fitted.get_height();
            let ox = ((canvas_w as i32 - aw) / 2).max(0);
            let oy = ((canvas_h as i32 - ah) / 2).max(0);
            ops::embed_with_opts(
                &fitted,
                ox,
                oy,
                canvas_w as i32,
                canvas_h as i32,
                &EmbedOptions {
                    extend: Extend::Background,
                    background: pad_background(&fitted),
                },
            )
        }
    }
}

fn size_mode(allow_upscale: bool) -> Size {
    if allow_upscale {
        Size::Both
    } else {
        Size::Down
    }
}

fn rotation(degrees: u16) -> Option<Angle> {
    match degrees {
        90 => Some(Angle::D90),
        180 => Some(Angle::D180),
        270 => Some(Angle::D270),
        _ => None,
    }
}

fn trim(img: &VipsImage) -> libvips::Result<VipsImage> {
    let (left, top, width, height) = ops::find_trim(img)?;
    if width <= 0 || height <= 0 {
        return ops::extract_area(img, 0, 0, img.get_width(), img.get_height());
    }
    ops::extract_area(img, left, top, width, height)
}

fn brightness(img: &VipsImage, factor: f64) -> libvips::Result<VipsImage> {
    let mut a = band_coeffs(img, factor, 1.0);
    let mut b = vec![0.0; a.len()];
    ops::linear_with_opts(img, &mut a, &mut b, &LinearOptions { uchar: true })
}

fn contrast(img: &VipsImage, c: f64) -> libvips::Result<VipsImage> {
    let has_alpha = img.image_hasalpha();
    let color = if has_alpha {
        img.get_bands() - 1
    } else {
        img.get_bands()
    };
    let mut a = vec![c; color.max(0) as usize];
    let mut b = vec![128.0 * (1.0 - c); color.max(0) as usize];
    if has_alpha {
        a.push(1.0);
        b.push(0.0);
    }
    ops::linear_with_opts(img, &mut a, &mut b, &LinearOptions { uchar: true })
}

fn saturation(img: &VipsImage, s: f64) -> libvips::Result<VipsImage> {
    let lch = ops::colourspace(img, Interpretation::Lch)?;
    let bands = lch.get_bands().max(0) as usize;
    let mut a = vec![1.0; bands];
    if bands >= 2 {
        a[1] = s.max(0.0);
    }
    let mut b = vec![0.0; bands];
    let scaled = ops::linear(&lch, &mut a, &mut b)?;
    ops::colourspace(&scaled, Interpretation::Srgb)
}

fn band_coeffs(img: &VipsImage, color: f64, alpha: f64) -> Vec<f64> {
    let bands = img.get_bands().max(0);
    if img.image_hasalpha() {
        let mut a = vec![color; (bands - 1).max(0) as usize];
        a.push(alpha);
        a
    } else {
        vec![color; bands as usize]
    }
}

fn pad_background(img: &VipsImage) -> Vec<f64> {
    if img.image_hasalpha() {
        vec![0.0, 0.0, 0.0, 0.0]
    } else {
        vec![255.0, 255.0, 255.0]
    }
}

fn encode(
    img: &VipsImage,
    codec: Codec,
    quality: i32,
    background: Option<Rgba>,
    cfg: &EncodeConfig,
) -> Result<Vec<u8>, ()> {
    match codec {
        Codec::Jpeg => {
            let opts = JpegsaveBufferOptions {
                q: quality,
                optimize_coding: true,
                interlace: true,
                keep: ForeignKeep::None,
                ..Default::default()
            };
            if img.image_hasalpha() {
                let flat = flatten(img, background).map_err(|_| ())?;
                ops::jpegsave_buffer_with_opts(&flat, &opts).map_err(|_| ())
            } else {
                ops::jpegsave_buffer_with_opts(img, &opts).map_err(|_| ())
            }
        }
        Codec::Png => ops::pngsave_buffer_with_opts(
            img,
            &PngsaveBufferOptions {
                compression: cfg.png_compression,
                keep: ForeignKeep::None,
                ..Default::default()
            },
        )
        .map_err(|_| ()),
        Codec::Webp => ops::webpsave_buffer_with_opts(
            img,
            &WebpsaveBufferOptions {
                q: quality,
                effort: cfg.webp_effort,
                keep: ForeignKeep::None,
                ..Default::default()
            },
        )
        .map_err(|_| ()),
        Codec::Avif => encode_avif(img, quality, (9 - cfg.avif_effort).clamp(0, 10) as u8),
        Codec::Gif => ops::pngsave_buffer_with_opts(
            img,
            &PngsaveBufferOptions {
                compression: cfg.png_compression,
                keep: ForeignKeep::None,
                ..Default::default()
            },
        )
        .map_err(|_| ()),
    }
}

fn encode_avif(img: &VipsImage, quality: i32, speed: u8) -> Result<Vec<u8>, ()> {
    let srgb = ops::colourspace(img, Interpretation::Srgb).map_err(|_| ())?;
    let width = srgb.get_width();
    let height = srgb.get_height();
    if width <= 0 || height <= 0 {
        return Err(());
    }
    let pixels = srgb.image_write_to_memory();
    let rgb = libavif::RgbPixels::new(width as u32, height as u32, &pixels).map_err(|_| ())?;
    let image = rgb.to_image(libavif::YuvFormat::Yuv420);
    let mut encoder = libavif::Encoder::new();
    encoder.set_max_threads(1);
    encoder.set_speed(speed);
    encoder.set_quality(quality.clamp(0, 100) as u8);
    encoder
        .encode(&image)
        .map(|data| data.as_ref().to_vec())
        .map_err(|_| ())
}

fn flatten(img: &VipsImage, background: Option<Rgba>) -> libvips::Result<VipsImage> {
    let c = background.filter(|c| c.is_opaque()).unwrap_or(Rgba::WHITE);
    ops::flatten_with_opts(
        img,
        &FlattenOptions {
            background: vec![c.r as f64, c.g as f64, c.b as f64],
            max_alpha: 255.0,
        },
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use hips_core::ImageParams;

    fn params(pairs: &[(&str, &str)]) -> ImageParams {
        ImageParams::from_pairs(pairs.iter().copied())
    }

    fn srgb_png(w: i32, h: i32) -> Vec<u8> {
        let base = ops::black(w, h).expect("black");
        let rgb = ops::colourspace(&base, Interpretation::Srgb).expect("srgb");
        ops::pngsave_buffer(&rgb).expect("encode source png")
    }

    fn dimensions(bytes: &[u8]) -> (i32, i32) {
        let img = VipsImage::new_from_buffer(bytes, "").expect("decode output");
        (img.get_width(), img.get_height())
    }

    #[test]
    fn pipeline() {
        let engine = Engine::init(1, 2_000, EncodeConfig::default()).expect("init libvips");
        let src = srgb_png(40, 30);

        let cover = engine
            .process(
                &src,
                &params(&[("w", "20"), ("h", "20"), ("fit", "cover"), ("f", "webp")]),
                Codec::Webp,
                None,
            )
            .expect("cover webp");
        assert_eq!(cover.codec, Codec::Webp);
        assert_eq!(dimensions(&cover.bytes), (20, 20));

        let avif = engine
            .process(
                &src,
                &params(&[("w", "24"), ("h", "24"), ("fit", "cover"), ("f", "avif")]),
                Codec::Avif,
                None,
            )
            .expect("cover avif");
        assert_eq!(avif.codec, Codec::Avif);
        assert_eq!(dimensions(&avif.bytes), (24, 24));

        let scaled_down = engine
            .process(
                &src,
                &params(&[("w", "200"), ("fit", "scale-down"), ("f", "png")]),
                Codec::Png,
                None,
            )
            .expect("scale-down png");
        assert_eq!(dimensions(&scaled_down.bytes).0, 40);

        let contain = engine
            .process(
                &src,
                &params(&[("w", "32"), ("h", "32"), ("fit", "contain"), ("f", "jpeg")]),
                Codec::Jpeg,
                None,
            )
            .expect("contain jpeg");
        let (cw, ch) = dimensions(&contain.bytes);
        assert!(cw <= 32 && ch <= 32 && (cw == 32 || ch == 32));

        let padded = engine
            .process(
                &src,
                &params(&[("w", "44"), ("h", "44"), ("fit", "pad"), ("f", "png")]),
                Codec::Png,
                None,
            )
            .expect("pad png");
        assert_eq!(dimensions(&padded.bytes), (44, 44));

        let directional = engine
            .process(
                &src,
                &params(&[
                    ("w", "30"),
                    ("h", "10"),
                    ("fit", "cover"),
                    ("gravity", "top"),
                ]),
                Codec::Webp,
                None,
            )
            .expect("directional cover");
        assert_eq!(dimensions(&directional.bytes), (30, 10));

        let oversized = srgb_png(64, 48);
        let err = engine
            .process(&oversized, &params(&[("w", "10")]), Codec::Png, None)
            .unwrap_err();
        assert!(matches!(err, EngineError::SourceTooLarge { .. }));
    }
}
