use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, SystemTime};

use bytes::Bytes;
use sha2::{Digest, Sha256};
use tokio::io::AsyncWriteExt;

use crate::error::AppError;

const GC_INTERVAL: Duration = Duration::from_secs(300);
const GC_LOW_WATERMARK_PCT: u64 = 90;
const ORPHAN_TMP_MAX_AGE: Duration = Duration::from_secs(3600);

#[derive(Clone)]
pub struct SourceCache {
    inner: Option<Arc<Store>>,
}

struct Store {
    root: PathBuf,
    max_bytes: u64,
}

impl SourceCache {
    pub fn new(dir: Option<String>, max_bytes: u64) -> SourceCache {
        let Some(dir) = dir.filter(|d| !d.is_empty()) else {
            return SourceCache { inner: None };
        };
        if max_bytes == 0 {
            return SourceCache { inner: None };
        }
        let root = PathBuf::from(dir);
        if let Err(e) = std::fs::create_dir_all(&root) {
            tracing::warn!(error = %e, path = %root.display(), "source cache disabled: cannot create dir");
            return SourceCache { inner: None };
        }
        SourceCache {
            inner: Some(Arc::new(Store { root, max_bytes })),
        }
    }

    pub fn capacity(&self) -> Option<u64> {
        self.inner.as_ref().map(|s| s.max_bytes)
    }

    pub async fn get<F, Fut>(&self, identity: &str, fetch: F) -> Result<Bytes, AppError>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = Result<Bytes, AppError>>,
    {
        let Some(store) = &self.inner else {
            return fetch().await;
        };
        let path = store.path_for(identity);
        match tokio::fs::read(&path).await {
            Ok(data) => return Ok(Bytes::from(data)),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => tracing::warn!(error = %e, "source cache read failed"),
        }
        let bytes = fetch().await?;
        if let Err(e) = store.write_atomic(&path, &bytes).await {
            tracing::warn!(error = %e, "source cache write failed");
        }
        Ok(bytes)
    }

    pub fn spawn_gc(&self) {
        let Some(store) = self.inner.clone() else {
            return;
        };
        tokio::spawn(async move {
            loop {
                tokio::time::sleep(GC_INTERVAL).await;
                let store = store.clone();
                let _ = tokio::task::spawn_blocking(move || store.gc()).await;
            }
        });
    }
}

impl Store {
    fn path_for(&self, identity: &str) -> PathBuf {
        let mut hasher = Sha256::new();
        hasher.update(identity.as_bytes());
        let hex = hex_lower(&hasher.finalize());
        self.root.join(&hex[0..2]).join(&hex[2..4]).join(&hex)
    }

    async fn write_atomic(&self, final_path: &Path, data: &[u8]) -> std::io::Result<()> {
        let parent = final_path.parent().expect("cache path has parent");
        tokio::fs::create_dir_all(parent).await?;
        let name = final_path.file_name().expect("cache path has file name");
        let tmp = parent.join(format!("{}.{}.tmp", name.to_string_lossy(), unique()));
        let mut file = tokio::fs::File::create(&tmp).await?;
        file.write_all(data).await?;
        file.sync_all().await?;
        drop(file);
        if let Err(e) = tokio::fs::rename(&tmp, final_path).await {
            let _ = tokio::fs::remove_file(&tmp).await;
            return Err(e);
        }
        Ok(())
    }

    fn gc(&self) {
        let mut entries: Vec<(PathBuf, u64, SystemTime, SystemTime)> = Vec::new();
        let mut total: u64 = 0;
        let now = SystemTime::now();
        for shard1 in dirs(&self.root) {
            for shard2 in dirs(&shard1) {
                for entry in std::fs::read_dir(&shard2).into_iter().flatten().flatten() {
                    let path = entry.path();
                    let Ok(meta) = entry.metadata() else { continue };
                    if !meta.is_file() {
                        continue;
                    }
                    if path.extension().map(|e| e == "tmp").unwrap_or(false) {
                        let stale = meta
                            .modified()
                            .ok()
                            .and_then(|m| now.duration_since(m).ok())
                            .map(|age| age > ORPHAN_TMP_MAX_AGE)
                            .unwrap_or(false);
                        if stale {
                            let _ = std::fs::remove_file(&path);
                        }
                        continue;
                    }
                    let atime = meta
                        .accessed()
                        .or_else(|_| meta.modified())
                        .unwrap_or(SystemTime::UNIX_EPOCH);
                    let mtime = meta.modified().unwrap_or(SystemTime::UNIX_EPOCH);
                    total += meta.len();
                    entries.push((path, meta.len(), atime, mtime));
                }
            }
        }
        if total <= self.max_bytes {
            return;
        }
        let target = self.max_bytes.saturating_mul(GC_LOW_WATERMARK_PCT) / 100;
        entries.sort_by_key(|(_, _, atime, _)| *atime);
        let mut freed: u64 = 0;
        for (path, size, _, snapshot_mtime) in entries {
            if total - freed <= target {
                break;
            }
            if let Ok(current) = std::fs::metadata(&path) {
                if current.modified().ok() != Some(snapshot_mtime) {
                    continue;
                }
            }
            if std::fs::remove_file(&path).is_ok() {
                freed += size;
            }
        }
        if freed > 0 {
            tracing::info!(freed_mb = freed / (1024 * 1024), "source cache gc evicted");
        }
    }
}

