# syntax=docker/dockerfile:1.7
#
# Heya application image.
#
# This Dockerfile builds the Nuxt frontend, embeds it into the Go binary, and
# copies only /usr/local/bin/heya onto a prebuilt runtime image. Runtime images
# live in:
#   - .docker/Dockerfile.cpu      amd64 + arm64
#   - .docker/Dockerfile.cuda     amd64
#   - .docker/Dockerfile.openvino amd64
#
# Build locally:
#   docker build -f .docker/Dockerfile.cpu -t heya:runtime-cpu .
#   docker build -f .docker/Dockerfile --build-arg BASE_RUNTIME_IMAGE=heya:runtime-cpu -t heya:base .

ARG BASE_RUNTIME_IMAGE=ghcr.io/heyamedia/heya-runtime:latest

# ────────────────────────────────────────────────────────────────
# Stage 1 — frontend: bun install + nuxi generate
# ────────────────────────────────────────────────────────────────
FROM oven/bun:1.3 AS frontend-build
WORKDIR /app/web

# Install deps first so a code-only change doesn't bust the dep layer.
# --frozen-lockfile mirrors CI: stale bun.lock fails the build instead of
# silently producing a different dep tree than developers see locally.
COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

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
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

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
# machines produce identical output for any given input. -buildvcs=false keeps
# release binary layers stable when only git metadata changes.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/heya ./cmd/heya

# ────────────────────────────────────────────────────────────────
# Stage 3 — binary export: CI artifact for packaging multiple runtimes
# ────────────────────────────────────────────────────────────────
FROM scratch AS binary-export
COPY --from=backend-build /out/heya /heya

# ────────────────────────────────────────────────────────────────
# Stage 4 — app runtime: small release layer on top of a runtime image
# ────────────────────────────────────────────────────────────────
FROM ${BASE_RUNTIME_IMAGE} AS runtime

COPY --link --from=backend-build /out/heya /usr/local/bin/heya

ENTRYPOINT ["/usr/local/bin/heya"]
CMD ["serve"]
