# Deployment — container images

Heya ships as a single self-contained binary with `serve` and `worker` runtime
roles. Production runs both roles from the same image. There are three
application flavours,
plus an opt-in all-in-one (`-aio`) form of each, published to
`ghcr.io/heyamedia/heya` on every `vX.Y.Z` tag:

| Tags | Arch | ONNX (sonic-analysis) | Video transcode | Database |
| --- | --- | --- | --- | --- |
| `:<ver>` / `:<ver>-aio` | amd64 **+ arm64** | CPU | Intel/AMD **VAAPI + QSV** | external / bundled |
| `:<ver>-cuda` / `:<ver>-cuda-aio` | amd64 | **CUDA + TensorRT** | **NVENC/NVDEC** | external / bundled |
| `:<ver>-openvino` / `:<ver>-openvino-aio` | amd64 | **OpenVINO** (Intel iGPU/Arc) | VAAPI + QSV | external / bundled |

The GPU variants are thin layers built **FROM the base image** (same heya
binary + jellyfin-ffmpeg), adding only the vendor GPU runtime + a GPU-enabled
ONNX Runtime. Pick the one image that matches your GPU; the base covers
everyone for transcode and CPU inference.

Regular images use an external Postgres selected by `HEYA_DATABASE_URL`.
All-in-one images add PostgreSQL 17, pgvector, and supervisord directly on top
of the corresponding completed regular image. Supervisor runs the database,
API/ingress, and worker as independent processes. All persistent state lives
below `/data`; always mount a volume there.

## One-command all-in-one

```bash
mkdir -p "$HOME/heya-data"
docker run -d --name heya \
  -p 8080:8080/tcp -p 8080:8080/udp \
  -v "$HOME/heya-data:/data" \
  -v "/path/to/your/media:/media:ro" \
  -e HEYA_ADMIN_USERNAME=admin \
  -e HEYA_ADMIN_PASSWORD='replace-with-a-long-passphrase' \
  --restart unless-stopped \
  ghcr.io/heyamedia/heya:latest-aio
```

This initializes PostgreSQL on first boot, waits for it to become ready, then
runs PostgreSQL, `heya serve`, and `heya worker` under supervisord. Heya applies its own migrations,
including pgvector, as usual. PostgreSQL listens only on container loopback and
port 5432 is not exposed; this image is intentionally a single-container unit.
Use the regular image and external Postgres when the database must be shared,
backed up independently, or managed separately.

Replace `/path/to/your/media` with an absolute host path, then create a library
pointing at `/media` under Settings → Libraries. The read-only (`:ro`) mount is
recommended unless Heya needs to write NFO or other sidecar files. Multiple
collections can be mounted independently, for example
`-v /mnt/movies:/media/movies:ro` and `-v /mnt/music:/media/music:ro`.

`$HOME/heya-data` contains the bundled PostgreSQL cluster plus Heya's images,
models, caches, and service state. A named volume such as
`-v heya-data:/data` works, but a direct host-path mount is strongly recommended:
it is visible on the host and much easier to inspect, back up, and migrate.
Whichever form you choose, never run the AIO image without persistent `/data`.

The two `HEYA_ADMIN_*` variables create the administrator only when the
database does not already contain one. Passwords shorter than 15 characters
are rejected. Subsequent container restarts do not reset it; remove the
bootstrap password from the deployment configuration after first boot. For a
`docker run` deployment, recreate the container with the same `/data` mount
but without `HEYA_ADMIN_PASSWORD`; the account remains in PostgreSQL while the
plaintext bootstrap secret disappears from the container configuration.

Heya's production edge is embedded Caddy and requires HTTPS on `HEYA_PORT`.
Plain HTTP on that same port receives a permanent HTTPS redirect. First boot
creates a private CA at `/data/caddy/pki/authorities/local/root.crt`; install
that root on LAN clients you want to trust. Publish both TCP and UDP as shown
above—TCP carries HTTP/1.1/HTTP/2 and UDP carries HTTP/3 (QUIC).

