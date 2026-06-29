# syntax=docker/dockerfile:1.7
#
# Heya — single-binary media server image.
#
# Three stages:
#   1. frontend-build — Bun + Nuxt SPA -> web/.output/public/*
#   2. backend-build  — Go compile with embedded SPA -> /out/heya
#   3. runtime        — minideb:trixie + jellyfin-ffmpeg7 + libonnxruntime
#
# Multi-arch: builds linux/amd64 and linux/arm64 from the same file. Every
# stage runs under the *target* platform (buildx emulates the non-native one
# via QEMU), so the Go CGO compile is always a native-for-target build — no
# cross C toolchain needed. The runtime stage derives the per-arch bits
# (jellyfin apt arch, onnxruntime asset name, Debian multiarch lib dir) from
# `dpkg --print-architecture` so there's nothing arch-specific hardcoded.
#   docker build --platform=linux/arm64 -t heya:dev .   # native on Apple Silicon
#   docker buildx build --platform=linux/amd64,linux/arm64 ...  # both (CI)
#
# Why these choices:
# - **minideb:trixie** instead of debian:bookworm-slim: ~30 MB base vs ~80 MB,
#   ships an `install_packages` helper that auto-cleans apt state, and trixie
#   (Debian 13) is current stable so apt sources are fresh.
# - **jellyfin-ffmpeg7** instead of distro ffmpeg: Jellyfin's fork is built
#   with libsoxr (high-quality audio resampling), all the modern hwaccel
#   paths (VAAPI, QSV, NVENC, V4L2), HDR tone-mapping LUTs, and tracks
#   ffmpeg upstream more aggressively than Debian's package. Heya looks up
#   `ffmpeg`/`ffprobe` from $PATH, so we symlink the jellyfin binaries.
# - **CGO=1 + libonnxruntime**: required because internal/sonicanalysis pulls
#   in `github.com/yalue/onnxruntime_go`, which `import "C"`s into the ONNX
#   Runtime shared library. Without this the build fails and sonic-analysis
#   features (similar-artist by audio embeddings) wouldn't work at runtime.
#
# Build locally:    docker build -t heya:dev .
# Run locally:      docker run -p 8080:8080 -v $PWD/data:/data heya:dev
# Postgres is external — point at it via HEYA_DATABASE_URL.

# ────────────────────────────────────────────────────────────────
# Stage 1 — frontend: bun install + nuxi generate
# ────────────────────────────────────────────────────────────────
FROM oven/bun:1.3 AS frontend-build
WORKDIR /app/web

# Install deps first so a code-only change doesn't bust the dep layer.
# --frozen-lockfile mirrors CI: stale bun.lock fails the build instead of
# silently producing a different dep tree than developers see locally.
COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN bun install --frozen-lockfile

# Copy the rest of the SPA source (nuxt.config.ts, app/, public/, shared/, …).
COPY web/ ./

# `bun run build` => `nuxi generate` => writes .output/public/* (SPA assets).
RUN bun run build

# ────────────────────────────────────────────────────────────────
# Stage 2 — backend: go build with the SPA embedded into the binary
# ────────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS backend-build
WORKDIR /src

# Module cache layer first.
COPY go.mod go.sum ./
RUN go mod download

# Source.
COPY . .

