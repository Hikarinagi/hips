use std::sync::Arc;
use std::time::Duration;

use bytes::Bytes;
use hips_core::Codec;
use moka::future::Cache;

use crate::error::AppError;

#[derive(Clone, Copy, Default)]
pub struct Timing {
    pub fetch_us: u64,
    pub process_us: u64,
}

#[derive(Clone)]
pub struct Rendered {
    pub bytes: Bytes,
    pub codec: Codec,
    pub timing: Timing,
}

#[derive(Clone)]
pub struct ResultCache {
    inner: Option<Cache<String, Arc<Rendered>>>,
}

impl ResultCache {
    pub fn new(max_bytes: u64, ttl: Duration) -> ResultCache {
        if max_bytes == 0 {
            return ResultCache { inner: None };
        }
        let cache = Cache::builder()
            .max_capacity(max_bytes)
            .weigher(|_k, v: &Arc<Rendered>| v.bytes.len().min(u32::MAX as usize) as u32)
            .time_to_live(ttl)
            .build();
        ResultCache { inner: Some(cache) }
    }

    pub async fn resolve<F>(&self, key: String, init: F) -> Result<(Arc<Rendered>, bool), AppError>
    where
        F: std::future::Future<Output = Result<Arc<Rendered>, AppError>>,
    {
        match &self.inner {
            None => Ok((init.await?, false)),
            Some(cache) => {
                let hit = cache.contains_key(&key);
                let value = cache
                    .try_get_with(key, init)
                    .await
                    .map_err(|e: Arc<AppError>| (*e).clone())?;
                Ok((value, hit))
            }
        }
    }
}
