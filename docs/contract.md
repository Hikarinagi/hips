# HTTP contract

## Routes

| Method + path                           | Purpose                                                                        |
| --------------------------------------- | ------------------------------------------------------------------------------ |
| `GET /{r2_object_key}?{params}`         | Transform an object stored in the R2 bucket. The whole path is the object key. |
| `GET /{provider}/{remote_url}?{params}` | Fetch and transform a whitelisted remote image.                                |
| `GET /health`                           | Liveness. `200` + `{"status":"healthy","inflight":N}`.                         |
| `GET /metrics`                          | Prometheus text exposition.                                                    |
| `GET /`                                 | `400` + a greeting; there is no root image.                                    |

`/health`, `/metrics`, and `/` are matched before the catch-all, so an object key never
collides with them.

### Source classification

A request path is treated as a **remote** fetch only when **all** of the following hold;
otherwise the entire path is an **R2 object key**:

1. the first path segment equals a configured provider `name`, and
2. the remainder is an `http(s)` URL (percent-decoded if needed), and
3. that URL's host is in the provider's `allowed_hosts`.

So `GET /bangumi/https://lain.bgm.tv/pic/cover/l/.../x.jpg?w=600&f=webp` proxies a Bangumi
image, while `GET /bangumi/123/cover.jpg` is just an R2 key that happens to start with
`bangumi/`. Provider config comes from the `THIRD_PARTY_PROVIDERS_JSON` env var. Hosts that
are not whitelisted return `403`. (In the Hikarinagi deployment that value is generated from
`packages/config/src/media.ts`.)

## Query parameters

Parsing is **lenient**: unknown keys are ignored, and an out-of-range or unparseable value
falls back to the default rather than erroring (this is an origin behind a CDN; a bad param
should still render something). Both long and short keys are accepted.

| Key(s)             | Type / range                                                                                             | Default      | Notes                                                              |
| ------------------ | -------------------------------------------------------------------------------------------------------- | ------------ | ------------------------------------------------------------------ |
| `w`, `width`       | int > 0                                                                                                  | —            | Multiplied by `dpr`, then clamped to 5000.                         |
| `h`, `height`      | int > 0                                                                                                  | —            | Same clamping as width.                                            |
| `q`, `quality`     | 1–100                                                                                                    | 85           | Applies to jpeg/webp/avif.                                         |
| `f`, `format`      | `webp` `avif` `jpeg` `png` `auto`                                                                        | keep source  | `auto` negotiates via `Accept`; absent keeps the source format.    |
| `fit`              | `scale-down` `contain` `cover` `crop` `pad`                                                              | `scale-down` | See semantics below.                                               |
| `gravity`, `g`     | `auto` `center` `top` `bottom` `left` `right` `top-left` `top-right` `bottom-left` `bottom-right` `face` | `center`     | Only used by `cover`/`crop`.                                       |
| `dpr`              | 1–3                                                                                                      | 1            | Multiplies `w`/`h`.                                                |
| `background`, `bg` | `#rgb` `#rgba` `#rrggbb` `#rrggbbaa` `white` `black` `transparent`                                       | —            | Used by `pad`; jpeg flatten uses it if opaque, else white.         |
| `blur`             | 0–100                                                                                                    | 0            | Gaussian sigma.                                                    |
| `brightness`       | float                                                                                                    | —            | Multiplier around 1.0.                                             |
| `contrast`         | float                                                                                                    | —            | Multiplier around 1.0.                                             |
| `gamma`            | float                                                                                                    | —            | libvips gamma exponent (clamped 0.01–10).                          |
| `saturation`       | float                                                                                                    | —            | Chroma multiplier (0 = greyscale).                                 |
| `sharpen`          | 0–10                                                                                                     | —            | Unsharp sigma.                                                     |
| `rotate`           | `90` `180` `270`                                                                                         | 0            | Other values ignored.                                              |
| `trim`             | `true`/`1`/`yes`                                                                                         | false        | Trim a uniform border.                                             |
| `pad`              | `true`/`1`/`yes`                                                                                         | false        | Alias for `fit=pad` when `fit` is absent; ignored if `fit` is set. |

### `fit` semantics (Cloudflare Images compatible)

- **scale-down** — fit within `w×h`, preserve aspect ratio, **never upscale**.
- **contain** — fit within `w×h`, preserve aspect ratio, may upscale.
- **cover** — output exactly `w×h` (both required); scale to fill, crop overflow per `gravity`. May upscale.
- **crop** — like `cover` but **never upscales**; a source smaller than the target degrades to `scale-down`.
- **pad** — fit within `w×h` (like `contain`), then pad the canvas to exactly `w×h` with `background`.

When only one of `w`/`h` is given, `cover`/`crop`/`pad` degrade to a single-dimension fit.

### `gravity`

Only meaningful for `cover`/`crop`. Directional values crop a window anchored to that edge/corner.

- `center` / `auto` — libvips smartcrop (`centre` and attention respectively). Attention scores
  edge energy, photographic skin tone and LAB saturation on a 32×32 reduction of the image; on
  illustration sources it reliably prefers saturated hair, clothing detail and text over faces.
- `face` — YOLOv8 face detection on the source. The crop is centred horizontally on the largest
  detected face and placed so the face sits 38% down the frame. Detection is memoised per source
  object, so a source is only scored once regardless of how many sizes are requested. When no face
  is found — or the model is not installed, or the transform pool is saturated — the crop falls
  back to a top-anchored window, which frames portrait sources better than attention does.

### `format`

- explicit `webp`/`avif`/`jpeg`/`png` — encode to that codec.
- `auto` — negotiate from the `Accept` header: avif → webp → fall back to the source codec. Adds `Vary: Accept`.
- absent (`keep`) — re-encode to the **source** codec (gif sources fall back to png).

Metadata (EXIF/ICC/etc.) is stripped on encode; EXIF orientation is applied during decode.

## Response

Success: `200`, `Content-Type` of the chosen codec, and:

```
Cache-Control: public, max-age=31536000, immutable
X-Cache: HIT | MISS
Vary: Accept            (only when f=auto)
```

CORS is permissive (`Access-Control-Allow-Origin: *`, any method). The CDN is the real cache;
these headers let it cache aggressively and key `auto` responses by `Accept`.

## Errors

JSON body `{"error": "..."}` with `Cache-Control: no-store`:

| Status                      | When                                                                  |
| --------------------------- | --------------------------------------------------------------------- |
| `400 Bad Request`           | invalid remote URL, or an undecodable/corrupt source image            |
| `403 Forbidden`             | unknown provider host, or a remote host that resolves to a blocked IP |
| `404 Not Found`             | object missing in R2                                                  |
| `413 Payload Too Large`     | source exceeds the megapixel cap or the remote byte cap               |
| `502 Bad Gateway`           | remote/upstream fetch failed                                          |
| `503 Service Unavailable`   | admission queue full (overloaded)                                     |
| `500 Internal Server Error` | libvips processing failure                                            |
