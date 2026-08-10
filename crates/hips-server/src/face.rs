use std::path::Path;
use std::sync::{Condvar, Mutex};
use std::time::Duration;

use hips_core::Focus;
use libvips::ops::{
    BandFormat, EmbedOptions, Extend, ExtractBandOptions, Interpretation, ThumbnailBufferOptions,
};
use libvips::{ops, VipsImage};
use moka::future::Cache;
use ort::session::builder::GraphOptimizationLevel;
use ort::session::Session;
use ort::value::Tensor;

const STRIDE: i32 = 32;

#[derive(Debug, thiserror::Error)]
#[error("failed to load face model {path}: {message}")]
pub struct FaceError {
    path: String,
    message: String,
}

pub struct FaceDetector {
    idle: Mutex<Vec<Session>>,
    released: Condvar,
    infer_size: i32,
    threshold: f32,
}

struct Input {
    shape: Vec<i64>,
    data: Vec<f32>,
    width: f64,
    height: f64,
}

struct Lease<'a> {
    detector: &'a FaceDetector,
    session: Option<Session>,
}

impl Drop for Lease<'_> {
    fn drop(&mut self) {
        if let Some(session) = self.session.take() {
            if let Ok(mut idle) = self.detector.idle.lock() {
                idle.push(session);
            }
            self.detector.released.notify_one();
        }
    }
}

impl FaceDetector {
    pub fn load(
        path: &Path,
        infer_size: i32,
        threshold: f32,
        threads: usize,
        sessions: usize,
    ) -> Result<FaceDetector, FaceError> {
        let build = || -> Result<Session, String> {
            let builder = Session::builder().map_err(|e| e.to_string())?;
            let builder = builder
                .with_optimization_level(GraphOptimizationLevel::Level3)
                .map_err(|e| e.to_string())?;
            let builder = builder
                .with_intra_threads(threads.max(1))
                .map_err(|e| e.to_string())?;
            let mut builder = builder.with_inter_threads(1).map_err(|e| e.to_string())?;
            builder.commit_from_file(path).map_err(|e| e.to_string())
        };
        let mut idle = Vec::with_capacity(sessions.max(1));
        for _ in 0..sessions.max(1) {
            idle.push(build().map_err(|message| FaceError {
                path: path.display().to_string(),
                message,
            })?);
        }
        Ok(FaceDetector {
            idle: Mutex::new(idle),
            released: Condvar::new(),
            infer_size: infer_size.max(STRIDE),
            threshold,
        })
    }

