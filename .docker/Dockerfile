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
ARG VERSION=dev

# ────────────────────────────────────────────────────────────────
# Stage 0 — official subscription-agent native CLIs
# ────────────────────────────────────────────────────────────────
# The vendors' own TypeScript SDKs are process wrappers around these binaries.
# Fetch the platform packages at build time so the final image needs neither
# Node nor npm. Versions are pinned and intentionally bumped by hand.
FROM node:24-bookworm-slim AS agent-cli-build
ARG TARGETARCH
ARG CLAUDE_CODE_VERSION=2.1.206
ARG CODEX_VERSION=0.144.0

ADD --checksum=sha256:d17f227e4df5da1600391338865ce0f3055211760a36688f816941d58232d8dc \
    https://raw.githubusercontent.com/openai/codex/rust-v0.144.0/LICENSE /tmp/codex-LICENSE

RUN --mount=type=cache,target=/root/.npm \
    set -eux; \
    case "${TARGETARCH}" in \
        amd64) NPM_ARCH=x64; CODEX_TRIPLE=x86_64-unknown-linux-musl ;; \
        arm64) NPM_ARCH=arm64; CODEX_TRIPLE=aarch64-unknown-linux-musl ;; \
        *) echo "unsupported agent CLI architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    mkdir -p /out/codex /out/licenses/claude /out/licenses/codex; \
    npm pack "@anthropic-ai/claude-code-linux-${NPM_ARCH}@${CLAUDE_CODE_VERSION}" --pack-destination /tmp; \
    tar -xzf /tmp/anthropic-ai-claude-code-linux-${NPM_ARCH}-${CLAUDE_CODE_VERSION}.tgz -C /tmp; \
    install -m 0755 /tmp/package/claude /out/claude; \
    install -m 0644 /tmp/package/LICENSE.md /out/licenses/claude/LICENSE.md; \
    rm -rf /tmp/package; \
    npm pack "@openai/codex@${CODEX_VERSION}-linux-${NPM_ARCH}" --pack-destination /tmp; \
    tar -xzf /tmp/openai-codex-${CODEX_VERSION}-linux-${NPM_ARCH}.tgz -C /tmp; \
    cp -a "/tmp/package/vendor/${CODEX_TRIPLE}/." /out/codex/; \
    install -m 0644 /tmp/package/README.md /out/licenses/codex/README.md; \
    install -m 0644 /tmp/codex-LICENSE /out/licenses/codex/LICENSE; \
    /out/claude --version; \
    /out/codex/bin/codex --version

# ────────────────────────────────────────────────────────────────
# Stage 1 — frontend: bun install + nuxi generate
# ────────────────────────────────────────────────────────────────
FROM oven/bun:1.3 AS frontend-build
ARG VERSION
ENV NUXT_PUBLIC_HEYA_VERSION=${VERSION}
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
ARG VERSION
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
    go build -tags embed_frontend -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/karbowiak/heya/internal/ui.Version=${VERSION}" \
    -o /out/heya ./cmd/heya

# ────────────────────────────────────────────────────────────────
# Stage 3 — app export: CI artifact for packaging multiple runtimes
# ────────────────────────────────────────────────────────────────
FROM scratch AS binary-export
COPY --from=backend-build /out/heya /heya
COPY --from=agent-cli-build /out/claude /agent-clis/claude
COPY --from=agent-cli-build /out/codex /agent-clis/codex
COPY --from=agent-cli-build /out/licenses /agent-clis/licenses

# ────────────────────────────────────────────────────────────────
# Stage 4 — app runtime: small release layer on top of a runtime image
# ────────────────────────────────────────────────────────────────
FROM ${BASE_RUNTIME_IMAGE} AS runtime

COPY --link --from=backend-build /out/heya /usr/local/bin/heya
COPY --link --from=agent-cli-build /out/claude /usr/local/bin/claude
COPY --link --from=agent-cli-build /out/codex /opt/heya-agent-clis/codex
COPY --link --from=agent-cli-build /out/licenses /usr/share/doc/heya-agent-clis

RUN ln -s /opt/heya-agent-clis/codex/bin/codex /usr/local/bin/codex

# `docker exec -it <heya> codex ... login --device-auth` and Heya's Go
# adapter intentionally share this persistent credential directory.
ENV CODEX_HOME=/data/llm/agents/codex

ENTRYPOINT ["/usr/local/bin/heya"]
CMD ["serve"]
