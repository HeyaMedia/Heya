# AI Subsystem + Smart Collections — Plan

Status: **foundation shipped** (2026-07-09), **first consumer shipped**
(2026-07-10). Phases 0 and 2–4 are live: `internal/llm` + local llama-server
runtime + `/api/ai/*` + `heya ai` CLI + Settings → AI page, verified
end-to-end on macOS/Metal and prod Intel/Vulkan. Phase 6 has its first slice:
AI-curated recommendations (`POST /api/ai/recommend`, `heya ai recommend`,
"Ask AI" on `/movies|tv/recommendations`) — LLM probes → embedding-KNN pool →
LLM fit-graded re-rank, code owns ordering ("AI proposes, code disposes").
Remaining: collections substrate (phase 1), collections curator (phase 5),
NL playlists, external-provider live test (needs a real key), CUDA-image
llama-server bake-in. Episode-overview
embeddings shipped (2026-07-10): `episode_facets` + two-source SemanticSearch
merge + matched-episode evidence in the AI re-rank — oblique "the one where…"
asks now resolve via episode text. Remaining ceiling: thematic/reception
depth ("it's a tearjerker") — heya.media reviews are the planned lift.

The original FUTURE.md idea was "smart playlists / collections". Working
through it, most of the value needs **no AI at all** (filter rules + metadata
we already ingest), and the part that does want AI generalizes into a reusable
subsystem that also unlocks natural-language playlists and "what should I
watch tonight". So the build order is: collections substrate (no AI) →
`internal/llm` foundation → AI consumers on top.

## Part 1 — Collections substrate (no AI)

### Tier 1: rule-based smart collections

A filter DSL over metadata we already store, saved as rule sets that evaluate
live (evergreen — new library items appear automatically):

- Axes: genre, year range, rating, vote count, keyword, studio/network,
  resolution, runtime, watched state, added-at. Music reuses the same DSL and
  adds sonic axes later (BPM, LUFS, decade).
- Examples: "80s Action" (`genre=Action AND year 1980..1989`), "Must See
  Comedies" (`genre=Comedy AND rating>=7.5 AND votes>5000`), "New This Week".
- Schema: `collections` (id, name, slug, description, kind ∈ {rules, pinned},
  `source ∈ {user, system, ai}`, artwork, sort) + `collection_rules` (the DSL,
  jsonb) + `collection_items` (ordered pins, for `kind=pinned`).
- Built in the web UI (rule builder), CLI-first via
  `heya collection create/list/eval`.

### Tier 2: system seed pack

~30 shipped collection definitions that are just *pinned rules* over metadata
we already have from heya.media:

- Studio collections: `studio = Studio Ghibli`, A24, Pixar…
- Keyword collections: `keyword = marvel cinematic universe` (TMDB 180547),
  time travel, heist, based-on-video-game…
- Shipped as static seed data with `source=system`; only materialize in the UI
  when the library actually matches (≥N items).

Non-goal: external list subscriptions (Trakt/MDBList/IMDb). That's Kometa's
product — per-service keys, stale lists, discovery-UX rabbit hole. Skipped.

Deferred: canonical **watch orders** (Rascal series+movies, Stargate
SG-1/Atlantis/Universe interleave) are relational *metadata*
(sequel/prequel edges + air dates from AniDB/AniList/TMDB) and belong in
heya.media eventually. The AI curator (below) covers them short-term; harden
with real relation data if it proves valuable.

## Part 2 — `internal/llm` foundation

### One client, two provenances

Everything speaks the **OpenAI-compatible chat API**. External providers
speak it natively; local mode is a **managed `llama-server` subprocess**
(llama.cpp), which also speaks it natively. One client, one code path.

Local mode is deliberately *not* ONNX (autoregressive LLMs through raw ORT
mean hand-rolled KV cache, sampling, tokenizer — onnxruntime-genai has no Go
bindings) and *not* cgo bindings (seed-hypermedia/llama-go would drag a C++
toolchain into `go build`, air, CI, cross-compile). If in-process ever looks
better, the purego/dlopen bindings (hybridgroup/yzma, dianlight/gollama.cpp)
match our onnxruntime_go pattern — the `internal/llm` interface must keep the
local runtime an implementation detail so that's a swap, not a rewrite.

### Interface sketch

```go
type Client interface {
    Complete(ctx, Request) (Response, error)          // chat completion
    CompleteJSON(ctx, Request, schema []byte) error   // schema-constrained, unmarshal into out
    Models(ctx) ([]Model, error)                      // provider model list (/v1/models)
    Status(ctx) (Status, error)                       // reachable? model loaded? which backend?
}
```

`CompleteJSON` is first-class, not an afterthought — the collections curator
depends on it. llama-server does grammar-constrained JSON (GBNF/json_schema)
natively, so the local model is the most reliable JSON emitter; external
providers use `response_format` where supported, validate+retry where not.

### Providers

Static preset table (~15 rows), no library needed: OpenAI, Anthropic (compat
endpoint), Google Gemini (compat endpoint), Groq, Mistral, DeepSeek, Together,
Fireworks, OpenRouter, xAI, Ollama (localhost), LM Studio (localhost), custom
base URL. Each row: name, base URL, auth header style, default model hint.
Model dropdown populates from `/v1/models`.

### Local runtime manager

