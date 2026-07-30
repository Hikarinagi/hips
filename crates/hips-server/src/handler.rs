use std::collections::HashMap;
use std::sync::atomic::Ordering;
use std::sync::Arc;
use std::time::Instant;

use axum::body::Body;
use axum::extract::{Path, Query, State};
use axum::http::HeaderMap;
use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::Json;
use bytes::Bytes;
use hips_core::{Accept, Codec, ImageParams, OutputFormat};
use serde_json::json;

use crate::cache::{Rendered, Timing};
use crate::error::AppError;
use crate::net::{classify, fetch_source, Source};
use crate::state::AppState;

pub async fn image(
    State(state): State<AppState>,
    Path(path): Path<String>,
    Query(query): Query<HashMap<String, String>>,
    headers: HeaderMap,
) -> Result<Response, AppError> {
    state.metrics.requests.fetch_add(1, Ordering::Relaxed);

    let params = ImageParams::from_pairs(query);
    let accept = Accept::parse(headers.get(header::ACCEPT).and_then(|v| v.to_str().ok()));
    let source = classify(&path, &state.config.providers)?;
    let key = cache_key(&source, &params, &accept);

    let outcome = state
        .cache
        .resolve(key, produce(&state, &source, &params, &accept))
        .await;

    let (rendered, hit) = match outcome {
        Ok(value) => value,
        Err(err) => {
            state.metrics.errors.fetch_add(1, Ordering::Relaxed);
            return Err(err);
        }
    };

    if hit {
        state.metrics.cache_hits.fetch_add(1, Ordering::Relaxed);
    }
    state
        .metrics
        .bytes_out
        .fetch_add(rendered.bytes.len() as u64, Ordering::Relaxed);

    Ok(render(&rendered, &params, hit))
}

async fn produce(
    state: &AppState,
    source: &Source,
    params: &ImageParams,
    accept: &Accept,
) -> Result<Arc<Rendered>, AppError> {
    let t_fetch = Instant::now();
    let src_bytes = fetch_source(state, source).await?;
    let fetch_us = t_fetch.elapsed().as_micros() as u64;

    let codec = params.format.resolve(Codec::from_magic(&src_bytes), accept);
    let engine = state.engine.clone();
    let params = params.clone();
    let bytes = src_bytes;

    let t_proc = Instant::now();
    let out = state
        .worker
        .run(move || engine.process(&bytes, &params, codec))
        .await??;
    let process_us = t_proc.elapsed().as_micros() as u64;
    Ok(Arc::new(Rendered {
        bytes: Bytes::from(out.bytes),
        codec: out.codec,
        timing: Timing {
            fetch_us,
            process_us,
        },
    }))
}

fn render(rendered: &Rendered, params: &ImageParams, hit: bool) -> Response {
    let t = &rendered.timing;
    let server_timing = format!(
        "fetch;dur={:.1}, process;dur={:.1}, total;dur={:.1}",
        t.fetch_us as f64 / 1000.0,
        t.process_us as f64 / 1000.0,
        (t.fetch_us + t.process_us) as f64 / 1000.0,
    );
    let mut builder = Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, rendered.codec.content_type())
        .header(header::ACCESS_CONTROL_ALLOW_ORIGIN, "*")
        .header(header::CACHE_CONTROL, "public, max-age=31536000, immutable")
        .header("x-cache", if hit { "HIT" } else { "MISS" })
        .header("server-timing", server_timing)
        .header("timing-allow-origin", "*");
    if params.format == OutputFormat::Auto {
        builder = builder.header(header::VARY, "Accept");
    }
    builder
        .body(Body::from(rendered.bytes.clone()))
        .expect("valid response")
}

fn cache_key(source: &Source, params: &ImageParams, accept: &Accept) -> String {
    let src = match source {
        Source::R2(key) => format!("r2:{key}"),
        Source::Remote { url } => format!("tp:{url}"),
    };
    format!("{src}|{}|{}", param_sig(params), fmt_tag(params, accept))
}

fn param_sig(p: &ImageParams) -> String {
    format!(
        "{:?}x{:?}|q{}|{:?}|{:?}|b{}|r{}|t{}|bg{:?}|br{:?}|co{:?}|ga{:?}|sh{:?}|sa{:?}",
        p.width,
        p.height,
        p.quality,
        p.fit,
        p.gravity,
        p.blur,
        p.rotate,
        p.trim,
        p.background,
        p.brightness,
        p.contrast,
        p.gamma,
        p.sharpen,
        p.saturation,
    )
}

fn fmt_tag(p: &ImageParams, accept: &Accept) -> String {
    match p.format {
        OutputFormat::Auto => format!("auto:{}{}", accept.avif as u8, accept.webp as u8),
        OutputFormat::Keep => "keep".to_string(),
        other => format!("{other:?}"),
    }
}

pub async fn health(State(state): State<AppState>) -> Json<serde_json::Value> {
    Json(json!({ "status": "healthy", "inflight": state.worker.inflight() }))
}

pub async fn metrics(State(state): State<AppState>) -> Response {
    let m = &state.metrics;
    let body = format!(
        concat!(
            "# TYPE hips_requests_total counter\nhips_requests_total {}\n",
            "# TYPE hips_cache_hits_total counter\nhips_cache_hits_total {}\n",
            "# TYPE hips_errors_total counter\nhips_errors_total {}\n",
            "# TYPE hips_bytes_out_total counter\nhips_bytes_out_total {}\n",
            "# TYPE hips_inflight gauge\nhips_inflight {}\n",
        ),
        m.requests.load(Ordering::Relaxed),
        m.cache_hits.load(Ordering::Relaxed),
        m.errors.load(Ordering::Relaxed),
        m.bytes_out.load(Ordering::Relaxed),
        state.worker.inflight(),
    );
    ([(header::CONTENT_TYPE, "text/plain; version=0.0.4")], body).into_response()
}

pub async fn root() -> impl IntoResponse {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "msg": "Ciallo～ (∠・ω< )⌒★" })),
    )
}