# web/embed.go embeds web/dist/* into the binary. Populate dist/ from the
# frontend stage so the embed pulls in the freshly-built SPA, not the empty
# committed placeholder.
RUN rm -rf web/dist/* && \
    mkdir -p web/dist
COPY --from=frontend-build /app/web/.output/public/ ./web/dist/

# CGO=1 because onnxruntime_go links against the ONNX Runtime C ABI.
# -trimpath drops /src paths from the binary so two builds on different
# machines produce identical output for any given input.
RUN CGO_ENABLED=1 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/heya ./cmd/heya

# ────────────────────────────────────────────────────────────────
# Stage 3 — runtime: minideb + jellyfin-ffmpeg + libonnxruntime + binary
# ────────────────────────────────────────────────────────────────
FROM bitnami/minideb:trixie AS runtime

# Pinned versions. Bump deliberately:
# - jellyfin-ffmpeg: tied to Jellyfin's release cadence; tracks ffmpeg upstream.
# - onnxruntime: must stay ABI-compatible with `github.com/yalue/onnxruntime_go`.
ARG JELLYFIN_FFMPEG_PACKAGE=jellyfin-ffmpeg7
# ONNX Runtime must satisfy the onnxruntime_go binding's ORT_API_VERSION
# (v1.27.0 → API 24 → needs ORT ≥ 1.24.0). 1.24.1 is the version pinned across
# ALL images (base CPU here, the CUDA gpu build, and the OpenVINO wheel) so the
# single heya binary's C-API version matches every variant's libonnxruntime.
# Bump in lockstep with the binding and the variant Dockerfiles.
ARG ONNXRUNTIME_VERSION=1.24.1

# minideb's install_packages handles apt update + install + cache cleanup
# in one shot, but we need to add the Jellyfin apt repo first, so we go
# manual to control the layering.
#
# Arch is read from `dpkg --print-architecture` (amd64 | arm64) and mapped to
# the two upstream naming schemes that differ per-arch:
#   - onnxruntime release asset:  linux-x64 (amd64) | linux-aarch64 (arm64)
#   - Debian multiarch lib dir:   x86_64-linux-gnu  | aarch64-linux-gnu
# jellyfin's apt repo already names archs amd64/arm64, so DPKG_ARCH feeds it
# directly. Keep this in sync with defaultOnnxLib() in
# internal/sonicanalysis/onnx.go, which resolves the same per-arch lib path.
RUN DPKG_ARCH="$(dpkg --print-architecture)" && \
    case "${DPKG_ARCH}" in \
        amd64) ORT_ARCH=x64;     LIB_DIR=x86_64-linux-gnu ;; \
        arm64) ORT_ARCH=aarch64; LIB_DIR=aarch64-linux-gnu ;; \
        *) echo "unsupported architecture: ${DPKG_ARCH}" >&2; exit 1 ;; \
    esac && \
    install_packages \
        ca-certificates \
        curl \
        gnupg \
        tzdata && \
    \
    # Jellyfin ffmpeg apt repo (trixie). Key is published at the team URL
    # and signs the per-distro Release files.
    install -d /etc/apt/keyrings && \
    curl -fsSL https://repo.jellyfin.org/jellyfin_team.gpg.key \
        | gpg --dearmor -o /etc/apt/keyrings/jellyfin.gpg && \
    echo "deb [arch=${DPKG_ARCH} signed-by=/etc/apt/keyrings/jellyfin.gpg] https://repo.jellyfin.org/master/debian trixie main" \
        > /etc/apt/sources.list.d/jellyfin.list && \
    install_packages ${JELLYFIN_FFMPEG_PACKAGE} && \
    \
    # Heya looks up ffmpeg/ffprobe via $PATH (exec.LookPath). Symlink the
    # jellyfin binaries onto the standard locations so no Go-side config
    # is required.
    ln -sf /usr/lib/jellyfin-ffmpeg/ffmpeg  /usr/local/bin/ffmpeg && \
    ln -sf /usr/lib/jellyfin-ffmpeg/ffprobe /usr/local/bin/ffprobe && \
    \
    # ONNX Runtime shared library, into the arch's Debian multiarch dir where
    # defaultOnnxLib() expects it.
    curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-${ORT_ARCH}-${ONNXRUNTIME_VERSION}.tgz" \
        -o /tmp/onnxruntime.tgz && \
    tar -xzf /tmp/onnxruntime.tgz -C /tmp && \
    # -P preserves symlinks: the tarball ships libonnxruntime.so ->
    # libonnxruntime.so.1 -> libonnxruntime.so.X.Y.Z, three names for one
    # ~50 MB blob. Without -P, cp resolves them and we'd ship the file
    # three times.
    cp -P /tmp/onnxruntime-linux-${ORT_ARCH}-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so* \
        /usr/lib/${LIB_DIR}/ && \
    ldconfig && \
    \
    # Drop bootstrap tools — they were only needed to fetch keys + tarball.
    apt-get purge -y curl gnupg && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/* /tmp/onnxruntime*

COPY --from=backend-build /out/heya /usr/local/bin/heya

# Data lives outside the image. Mount a host directory or named volume here.
# Inside the container the binary defaults HEYA_DATA_DIR to /data so the
# Tailscale state, transcode cache, and any uploaded assets all land here.
RUN mkdir -p /data
WORKDIR /data
ENV HEYA_DATA_DIR=/data \
    HEYA_HOST=0.0.0.0 \
    HEYA_PORT=8080

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/heya"]
CMD ["serve"]
