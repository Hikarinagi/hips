# Configuration

All configuration is via environment variables. See [`.env.example`](../.env.example) for a
starting point.

## Required

| Variable        | Description                                                 |
| --------------- | ----------------------------------------------------------- |
| `R2_ENDPOINT`   | S3-compatible endpoint of the object store (Cloudflare R2). |
| `R2_ACCESS_KEY` | Access key id.                                              |
| `R2_SECRET_KEY` | Secret access key.                                          |
| `R2_BUCKET`     | Bucket name.                                                |

## Server

| Variable   | Default   | Description       |
| ---------- | --------- | ----------------- |
| `HOST`     | `0.0.0.0` | Bind address.     |
| `PORT`     | `8080`    | Bind port.        |
| `RUST_LOG` | `info`    | `tracing` filter. |

## Remote providers

| Variable                     | Default | Description                                                                                                    |
| ---------------------------- | ------- | -------------------------------------------------------------------------------------------------------------- |
| `THIRD_PARTY_PROVIDERS_JSON` | —       | JSON array of `{ "name": "...", "allowed_hosts": ["..."] }`. Enables the `GET /{provider}/{remote_url}` route. |
| `ALLOW_PRIVATE_REMOTE`       | `false` | Disable the SSRF IP check. Keep `false` in production.                                                         |

## Concurrency & limits

| Variable                | Default        | Description                                                                     |
| ----------------------- | -------------- | ------------------------------------------------------------------------------- |
| `WORKERS`               | CPU cores      | Size of the transform thread pool and the in-flight semaphore.                  |
| `VIPS_CONCURRENCY`      | `1`            | libvips threads per operation (parallelism is across requests, not within one). |
| `MAX_QUEUE`             | `WORKERS × 20` | Admission limit; requests beyond this get `503`.                                |
| `MAX_SRC_MEGAPIXELS`    | `50`           | Source resolution cap (decompression-bomb guard), checked before decode.        |
| `MAX_SRC_MB`            | `30`           | Max source bytes for remote fetches.                                            |
| `DOWNLOAD_TIMEOUT_SECS` | `10`           | Remote fetch timeout.                                                           |

## Result cache

| Variable                | Default | Description                                                                                             |
| ----------------------- | ------- | ------------------------------------------------------------------------------------------------------- |
| `RESULT_CACHE_MB`       | `256`   | Bounded, byte-weighted in-process cache; also coalesces identical concurrent requests. `0` disables it. |
| `RESULT_CACHE_TTL_SECS` | `60`    | Entry TTL.                                                                                              |

The CDN is the primary cache; this exists to coalesce bursts, not to be durable.

## Source cache

| Variable           | Default | Description                                                                                                                                |
| ------------------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `SOURCE_CACHE_DIR` | —       | Directory for the on-disk cache of fetched source bytes. Unset disables it; the container mounts a persistent volume at `/var/cache/hips`. |
| `SOURCE_CACHE_GB`  | `12`    | Disk size cap in GiB. A periodic sweep evicts least-recently-accessed entries back under the cap. `0` disables.                            |

Source objects live under immutable keys, so cached entries never go stale and the cache only bounds disk usage. It removes the upstream re-fetch when the same image is requested at several sizes; the result cache and the CDN still sit in front of it.

## Face detection (`gravity=face`)

| Variable              | Default                           | Description                                                                                                                                                                                                                                              |
| --------------------- | --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FACE_MODEL`          | `/usr/local/share/hips/face.onnx` | Path to the ONNX face detector. The image bakes in `deepghs/anime_face_detection` `face_detect_v1.4_n` (YOLOv8-nano, MIT). Empty or missing file disables detection; `gravity=face` then falls back to a top-anchored crop.                              |
| `FACE_INFER_SIZE`     | `320`                             | Longest side the source is letterboxed to before inference. 640 shifts the detected centre by ~2% of face width for ~3.5× the CPU.                                                                                                                       |
| `FACE_THRESHOLD`      | `0.28`                            | Confidence floor. The model's published best-F1 point is 0.278.                                                                                                                                                                                          |
| `FACE_SESSIONS`       | `WORKERS ÷ 2`                     | ONNX sessions in the pool; detections run concurrently up to this many. Clamped to `WORKERS`. On an 8-core M3 the cold path went 51 → 81 → 92 rps at 1 → 2 → 4 sessions and flattened after that. The first session costs ~44 MB RSS, each extra ~15 MB. |
| `FACE_THREADS`        | `1`                               | ONNX Runtime intra-op threads. Keep at `1` and size `FACE_SESSIONS` instead: splitting one small detection across threads measured _worse_ (~30 rps at `4`) because the transform pool already owns the cores.                                           |
| `FACE_CACHE_ENTRIES`  | `50000`                           | Detected focal points held in memory, keyed by source object.                                                                                                                                                                                            |
| `FACE_CACHE_TTL_SECS` | `86400`                           | Entry TTL. Detection failures caused by an overloaded pool are not cached.                                                                                                                                                                               |

Detection runs on the transform pool, on cache misses only, and costs roughly 25 ms on top of the
transform. `gravity=auto` does not use the model.

## Encoder effort

| Variable      | Default | Description                   |
| ------------- | ------- | ----------------------------- |
| `WEBP_EFFORT` | `4`     | libvips webp effort, 0–6.     |
| `AVIF_EFFORT` | `4`     | libvips heif/AV1 effort, 0–9. |

## Memory (deployment)

| Variable           | Description                                                                                      |
| ------------------ | ------------------------------------------------------------------------------------------------ |
| `MALLOC_ARENA_MAX` | Caps glibc malloc arenas (set to `2` in the container/unit) to reduce RSS fragmentation.         |
| `LD_PRELOAD`       | The container entrypoint preloads jemalloc for the glib/C allocator; set explicitly to override. |
