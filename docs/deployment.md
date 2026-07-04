# Deployment — container images

Heya ships as a single self-contained binary (embedded Nuxt SPA + API + WS).
Production runs it from a container. There are **three image flavours**, all
published to `ghcr.io/heyamedia/heya` on every `vX.Y.Z` tag:

| Tag | Arch | ONNX (sonic-analysis) | Video transcode | Run flag |
| --- | --- | --- | --- | --- |
| `:<ver>` (+ `latest`) | amd64 **+ arm64** | CPU | Intel/AMD **VAAPI + QSV** | `--device /dev/dri` |
| `:<ver>-cuda` | amd64 | **CUDA + TensorRT** | **NVENC/NVDEC** | `--gpus all` |
| `:<ver>-openvino` | amd64 | **OpenVINO** (Intel iGPU/Arc) | VAAPI + QSV | `--device /dev/dri` |

The GPU variants are thin layers built **FROM the base image** (same heya
binary + jellyfin-ffmpeg), adding only the vendor GPU runtime + a GPU-enabled
ONNX Runtime. Pick the one image that matches your GPU; the base covers
everyone for transcode and CPU inference.

Postgres is always external — point at it with `HEYA_DATABASE_URL`. Data
(Tailscale state, transcode cache, sonic-analysis models, OpenVINO kernel
cache) lives under `/data`; mount a volume there.

## Docker Compose

The repo's [`docker-compose.yml`](../docker-compose.yml) runs the full stack:
pgvector Postgres plus the released base image on `:8080`.

```bash
docker compose up -d                                  # Postgres + heya:latest
docker compose pull heya && docker compose up -d heya # update to newest latest
```

The compose file carries commented-out blocks for the common extras — admin
bootstrap, declarative `HEYA_LIBRARY_<N>_*` libraries, media mounts, and
`/dev/dri` passthrough for hardware transcode. Uncomment what you need.
(`make db-up` starts only the `postgres` service — that's the dev flow, and it
shares this file.)

## Base image — CPU + Intel/AMD transcode

```bash
docker run -p 8080:8080 -v $PWD/data:/data \
  -e HEYA_DATABASE_URL='postgres://heya:heya@db:5432/heya?sslmode=disable' \
  ghcr.io/heyamedia/heya:latest
```

Hardware **video transcode** (Intel Arc/iGPU + AMD) needs only the render node
passed in — jellyfin-ffmpeg bundles the VAAPI/QSV drivers (incl. AMD Mesa and
AV1 on Arc), so no extra packages:

```bash
docker run -p 8080:8080 -v $PWD/data:/data \
  --device /dev/dri:/dev/dri \
  --group-add "$(getent group render | cut -d: -f3)" \
  -e HEYA_HWACCEL=vaapi \
  -e HEYA_DATABASE_URL=... \
  ghcr.io/heyamedia/heya:latest
```

`HEYA_HWACCEL` ∈ `auto|none|vaapi|qsv|nvenc|videotoolbox`. On a host with more
than one render node (e.g. an Intel + an AMD GPU), pass only the device you
want, or select it with the transcoder settings.

> **AMD note:** AMD GPUs do **transcode** only. There is no AMD path for
> sonic-analysis ONNX — the Go ONNX binding has no ROCm provider and consumer
> APUs aren't ROCm targets. Use the base image + `/dev/dri` for AMD.

## NVIDIA — `:<ver>-cuda`

GPU ONNX (CUDA/TensorRT) **and** NVENC transcode. Needs the [NVIDIA Container
Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/) on
the host; the CUDA *driver* libs are injected by `--gpus all`, while the CUDA
*toolkit* runtime ships in the image.

```bash
docker run --gpus all -p 8080:8080 -v $PWD/data:/data \
  -e HEYA_DATABASE_URL=... \
  ghcr.io/heyamedia/heya:latest-cuda
```

The image defaults `HEYA_SONIC_ACCELERATOR=cuda` and `HEYA_HWACCEL=nvenc`; both
degrade to CPU if no GPU/driver is present, so the image still boots without
`--gpus`. (Built + boots verified; the CUDA path is unverified on real NVIDIA
hardware in-repo.)

## Intel Arc / iGPU — `:<ver>-openvino`

GPU ONNX via the OpenVINO execution provider, plus QSV/VAAPI transcode. Pass
the Intel render node:

```bash
docker run -p 8080:8080 -v $PWD/data:/data \
  --device /dev/dri:/dev/dri \
  --group-add "$(getent group render | cut -d: -f3)" \
  -e HEYA_DATABASE_URL=... \
  ghcr.io/heyamedia/heya:latest-openvino
```

Defaults: `HEYA_SONIC_ACCELERATOR=openvino`, `HEYA_SONIC_OPENVINO_DEVICE=GPU`
(set `CPU`/`AUTO`/`GPU.1` to retarget), and `HEYA_SONIC_OPENVINO_CACHE_DIR=
/data/openvino-cache`. The OpenVINO GPU plugin JIT-compiles each model on first
inference (tens of seconds across the model set); the cache persists those
compiled kernels on the data volume so subsequent starts are fast (validated on
an Arc A380: ~37s cold → ~5.6s warm end-to-end). Keep `/data` persistent.

Validated end-to-end on an Intel Arc A380 (DG2): sonic-analysis runs on the GPU.

## Building locally

```bash
make docker                 # base, host arch (override: make docker PLATFORM=linux/amd64)
make docker-cuda            # heya:cuda      (amd64, FROM heya:base)
make docker-openvino        # heya:openvino  (amd64, FROM heya:base)
make docker-run             # run base against the compose Postgres
make docker-run-gpu         # run base with /dev/dri for hw transcode
make docker-multiarch IMAGE=ghcr.io/...  # push base as one amd64+arm64 manifest
```

> **amd64 build note:** the Nuxt prerender step (`bun run build`) is markedly
> slower under bun on amd64 than arm64 — an amd64 base build spends a few extra
> minutes at that step. It is **not** a hang; let it finish. (Native arm64,
> e.g. an Apple-Silicon `make docker`, is fast.)

CI (`.github/workflows/container.yml`) builds the base multi-arch on a tag,
then builds both GPU variants FROM that exact base digest and pushes all three.

## Version lockstep (maintainers)

The one heya binary must agree on an ONNX Runtime C-API version with **every**
image's `libonnxruntime`. The only prebuilt ORT+OpenVINO is the
`onnxruntime-openvino` wheel, which pins **ORT 1.24.1**. So these move together
and must never be bumped in isolation:

- `go.mod` → `github.com/yalue/onnxruntime_go v1.27.0` (ORT API 24)
- `Dockerfile`, `Dockerfile.cuda` → `ONNXRUNTIME_VERSION=1.24.1`
- `Dockerfile.openvino` → `ONNXRUNTIME_OPENVINO_VERSION=1.24.1`

A mismatch fails sonic-analysis init at runtime with
`Error setting ORT API base: 2` (`GetApi(N)` → NULL). Keep `go.sum` free of
stale higher-version entries, which can silently revert the pin.
