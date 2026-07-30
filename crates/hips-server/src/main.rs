mod cache;
mod config;
mod engine;
mod error;
mod handler;
mod net;
mod source_cache;
mod state;
mod worker;

use std::sync::Arc;

use axum::routing::get;
use axum::Router;
use object_store::aws::AmazonS3Builder;
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

use crate::cache::ResultCache;
use crate::config::Config;
use crate::engine::{EncodeConfig, Engine};
use crate::source_cache::SourceCache;
use crate::state::{AppState, Inner, Metrics};
use crate::worker::WorkerPool;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let config = Config::from_env().unwrap_or_else(|e| {
        eprintln!("config error: {e}");
        std::process::exit(1);
    });

    let engine = Engine::init(
        config.vips_concurrency,
        config.max_src_pixels,
        EncodeConfig {
            webp_effort: config.webp_effort,
            avif_effort: config.avif_effort,
            png_compression: config.png_compression,
        },
    )
    .unwrap_or_else(|e| {
        eprintln!("libvips init failed: {e}");
        std::process::exit(1);
    });

    let storage = build_storage(&config).unwrap_or_else(|e| {
        eprintln!("storage init failed: {e}");
        std::process::exit(1);
    });

    let http = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .user_agent(concat!("hips/", env!("CARGO_PKG_VERSION")))
        .build()
        .expect("failed to build http client");

    let worker = WorkerPool::new(config.workers, config.max_queue);
    let cache = ResultCache::new(config.result_cache_bytes, config.result_cache_ttl);
    let source_cache = SourceCache::new(config.source_cache_dir.clone(), config.source_cache_bytes);
    let bind = config.bind.clone();

    tracing::info!(
        workers = config.workers,
        vips_concurrency = config.vips_concurrency,
        max_src_megapixels = config.max_src_pixels / 1_000_000,
        providers = config.providers.len(),
        source_cache_gb = source_cache
            .capacity()
            .map(|b| b / (1024 * 1024 * 1024))
            .unwrap_or(0),
        "hips configured"
    );

    let state = AppState(Arc::new(Inner {
        engine: Arc::new(engine),
        storage,
        http,
        cache,
        source_cache,
        worker,
        config,
        metrics: Metrics::default(),
    }));

    state.source_cache.spawn_gc();

    let app = Router::new()
        .route("/", get(handler::root))
        .route("/health", get(handler::health))
        .route("/metrics", get(handler::metrics))
        .route("/{*path}", get(handler::image))
        .layer(TraceLayer::new_for_http())
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(&bind)
        .await
        .unwrap_or_else(|e| {
            eprintln!("failed to bind {bind}: {e}");
            std::process::exit(1);
        });
    tracing::info!("hips listening on {bind}");

    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
        .expect("server error");
}

fn build_storage(
    config: &Config,
) -> Result<Arc<dyn object_store::ObjectStore>, object_store::Error> {
    let s3 = AmazonS3Builder::new()
        .with_endpoint(config.r2.endpoint.clone())
        .with_region("auto")
        .with_bucket_name(config.r2.bucket.clone())
        .with_access_key_id(config.r2.access_key.clone())
        .with_secret_access_key(config.r2.secret_key.clone())
        .with_virtual_hosted_style_request(false)
        .build()?;
    Ok(Arc::new(s3))
}

async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c().await.ok();
    };

    #[cfg(unix)]
    let terminate = async {
        if let Ok(mut signal) =
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        {
            signal.recv().await;
        }
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
    tracing::info!("shutdown signal received, draining");
}
