ARG VIPS_VERSION=8.18.3

FROM rust:1.95-bookworm AS build
ARG VIPS_VERSION
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential meson ninja-build pkg-config ca-certificates curl xz-utils cmake nasm perl \
        libglib2.0-dev libexpat1-dev \
        libjpeg62-turbo-dev libpng-dev libwebp-dev libexif-dev liblcms2-dev libheif-dev \
    && rm -rf /var/lib/apt/lists/*

ARG HWY_VERSION=1.2.0
RUN curl -fsSL "https://github.com/google/highway/archive/refs/tags/${HWY_VERSION}.tar.gz" | tar -xz \
    && cmake -S "highway-${HWY_VERSION}" -B hwy-build \
         -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=ON \
         -DCMAKE_INSTALL_PREFIX=/usr/local -DCMAKE_INSTALL_LIBDIR=lib \
         -DHWY_ENABLE_TESTS=OFF -DHWY_ENABLE_EXAMPLES=OFF \
    && cmake --build hwy-build -j"$(nproc)" \
    && cmake --install hwy-build \
    && ldconfig

ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
RUN curl -fsSL "https://github.com/libvips/libvips/releases/download/v${VIPS_VERSION}/vips-${VIPS_VERSION}.tar.xz" \
      | tar -xJ \
    && cd "vips-${VIPS_VERSION}" \
    && meson setup build --prefix=/usr/local --libdir=lib --buildtype=release -Db_lto=true \
         -Dcplusplus=false -Dintrospection=disabled -Dmodules=disabled \
         -Djpeg=enabled -Dpng=enabled -Dwebp=enabled -Dheif=enabled -Dexif=enabled -Dlcms=enabled \
         -Dorc=disabled -Dhighway=enabled -Dzlib=enabled \
         -Darchive=disabled -Dcfitsio=disabled -Dcgif=disabled -Dfftw=disabled -Dfontconfig=disabled \
         -Dimagequant=disabled -Djpeg-xl=disabled -Dmagick=disabled -Dmatio=disabled -Dnifti=disabled \
         -Dopenexr=disabled -Dopenjpeg=disabled -Dopenslide=disabled -Dpangocairo=disabled \
         -Dpdfium=disabled -Dpoppler=disabled -Dquantizr=disabled -Drsvg=disabled -Dspng=disabled -Dtiff=disabled \
         -Draw=disabled -Duhdr=disabled \
    && meson compile -C build \
    && meson install -C build \
    && ldconfig

WORKDIR /build
ARG APP_VERSION=dev
COPY Cargo.toml Cargo.lock ./
COPY crates ./crates

FROM build AS test
RUN rustup component add rustfmt \
    && cargo fmt --all --check \
    && cargo test --workspace --locked

FROM build AS binary
RUN cargo build --release --bin hips

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        libglib2.0-0 libexpat1 \
        libjpeg62-turbo libpng16-16 libwebp7 libwebpdemux2 libwebpmux3 \
        libexif12 liblcms2-2 libheif1 \
        libjemalloc2 ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-create-home --shell /usr/sbin/nologin hips

COPY --from=binary /usr/local/lib/libvips.so* /usr/local/lib/libhwy*.so* /usr/local/lib/
RUN ldconfig
COPY --from=binary /build/target/release/hips /usr/local/bin/hips
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
RUN mkdir -p /var/cache/hips && chown hips:hips /var/cache/hips

ENV PORT=8080 \
    HOST=0.0.0.0 \
    MALLOC_ARENA_MAX=2 \
    RUST_LOG=info

USER hips
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