The zero-configuration database credentials are internal-only `heya` / `heya`.
They can be changed on first boot with `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
`POSTGRES_DB`, but a matching `HEYA_DATABASE_URL` must then also be provided.
Changing those variables does not rewrite an existing cluster in `/data`.

GPU forms use the same host flags as their regular counterparts:

```bash
# NVIDIA
docker run -d --name heya --gpus all -p 8080:8080/tcp -p 8080:8080/udp \
  -v "$HOME/heya-data:/data" -v "/path/to/your/media:/media:ro" \
  -e HEYA_ADMIN_USERNAME=admin -e HEYA_ADMIN_PASSWORD='replace-with-a-long-passphrase' \
  ghcr.io/heyamedia/heya:latest-cuda-aio

# Intel OpenVINO
docker run -d --name heya -p 8080:8080/tcp -p 8080:8080/udp \
  -v "$HOME/heya-data:/data" -v "/path/to/your/media:/media:ro" \
  -e HEYA_ADMIN_USERNAME=admin -e HEYA_ADMIN_PASSWORD='replace-with-a-long-passphrase' \
  --device /dev/dri:/dev/dri \
  --group-add "$(getent group render | cut -d: -f3)" \
  ghcr.io/heyamedia/heya:latest-openvino-aio
```

## Docker Compose

The repo's [`docker-compose.yml`](../docker-compose.yml) runs the full stack:
pgvector Postgres plus separate API and worker containers from the released
base image. Only the API publishes `:8080` TCP+UDP.

```bash
docker compose up -d                    # Postgres + API + worker
docker compose pull && docker compose up -d # update both Heya roles together
```

The compose file carries commented-out blocks for the common extras — admin
bootstrap, declarative `HEYA_LIBRARY_<N>_*` libraries, media mounts, and
`/dev/dri` passthrough for hardware transcode. Uncomment what you need.
Application containers set `no-new-privileges` and drop every Linux capability
except the three filesystem capabilities needed to work with host-owned
`/data` bind mounts; neither Heya role receives raw-network, device-node, or
privileged-port capabilities.
(`make db-up` starts only the `postgres` service — that's the dev flow, and it
shares this file.)

Library paths are absolute paths inside the container. For NAS or other network
storage, mount the share on the host and bind-mount it at the same path into the
serve and worker containers. Configure that mounted path in Heya; transport
URLs such as `smb://…` are intentionally rejected.

## Public exposure hardening

Heya's public listener is the embedded Caddy edge, not a bare application
server. It applies bounded request bodies and headers, connection timeouts,
security response headers, login/registration throttles by client IP and
account, and a bounded password-verification concurrency limit. Failed logins
return one generic error so they do not disclose whether an account exists.

First-user registration is opt-in and disabled by default:

```env
HEYA_ENABLE_REGISTRATION=false
```

Prefer the `HEYA_ADMIN_*` first-boot bootstrap for unattended deployments. If
you enable registration, do it only for the short enrollment window; the
endpoint closes permanently once the first user exists. New passwords are
Argon2id-hashed and require at least 15 characters. Existing bcrypt hashes are
accepted and upgraded after the next successful full-password login. A password
change keeps only the credential that authorized it and revokes other sessions
and API tokens; administrative resets revoke every credential for the affected
account.

Coraza and the OWASP Core Rule Set are built into the regular and AIO binaries:

```env
HEYA_WAF_MODE=detect # off | detect | block
```

`detect` logs matches without blocking and is the safe default. Review those
events with your actual clients before selecting `block`; media servers carry
unusual range requests, manifests, metadata, and third-party client traffic
that deserve a tuning period. Authorization, cookie, and password values are
excluded from CRS inspection to avoid placing credentials in audit events.