- Downloads a pinned, checksummed `llama-server` build per platform + a GGUF
  from a curated model list — same download/verify infra as
  `internal/sonicanalysis` and `internal/textembed` models.
- Backend per artifact: CPU (base image, prebuilt), CUDA (cuda image,
  prebuilt), **Vulkan** (openvino image — ANV drivers now baked in), Metal
  (macOS dev, prebuilt). ggml backends are runtime-loadable
  (`GGML_BACKEND_DL`): one server binary, backend `.so`s beside it, auto-pick
  with CPU fallback — same shape as ORT execution providers.
  - SYCL: deferred; only if `llama-bench` on the A380 beats Vulkan enough to
    justify ~1 GB of oneAPI runtime libs + a oneAPI build stage.
  - llama.cpp OpenVINO backend: preview (2026.1) — watch it; would need its
    own co-located OV runtime (the wheel-bundled one is pinned by the ORT
    1.24.1 lockstep). Only path to Intel NPUs.
- Lifecycle: spawn on demand on a localhost port, health-check, **idle
  timeout → kill** (reclaims RAM), crash → restart with backoff. Subprocess
  isolation matters: ggml OOM/segfault on low-power boxes must not take Heya
  down. Remember the bufio.Scanner buffer rule on its pipes.
- Default model: curated dropdown (2–3 vetted GGUFs with size/RAM labels).
  Default pick: Qwen3-4B-Instruct class — Apache 2.0 (no HF license gate to
  click through, unlike Gemma), 256K-class native context. Context window is
  a knob defaulting modest (8–16K): KV cache eats gigabytes at large windows,
  and our grounded prompts don't need them.

### Config, CLI, UI

- Env → DB provenance as usual: `HEYA_AI_MODE ∈ {off, local, external}`
  (default **off**), `HEYA_AI_PROVIDER`, `HEYA_AI_API_KEY`, `HEYA_AI_MODEL`,
  `HEYA_AI_BASE_URL`, `HEYA_AI_CONTEXT`, local model choice + data dir.
- CLI-first: `heya ai status`, `heya ai test`, `heya ai chat "…"`,
  `heya ai models`.
- Settings UI: mode toggle, provider dropdown, key field, model select
  (fetched), test button; env-sourced fields greyed per provenance rules.

## Part 3 — AI consumers

### 3a. Collections curator (consumer #1)

Opt-in scheduled kickoff job (same family as trickplay/sonic — never
scan-triggered). Daily-ish. **The model proposes in the language of Tiers
1–2; deterministic code disposes.** It never emits free-form playlists.

1. Build grounded context: recent watch history, date/season, library facet
   summary (genre/keyword/studio/decade counts — never 10k titles), and for
   each recently-watched franchise the *actual owned related items* as
   explicit candidates (id + title + year).
2. `CompleteJSON` → collection proposals: either a Tier-1 rule set, or an
   ordered pin list that may only reference candidate ids. Anything else is
   rejected by validation.
3. Materialize into `collections` with `source=ai`. Lifecycle: unkept AI
   collections rotate on the next run; a "keep" action flips them to
   `source=user` (editable, permanent).

Hallucination surface ≈ zero: worst case is a boring collection, never a
nonexistent item or wrong id.

### 3b. Natural-language playlist ("make me a playlist")

Same grounded-context builder + `CompleteJSON`, user prompt instead of
schedule. Returns a pinned collection/playlist draft the user can edit before
saving.

### 3c. "What should I watch tonight?"

Prompt template over watch history + continue-watching + facets; returns
ranked suggestions with one-line reasons. Pairs with (doesn't replace) the
embedding-based recommendation engine — embeddings give similarity, the LLM
gives named, explained, seasonal picks.

## Phases

| # | Deliverable | Depends on |
|---|-------------|------------|
| 0 | Vulkan in openvino image | **done** (a0a49b5) |
| 1 | Collections substrate: tables, DSL, eval, CLI, seed pack, UI | — (no AI) |
| 2 | `internal/llm`: client, presets, `CompleteJSON`, config, `heya ai` CLI | **done** |
| 3 | Local runtime manager: downloads, subprocess lifecycle, per-image backends | **done** |
| 4 | Settings UI for AI | **done** |
| 5 | Collections curator job | 1 + 2 (works external-only before 3) |
| 6 | NL playlist + tonight recommendations | 2 (+1 for saving) — **first slice done**: AI-curated recommendations (probe → KNN pool → fit-graded re-rank) |

Phases 1 and 2 are independent — can interleave. Phase 5 is the proof that
the whole stack hangs together.

## Risks / open questions

- **RAM budgeting on low-power boxes**: Heya + ORT models + llama-server +
  KV cache. Mitigations: on-demand spawn, idle kill, modest default context,
  Q4 quants, small default model.
- **llama.cpp release velocity**: pin an exact tested tag + checksums; bump
  deliberately like the ORT lockstep.
- **API key storage**: lands in the DB via settings — decide whether config
  secrets need at-rest encryption before this ships beyond the homelab.
- **Multi-user**: watch history is per-user → curator runs per user. Fine
  locally; meterable cost on external providers. Start: admin-enabled,
  per-user opt-in.
- **Prompt injection**: low surface (media titles/keywords are the only
  quasi-user content), and the proposal-validation design bounds the blast
  radius to "weird collection name".
