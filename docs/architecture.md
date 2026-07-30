# Architecture

## Crate split

```
crates/
  hips-core    # pure contract logic, no libvips, no IO. Fully unit-tested.
  hips-server  # axum HTTP edge + libvips engine + R2/remote IO + concurrency/cache.
```

`hips-core` decides *what* to do with a request (parse params, compute the resize/crop/pad
geometry, negotiate the output codec). `hips-server` executes it. Keeping the contract logic
in a libvips-free crate means it can be tested without a working libvips runtime, and the
tricky geometry is verified in isolation.

### `hips-core` modules

- `params` — `ImageParams::from_pairs`, lenient parse of the full query contract.
- `fit` — `plan(src_w, src_h, w, h, fit, gravity) -> Plan`. The `Plan` enum (`Fit`,
  `SmartCover`, `DirectionalCover`, `Pad`) is an engine-agnostic description of the geometry,
  with all dimension/crop math precomputed and unit-tested.
- `format` — `OutputFormat`, `Codec`, source detection from magic bytes, and `Accept`
  negotiation for `f=auto`.
- `color` — `Rgba` parsing for `background`.

## Request lifecycle

```
GET /{path}?{query}, Accept
  → parse params + Accept                       (hips-core)
  → classify path: R2 key | provider+remote URL (net::classify)
  → cache key = source + param signature + format/accept tag
  → ResultCache.resolve(key, produce)           (moka try_get_with → coalesces dupes)
        produce:
          → fetch source bytes
                R2:     object_store GET
                remote: reqwest GET, SSRF-guarded, size-capped, streamed
          → detect source codec, resolve output codec (Accept for `auto`)
          → WorkerPool.run(|| engine.process(bytes, params, codec))
                rayon pool (fixed threads) + bounded semaphore + catch_unwind
          → Rendered { bytes, codec }
  → response: 200, content-type, cache-control, X-Cache, (Vary: Accept)
```

The result-cache `resolve` wraps the whole expensive path, so N identical concurrent requests
for the same key trigger exactly one fetch+transform (request coalescing / singleflight); the
rest await and share the bytes.

## libvips transform pipeline (`engine.rs`)

`Engine::init` initializes one process-global `VipsApp`, then:

- `cache_set_max(0)` + `cache_set_max_mem(0)` — **disable the operation cache**. It is useless
  for a stateless proxy (every request is a different image+params).
- `concurrency_set(VIPS_CONCURRENCY)` — threads libvips uses *per operation* (default 1; we
  parallelize across requests via the rayon pool instead).

`Engine::process`:

1. `new_from_buffer` to read the header (lazy, no pixel decode) → source dimensions.
2. **Megapixel guard**: reject before allocating any pixel buffer if `w*h` exceeds the cap.
3. `params.plan(src_w, src_h)` → a `Plan`.
4. `geometry()` executes the plan, preferring `thumbnail_buffer` (decode + shrink-on-load +
   EXIF auto-rotate + optional smartcrop in one call — the lowest-peak-memory path):
   - `Fit` → `thumbnail_buffer` with `size = Down|Both`, no crop.
   - `SmartCover` → `thumbnail_buffer` with `crop = Centre|Attention` (fills then smartcrops to exact size).
   - `DirectionalCover` → `thumbnail_buffer` to fill, then `extract_area` at the gravity anchor.
   - `Pad` → `thumbnail_buffer` to contain, then `embed` onto the background canvas.
5. Post ops, applied only if requested: `trim`, `gamma`, `brightness`, `contrast`,
   `saturation` (via Lch), `sharpen`, `blur`, `rotate`.
6. Encode: jpeg (progressive, optimized Huffman; alpha flattened over `background`/white),
   png (max compression), webp (`WEBP_EFFORT`), avif (heif/AV1, `AVIF_EFFORT`, 8-bit). All
   strip metadata.

`VipsImage` handles are dropped deterministically as each step's result goes out of scope.

## Concurrency model (`worker.rs`)

Image work is CPU- and RAM-bound, so it must not run on the tokio runtime threads.

- A **fixed `rayon` thread pool** sized to `WORKERS` runs every transform. Fixed, long-lived
  threads mean libvips' thread-local memory is bounded by pool size and never churns.
- A **tokio `Semaphore(WORKERS)`** gates admission so at most `WORKERS` transforms run at once.
- An **inflight counter** bounds the queue: once `inflight >= MAX_QUEUE`, new requests get
  `503` instead of piling up.
- The transform runs inside `catch_unwind`, so a panic on one malformed image returns `500`
  for that request instead of crashing the process.

## Caching / coalescing (`cache.rs`)

There is **no multi-level cache**. The CDN is the cache. In-process there is one bounded,
byte-weighted `moka` cache:

- `RESULT_CACHE_MB` caps total bytes; entries expire after `RESULT_CACHE_TTL_SECS`.
- `try_get_with` provides request coalescing for free; set `RESULT_CACHE_MB=0` to disable the
  cache (coalescing goes with it).

Its job is to absorb micro-bursts (e.g. thumbnail+medium+large of one cover requested at once)
and to dedupe concurrent identical requests — not to be a durable cache.

## Security (`net.rs`)

- **SSRF**: remote fetches are restricted to provider-whitelisted hosts, then the host is
  resolved and every resolved IP is checked; loopback / private / link-local / CGNAT /
  multicast / unspecified / `0.0.0.0` / IPv4-mapped equivalents are rejected (resolve-then-
  validate, to defeat DNS rebinding). HTTP redirects are disabled. `ALLOW_PRIVATE_REMOTE`
  is an explicit opt-out for trusted internal setups.
- **Decompression bombs**: remote bodies are streamed with a hard byte cap and a download
  timeout; the megapixel guard rejects oversized sources before any pixel buffer is allocated.

## Memory

Peak RSS is bounded and sized by configuration:

```
baseline + RESULT_CACHE_MB + WORKERS × per-transform peak
```

Per-transform peak depends on source/output dimensions (libvips shrink-on-load keeps it low for
large sources) and is capped by `MAX_SRC_MEGAPIXELS`. The libvips operation cache is off,
transforms run on a fixed worker pool, `VipsImage` handles drop as soon as they leave scope, and
`mimalloc` (Rust) + jemalloc (`LD_PRELOAD`, C side) + `MALLOC_ARENA_MAX=2` keep allocator
retention tight.
