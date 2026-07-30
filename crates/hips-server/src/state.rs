use std::ops::Deref;
use std::sync::atomic::AtomicU64;
use std::sync::Arc;

use object_store::ObjectStore;

use crate::cache::ResultCache;
use crate::config::Config;
use crate::engine::Engine;
use crate::source_cache::SourceCache;
use crate::worker::WorkerPool;

#[derive(Default)]
pub struct Metrics {
    pub requests: AtomicU64,
    pub cache_hits: AtomicU64,
    pub errors: AtomicU64,
    pub bytes_out: AtomicU64,
}

pub struct Inner {
    pub engine: Arc<Engine>,
    pub storage: Arc<dyn ObjectStore>,
    pub http: reqwest::Client,
    pub cache: ResultCache,
    pub source_cache: SourceCache,
    pub worker: WorkerPool,
    pub config: Config,
    pub metrics: Metrics,
}

#[derive(Clone)]
pub struct AppState(pub Arc<Inner>);

impl Deref for AppState {
    type Target = Inner;

    fn deref(&self) -> &Inner {
        &self.0
    }
}