fn dirs(root: &Path) -> Vec<PathBuf> {
    std::fs::read_dir(root)
        .into_iter()
        .flatten()
        .flatten()
        .map(|e| e.path())
        .filter(|p| p.is_dir())
        .collect()
}

fn unique() -> String {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    format!(
        "{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, Ordering::Relaxed)
    )
}

fn hex_lower(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        out.push(HEX[(b >> 4) as usize] as char);
        out.push(HEX[(b & 0xf) as usize] as char);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmp_root() -> PathBuf {
        std::env::temp_dir().join(format!("hips-sc-test-{}", unique()))
    }

    #[tokio::test]
    async fn miss_then_hit_fetches_once() {
        let root = tmp_root();
        let cache = SourceCache::new(Some(root.to_string_lossy().into_owned()), 1024 * 1024);
        let calls = Arc::new(AtomicU64::new(0));

        let c1 = calls.clone();
        let first = cache
            .get("r2:foo/bar.jpg", || async move {
                c1.fetch_add(1, Ordering::Relaxed);
                Ok(Bytes::from_static(b"hello-image-bytes"))
            })
            .await
            .unwrap();
        assert_eq!(&first[..], b"hello-image-bytes");

        let c2 = calls.clone();
        let second = cache
            .get("r2:foo/bar.jpg", || async move {
                c2.fetch_add(1, Ordering::Relaxed);
                Ok(Bytes::from_static(b"SHOULD-NOT-RUN"))
            })
            .await
            .unwrap();
        assert_eq!(&second[..], b"hello-image-bytes");
        assert_eq!(calls.load(Ordering::Relaxed), 1);

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn distinct_identities_do_not_collide() {
        let root = tmp_root();
        let cache = SourceCache::new(Some(root.to_string_lossy().into_owned()), 1024 * 1024);
        let a = cache
            .get("r2:a", || async { Ok(Bytes::from_static(b"aaa")) })
            .await
            .unwrap();
        let b = cache
            .get("tp:https://x/y", || async {
                Ok(Bytes::from_static(b"bbbb"))
            })
            .await
            .unwrap();
        assert_eq!(&a[..], b"aaa");
        assert_eq!(&b[..], b"bbbb");
        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn disabled_passthrough() {
        let cache = SourceCache::new(None, 1024);
        assert!(cache.capacity().is_none());
        let b = cache
            .get("r2:x", || async { Ok(Bytes::from_static(b"z")) })
            .await
            .unwrap();
        assert_eq!(&b[..], b"z");
    }

    #[tokio::test]
    async fn gc_evicts_over_cap() {
        let root = tmp_root();
        let cache = SourceCache::new(Some(root.to_string_lossy().into_owned()), 4096);
        for i in 0..24 {
            let id = format!("r2:obj-{i}");
            let data = vec![0u8; 1024];
            cache
                .get(&id, || async move { Ok(Bytes::from(data)) })
                .await
                .unwrap();
        }
        let store = cache.inner.clone().unwrap();
        store.gc();
        let mut total = 0u64;
        for s1 in dirs(&store.root) {
            for s2 in dirs(&s1) {
                for e in std::fs::read_dir(&s2).into_iter().flatten().flatten() {
                    if let Ok(m) = e.metadata() {
                        if m.is_file() {
                            total += m.len();
                        }
                    }
                }
            }
        }
        assert!(total <= 4096, "after gc total={total} must be <= cap");
        let _ = std::fs::remove_dir_all(&root);
    }
}
