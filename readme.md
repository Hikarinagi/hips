# hips — Hikarinagi Image Processing Service

On-the-fly image resize / crop / transcode origin, served behind a CDN. Powered by libvips.

This repository is mirrored from Hikarinagi's private development monorepo. Issues and pull
requests are welcome here; maintainers import accepted changes into the upstream monorepo before
the next mirror sync.

See [`docs/`](docs/) for the HTTP API, architecture, and configuration.

## Develop

Requires libvips (`apt install libvips-dev` / `brew install vips`).

```bash
cargo test -p hips-core        # contract logic
cargo run --bin hips           # needs R2_* env (see .env.example)
```

## Build

```bash
docker build -t hips .
```