The ruleset is pinned as a Go dependency. Dependabot proposes grouped weekly
minor/patch upgrades for Coraza and CRS, which keeps updates reviewable and
reproducible. Heya deliberately does not fetch executable rules at runtime:
an unattended rule update can introduce false positives or become a
supply-chain path without passing CI or producing a new immutable image.

For an Internet-facing deployment also restrict the host firewall to the ports
you intentionally publish, keep PostgreSQL private (the supplied Compose maps
its development port to loopback only), update Heya regularly, and keep
`/data` backups. UDP 8080 is needed only if you want HTTP/3. Tailscale remains
the lower-exposure option when access does not need to be public.

### Query diagnostics with pg_stat_statements

The bundled Compose and all-in-one PostgreSQL configurations preload
`pg_stat_statements`; Heya then creates the extension automatically when its
database role has permission. Existing containers need one PostgreSQL restart
after updating so the preload setting takes effect:

```bash
docker compose up -d --force-recreate postgres
```

For an external PostgreSQL server, add the following to `postgresql.conf` (or
the equivalent managed-database parameter group), restart PostgreSQL, and
create the extension in Heya's database:

```conf
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
```

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Without preload or extension privileges Heya remains fully functional and its
Diagnostics page falls back to the bounded per-process query tracer.

## Production process topology

Regular images do not supervise multiple processes. Every deployment must run:

- one `heya serve` process for Caddy, API/SPA, WebSockets, Tailscale, UPnP, and
  casting; and
- exactly one `heya worker` process for River, filesystem watchers, schedules,
  orphan recovery, and background model work.

Both roles need the same release image, `HEYA_DATABASE_URL`, `/data`, media
mount paths, and relevant GPU devices. Only `serve` binds ports or needs LAN
multicast networking. Kubernetes should use `args: ["worker"]` for the worker
container because the image entrypoint is already `/usr/local/bin/heya`.
Settings → Watchers reports the worker heartbeat and active paths; readiness
also reports a degraded `worker` component when that heartbeat is missing.

The direct `docker run` examples below show the serving container. When using a
regular image, launch a second container with the same environment, mounts,
devices, and image, no published ports, and append `worker` to its command.
Docker Compose already does this correctly.

## Base image — CPU + Intel/AMD transcode

```bash
docker run -p 8080:8080/tcp -p 8080:8080/udp -v $PWD/data:/data \
  -e HEYA_DATABASE_URL='postgres://heya:heya@db:5432/heya?sslmode=disable' \
  ghcr.io/heyamedia/heya:latest
```

Hardware **video transcode** (Intel Arc/iGPU + AMD) needs only the render node
passed in — jellyfin-ffmpeg bundles the VAAPI/QSV drivers (incl. AMD Mesa and
AV1 on Arc), so no extra packages:

```bash
docker run -p 8080:8080/tcp -p 8080:8080/udp -v $PWD/data:/data \
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
docker run --gpus all -p 8080:8080/tcp -p 8080:8080/udp -v $PWD/data:/data \
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
docker run -p 8080:8080/tcp -p 8080:8080/udp -v $PWD/data:/data \
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
make docker-cuda            # heya:cuda      (amd64, app image on CUDA runtime)
make docker-openvino        # heya:openvino  (amd64, app image on OpenVINO runtime)
# Overlay a completed local or published image:
docker build -f .docker/Dockerfile.aio --build-arg BASE_IMAGE=heya:base -t heya:aio .
make docker-run             # run base against the compose Postgres
make docker-run-gpu         # run base with /dev/dri for hw transcode
make docker-multiarch IMAGE=ghcr.io/...  # push base as one amd64+arm64 manifest
```

> **amd64 build note:** the Nuxt prerender step (`bun run build`) is markedly
> slower under bun on amd64 than arm64 — an amd64 base build spends a few extra
> minutes at that step. It is **not** a hang; let it finish. (Native arm64,
> e.g. an Apple-Silicon `make docker`, is fast.)