    fn acquire(&self) -> Option<Lease<'_>> {
        let mut idle = self.idle.lock().ok()?;
        while idle.is_empty() {
            idle = self.released.wait(idle).ok()?;
        }
        Some(Lease {
            detector: self,
            session: idle.pop(),
        })
    }

    pub fn detect(&self, src: &[u8]) -> Option<Focus> {
        let input = self.prepare(src)?;
        let tensor = Tensor::from_array((input.shape, input.data)).ok()?;
        let mut lease = self.acquire()?;
        let outputs = lease
            .session
            .as_mut()?
            .run(ort::inputs!["images" => tensor])
            .ok()?;
        let (shape, data) = outputs.get("output0")?.try_extract_tensor::<f32>().ok()?;
        if shape.len() != 3 {
            return None;
        }
        let (a, b) = (shape[1] as usize, shape[2] as usize);
        let (anchors, fields, channels_first) = if a < b { (b, a, true) } else { (a, b, false) };
        if fields < 5 {
            return None;
        }
        let at = |anchor: usize, field: usize| -> f32 {
            if channels_first {
                data[field * anchors + anchor]
            } else {
                data[anchor * fields + field]
            }
        };

        let mut best: Option<(f32, f32, f32)> = None;
        for anchor in 0..anchors {
            if at(anchor, 4) < self.threshold {
                continue;
            }
            let area = at(anchor, 2) * at(anchor, 3);
            if best.map(|(_, _, top)| area > top).unwrap_or(true) {
                best = Some((at(anchor, 0), at(anchor, 1), area));
            }
        }

        let (cx, cy, _) = best?;
        Some(Focus::new(
            cx as f64 / input.width,
            cy as f64 / input.height,
        ))
    }

    fn prepare(&self, src: &[u8]) -> Option<Input> {
        let probe = VipsImage::new_from_buffer(src, "").ok()?;
        let (src_w, src_h) = (probe.get_width(), probe.get_height());
        drop(probe);
        if src_w <= 0 || src_h <= 0 {
            return None;
        }

        let size = self.infer_size as f64;
        let scale = (size / src_w as f64).min(size / src_h as f64);
        let thumb = ops::thumbnail_buffer_with_opts(
            src,
            ((src_w as f64 * scale).round() as i32).max(1),
            &ThumbnailBufferOptions {
                height: ((src_h as f64 * scale).round() as i32).max(1),
                ..Default::default()
            },
        )
        .ok()?;

        let rgb = ops::colourspace(&thumb, Interpretation::Srgb).ok()?;
        let rgb = ops::cast(&rgb, BandFormat::Uchar).ok()?;
        let rgb = if rgb.image_hasalpha() || rgb.get_bands() > 3 {
            ops::extract_band_with_opts(&rgb, 0, &ExtractBandOptions { n: 3 }).ok()?
        } else {
            rgb
        };
        if rgb.get_bands() != 3 {
            return None;
        }

        let (tw, th) = (rgb.get_width(), rgb.get_height());
        let padded = ops::embed_with_opts(
            &rgb,
            0,
            0,
            round_up(tw),
            round_up(th),
            &EmbedOptions {
                extend: Extend::Background,
                background: vec![114.0, 114.0, 114.0],
            },
        )
        .ok()?;

        let (pw, ph) = (padded.get_width() as usize, padded.get_height() as usize);
        let pixels = padded.image_write_to_memory();
        let plane = pw * ph;
        if pixels.len() < plane * 3 {
            return None;
        }
        let mut data = vec![0f32; plane * 3];
        for i in 0..plane {
            data[i] = pixels[i * 3] as f32 / 255.0;
            data[plane + i] = pixels[i * 3 + 1] as f32 / 255.0;
            data[plane * 2 + i] = pixels[i * 3 + 2] as f32 / 255.0;
        }

        Some(Input {
            shape: vec![1, 3, ph as i64, pw as i64],
            data,
            width: tw as f64,
            height: th as f64,
        })
    }
}

fn round_up(value: i32) -> i32 {
    (value + STRIDE - 1) / STRIDE * STRIDE
}

#[derive(Clone)]
pub struct FaceCache {
    inner: Cache<String, Option<Focus>>,
}

impl FaceCache {
    pub fn new(entries: u64, ttl: Duration) -> FaceCache {
        FaceCache {
            inner: Cache::builder()
                .max_capacity(entries)
                .time_to_live(ttl)
                .build(),
        }
    }

    pub async fn resolve<F, E>(&self, key: String, init: F) -> Option<Focus>
    where
        F: std::future::Future<Output = Result<Option<Focus>, E>>,
        E: Send + Sync + 'static,
    {
        self.inner.try_get_with(key, init).await.unwrap_or(None)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_up_snaps_to_stride() {
        assert_eq!(round_up(1), 32);
        assert_eq!(round_up(32), 32);
        assert_eq!(round_up(33), 64);
        assert_eq!(round_up(320), 320);
    }

    #[test]
    fn missing_model_reports_path() {
        let message = match FaceDetector::load(Path::new("/nonexistent/face.onnx"), 320, 0.28, 1, 1)
        {
            Ok(_) => panic!("missing model must fail"),
            Err(e) => e.to_string(),
        };
        assert!(message.contains("/nonexistent/face.onnx"));
    }
}
