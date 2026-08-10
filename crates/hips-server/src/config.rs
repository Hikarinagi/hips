use std::env;
use std::thread::available_parallelism;
use std::time::Duration;

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct Provider {
    pub name: String,
    pub allowed_hosts: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct R2Config {
    pub endpoint: String,
    pub access_key: String,
    pub secret_key: String,
    pub bucket: String,
}

#[derive(Debug, Clone)]
pub struct Config {
    pub bind: String,
    pub r2: R2Config,
    pub providers: Vec<Provider>,
    pub workers: usize,
    pub vips_concurrency: i32,
    pub max_queue: usize,
    pub max_src_pixels: u64,
    pub max_src_bytes: usize,
    pub download_timeout: Duration,
    pub result_cache_bytes: u64,
    pub result_cache_ttl: Duration,
    pub source_cache_dir: Option<String>,
    pub source_cache_bytes: u64,
    pub webp_effort: i32,
    pub avif_effort: i32,
    pub png_compression: i32,
    pub allow_private_remote: bool,
    pub face_model: Option<String>,
    pub face_infer_size: i32,
    pub face_threshold: f32,
    pub face_threads: usize,
    pub face_sessions: usize,
    pub face_cache_entries: u64,
    pub face_cache_ttl: Duration,
}

impl Config {
    pub fn from_env() -> Result<Config, String> {
        let mut missing = Vec::new();
        let endpoint = require("R2_ENDPOINT", &mut missing);
        let access_key = require("R2_ACCESS_KEY", &mut missing);
        let secret_key = require("R2_SECRET_KEY", &mut missing);
        let bucket = require("R2_BUCKET", &mut missing);
        if !missing.is_empty() {
            return Err(format!(
                "missing required environment variables: {}",
                missing.join(", ")
            ));
        }

        let providers = match env::var("THIRD_PARTY_PROVIDERS_JSON") {
            Ok(raw) if !raw.trim().is_empty() => serde_json::from_str(&raw)
                .map_err(|e| format!("invalid THIRD_PARTY_PROVIDERS_JSON: {e}"))?,
            _ => Vec::new(),
        };

        let workers = parse_env("WORKERS").unwrap_or_else(default_workers).max(1);

        Ok(Config {
            bind: format!("{}:{}", get("HOST", "0.0.0.0"), get("PORT", "8080")),
            r2: R2Config {
                endpoint,
                access_key,
                secret_key,
                bucket,
            },
            providers,
            workers,
            vips_concurrency: parse_env("VIPS_CONCURRENCY").unwrap_or(1),
            max_queue: parse_env("MAX_QUEUE").unwrap_or(workers * 20).max(workers),
            max_src_pixels: (parse_env::<f64>("MAX_SRC_MEGAPIXELS")
                .unwrap_or(50.0)
                .max(1.0)
                * 1_000_000.0) as u64,
            max_src_bytes: parse_env("MAX_SRC_MB")
                .unwrap_or(30usize)
                .saturating_mul(1024 * 1024),
            download_timeout: Duration::from_secs(parse_env("DOWNLOAD_TIMEOUT_SECS").unwrap_or(10)),
            result_cache_bytes: parse_env::<u64>("RESULT_CACHE_MB")
                .unwrap_or(256)
                .saturating_mul(1024 * 1024),
            result_cache_ttl: Duration::from_secs(parse_env("RESULT_CACHE_TTL_SECS").unwrap_or(60)),
            source_cache_dir: env::var("SOURCE_CACHE_DIR").ok().filter(|v| !v.is_empty()),
            source_cache_bytes: parse_env::<u64>("SOURCE_CACHE_GB")
                .unwrap_or(12)
                .saturating_mul(1024 * 1024 * 1024),
            webp_effort: parse_env::<i32>("WEBP_EFFORT").unwrap_or(2).clamp(0, 6),
            avif_effort: parse_env::<i32>("AVIF_EFFORT").unwrap_or(1).clamp(0, 9),
            png_compression: parse_env::<i32>("PNG_COMPRESSION").unwrap_or(6).clamp(0, 9),
            allow_private_remote: parse_env("ALLOW_PRIVATE_REMOTE").unwrap_or(false),
            face_model: Some(get("FACE_MODEL", "/usr/local/share/hips/face.onnx"))
                .filter(|v| !v.is_empty()),
            face_infer_size: parse_env::<i32>("FACE_INFER_SIZE")
                .unwrap_or(320)
                .clamp(64, 1280),
            face_threshold: parse_env::<f32>("FACE_THRESHOLD")
                .unwrap_or(0.28)
                .clamp(0.05, 0.95),
            face_threads: parse_env::<usize>("FACE_THREADS").unwrap_or(1).max(1),
            face_sessions: parse_env::<usize>("FACE_SESSIONS")
                .unwrap_or_else(|| workers.div_ceil(2))
                .clamp(1, workers),
            face_cache_entries: parse_env("FACE_CACHE_ENTRIES").unwrap_or(50_000),
            face_cache_ttl: Duration::from_secs(
                parse_env("FACE_CACHE_TTL_SECS").unwrap_or(24 * 3600),
            ),
        })
    }
}

fn require(key: &str, missing: &mut Vec<String>) -> String {
    match env::var(key) {
        Ok(v) if !v.is_empty() => v,
        _ => {
            missing.push(key.to_string());
            String::new()
        }
    }
}

fn get(key: &str, default: &str) -> String {
    match env::var(key) {
        Ok(v) if !v.is_empty() => v,
        _ => default.to_string(),
    }
}

fn parse_env<T: std::str::FromStr>(key: &str) -> Option<T> {
    env::var(key).ok().and_then(|v| v.parse().ok())
}

fn default_workers() -> usize {
    available_parallelism().map(|n| n.get()).unwrap_or(4)
}
