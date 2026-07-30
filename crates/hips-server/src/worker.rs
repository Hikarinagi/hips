use std::panic::AssertUnwindSafe;
use std::sync::atomic::{AtomicUsize, Ordering};

use tokio::sync::{oneshot, Semaphore};

#[derive(Debug, thiserror::Error)]
pub enum WorkerError {
    #[error("service overloaded")]
    Overloaded,
    #[error("worker pool closed")]
    Closed,
    #[error("worker panicked")]
    Panicked,
}

pub struct WorkerPool {
    pool: rayon::ThreadPool,
    permits: Semaphore,
    inflight: AtomicUsize,
    max_queue: usize,
}

impl WorkerPool {
    pub fn new(workers: usize, max_queue: usize) -> WorkerPool {
        let pool = rayon::ThreadPoolBuilder::new()
            .num_threads(workers)
            .thread_name(|i| format!("hips-vips-{i}"))
            .build()
            .expect("failed to build rayon thread pool");
        WorkerPool {
            pool,
            permits: Semaphore::new(workers),
            inflight: AtomicUsize::new(0),
            max_queue: max_queue.max(workers),
        }
    }

    pub fn inflight(&self) -> usize {
        self.inflight.load(Ordering::Relaxed)
    }

    pub async fn run<F, T>(&self, work: F) -> Result<T, WorkerError>
    where
        F: FnOnce() -> T + Send + 'static,
        T: Send + 'static,
    {
        let admitted = self.inflight.fetch_add(1, Ordering::AcqRel);
        let _guard = InflightGuard(&self.inflight);
        if admitted >= self.max_queue {
            return Err(WorkerError::Overloaded);
        }

        let permit = self
            .permits
            .acquire()
            .await
            .map_err(|_| WorkerError::Closed)?;
        let (tx, rx) = oneshot::channel();
        self.pool.spawn(move || {
            let outcome = std::panic::catch_unwind(AssertUnwindSafe(work));
            let _ = tx.send(outcome);
        });
        let outcome = rx.await.map_err(|_| WorkerError::Closed)?;
        drop(permit);
        outcome.map_err(|_| WorkerError::Panicked)
    }
}

struct InflightGuard<'a>(&'a AtomicUsize);

impl Drop for InflightGuard<'_> {
    fn drop(&mut self) {
        self.0.fetch_sub(1, Ordering::AcqRel);
    }
}