CI (`.github/workflows/container.yml`) builds the app binary on a tag and
layers it onto prebuilt runtime images. Runtime images are built by
`.github/workflows/runtime.yml` every Saturday and can be run manually when
ffmpeg, ONNX Runtime, CUDA, or OpenVINO dependencies change.

## Casting from containers (mDNS reality check)

Cast discovery is a **pure-Go multicast mDNS browse** — no avahi needed in
the image — and multicast discovery is the intended, zero-config path.
But multicast never crosses a container/pod network boundary, and it
never crosses subnets/VLANs at all (it's link-local). **The fix is to
give the container a real network presence on the receivers' L2**, not to
configure devices by hand:

- `hostNetwork: true` (+ `dnsPolicy: ClusterFirstWithHostNet`) when the
  node itself sits on the receivers' subnet, or
- a **macvlan/ipvlan attachment** (Multus on k8s, `docker network create
  -d macvlan …` elsewhere) giving the container an IP on the receivers'
  VLAN, or
- an **mDNS reflector on the router** (UniFi "Multicast DNS",
  avahi-reflector) when server and receivers must stay on separate VLANs.

The browse loop re-enumerates interfaces every cycle, so an attached leg
starts discovering within a minute — no restart, no config. Diagnose all
of this in **Settings → Casting**: it lists the server's network legs
next to the discovered receivers, so a subnet mismatch is visible at a
glance.

Streaming additionally needs plain unicast reachability to the receiver
(`:7000` RTSP + UDP, replies routing back). Verify from inside the
container: `ffmpeg -v error -i tcp://<receiver>:7000?timeout=3000000 -f null -`
— a fast "Connection refused" is *good* (routable), a timeout is not.

**Last resort — pinned receivers.** For networks that filter multicast
(some switches/APs do) there's `HEYA_CAST_DEVICES=192.168.1.216,…` (or
the editable field in Settings → Casting): each address is resolved by a
direct unicast mDNS query on `:5353`. Receivers enforce RFC 6762's
source-address check and only answer unicast from their **own subnet**,
so this cannot cross VLANs — it is not a substitute for the options
above.

**Chromecast/DLNA media return path.** URL-pull receivers do not need Heya to
be public on the internet. They do need to fetch media from Heya on their own
LAN. By default Heya asks the kernel which local interface routes to each
receiver and signs a URL under `https://<that-interface-IP>:HEYA_PORT`; Settings
→ Casting shows the selected media origin beside every discovered device. This
automatically pairs a receiver on one VLAN with Heya's leg on that VLAN instead
of leaking a loopback, Tailscale, or unrelated interface address.

When the selected address is a container/pod IP the receiver cannot route to,
set **Receiver media URL** in Settings → Casting (or
`HEYA_CAST_BASE_URL=https://<LAN-reachable-Heya-address>:<port>`). This is an
origin only—no path/query—and should normally remain private to the LAN. A
URL-pull receiver that cannot trust Heya's private CA needs an explicit
browser-trusted HTTPS origin here. Cast
media URLs contain a short-lived token scoped to the exact resource and user;
they never contain the user's normal Heya bearer token.

## Version lockstep (maintainers)

The one heya binary must agree on an ONNX Runtime C-API version with **every**
image's `libonnxruntime`. The only prebuilt ORT+OpenVINO is the
`onnxruntime-openvino` wheel, which pins **ORT 1.24.1**. So these move together
and must never be bumped in isolation:

- `go.mod` → `github.com/yalue/onnxruntime_go v1.27.0` (ORT API 24)
- `.docker/Dockerfile.cpu`, `.docker/Dockerfile.cuda` → `ONNXRUNTIME_VERSION=1.24.1`
- `.docker/Dockerfile.openvino` → `ONNXRUNTIME_OPENVINO_VERSION=1.24.1`

A mismatch fails sonic-analysis init at runtime with
`Error setting ORT API base: 2` (`GetApi(N)` → NULL). Keep `go.sum` free of
stale higher-version entries, which can silently revert the pin.
