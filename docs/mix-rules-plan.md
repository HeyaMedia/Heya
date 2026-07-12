# Mixes for You — Rule Engine Plan

Status: **phases 1+2 shipped** (2026-07-12) — affinity CTE (plays w/
completion/skip/decay + reaction bands, dislike = hard veto), liked-track
taste centroids per artist, track-level KNN expansion, personal-affinity
core, day-seeded exploration share, version dedup; dislike veto also
enforced in instant radio + AI mix builder. Legacy generator remains the
cold-start fallback. Remaining: archetype slate (phase 3), mood/BPM arcs +
serving memory (phase 4), scrobble import UI polish.

## Why mixes feel rigid today

`GenerateMixesForUser` (internal/service/music_home.go) does, per mix:
top-played artist (30d raw play count) → artist-centroid KNN (10 neighbors)
→ top-3 tracks per artist by **global** play count → adjacency shuffle.

Four structural problems:

1. **Whole-artist seeding.** The seed vector is `artist_centroids.sonic_centroid`
   = AVG over the artist's entire catalog. An artist with range (Babymetal's
   kawaii + metal, Ado's ballads + bangers) averages into a mush point that
   represents none of their music — and the *user's* taste within that artist
   (which tracks they actually play) never enters the equation.
2. **Global track fill.** Tracks inside a mix are ranked by server-wide play
   count, not the user's. The same top-3 per artist appears every time.
3. **Determinism.** Same seeds → same neighbors → same tracks. The only
   "rotation" is a 1h cache TTL that regenerates the identical result.
   No exploration, no memory of what was served yesterday.
4. **One archetype.** Every mix is "Inspired by <artist>". There are no
   genre, mood, discovery, or rediscovery mixes, even though `track_facets`
   already carries everything needed (per-track CLAP embedding, BPM, key,
   `top_genres`, `mood_tags`) and `play_events` carries `completed` +
   `listened_seconds` that nothing reads.

## Layer 0 — the taste model (foundation for every rule)

One concept powers everything: a per-user, per-track **affinity score**,
computed live (CTE; the data volume makes this cheap for years) and cached
~15 min:

```
affinity(track) =
    Σ over play_events(user, track):
        weight(event) × decay(event)
  + 3.0 if the track is loved (user_favorites)
  + 1.0 if its album is loved, 0.5 if its artist is loved

weight(event):
    completed                        → 1.0
    listened 30–80% of duration      → 0.4
    listened < 25% and not completed → −0.6   ← the skip signal, free
decay(event) = 0.5 ^ (age_days / 14)          ← half-life 14 days
```

Everything downstream derives from affinity:

- **Liked-track set**: tracks with affinity above a small threshold.
- **Per-artist taste centroid**: AVG(`track_embedding`) over the user's
  liked tracks *of that artist* — NOT the artist's catalog centroid. This is
  the single most important change in this plan.
- **Genre affinity**: Σ affinity × `top_genres` score → "you're on a
  hardstyle binge" falls out of the math for free.
- **Mood/BPM affinity**: same aggregation over `mood_tags` / `bpm`.

The skip signal costs no schema change (`listened_seconds`/`completed`
already exist) and also slightly penalizes the skipped track's immediate
KNN neighborhood during fill.

## Layer 1 — mix archetypes (the rule block)

A slate of ~6–8 mixes assembled from a Go rule table:

```go
type MixArchetype struct {
    Key        string
    Title      func(seed) string        // "Hardstyle Mix", "Deeper into Ado"
    MinSignal  SignalTier               // cold / sparse / rich
    Seed       func(taste) []seedVec    // where the mix points
    Fill       func(seeds, rng) []track // how slots are filled
    Constraints Constraints             // caps, dedup, freshness quotas
}
```

| # | Archetype | Seed | Fill rule | Ships the user's ask |
|---|-----------|------|-----------|----------------------|
| 1 | **Artist mix** ("Ado Mix") | user's liked-track centroidS for a top artist — one seed vec per liked-track *cluster*, so an artist's ballads and bangers both survive | track-level KNN around each seed (HNSW on `track_facets.track_embedding` already exists); ~30% seed artist, 70% neighbors ranked by user affinity then distance | "seed from the tracks I like, not the artist as a whole" |
| 2 | **Genre mix** ("Hardstyle Mix") | top 1–2 genres by *recent* genre affinity | high-affinity tracks tagged that genre + KNN-adjacent, with a 25% quota of never-played tracks in the genre | "I listen to a lot of hardstyle lately → make a mix from it" |
| 3 | **Mood / energy mix** ("High Energy", "Late Night Chill") | dominant recent `mood_tags` bands + BPM band | filter taste neighborhood by mood/BPM window; time-of-day naming optional | sonic-criteria mixes |
| 4 | **Discovery mix** ("New Discoveries") | overall taste centroid(s) | ONLY tracks with zero plays, biased toward artists with zero plays, ranked by embedding distance to taste | "doesn't really wanna branch out" — this one exists to branch out |
| 5 | **Deep cuts** ("Deeper into Ado") | top artist | that artist's *unplayed* tracks ranked by similarity to the user's liked tracks of the artist | catalog exploration inside loved artists |
| 6 | **Rediscovery** ("Time Capsule") | high lifetime affinity, not played in 60d+ | affinity rank with a wide artist spread | resurfaces shelved favorites |
| 7 | **On Repeat+** ("Your Week") | last-7-day heavy rotation | half the heavy rotation itself, half fresh KNN neighbors of it | recency-forward mirror |

Slate assembly rules: each archetype declares the signal tier it needs; a
track appears in at most one mix per slate; at most 2 mixes share a seed
artist; archetypes that fail their quality bar (too few candidates above a
distance cutoff — same `best + margin` trick as the AI mix builder) drop out
rather than pad.

## Layer 2 — anti-rigidity mechanics

- **Exploration share.** ~20–25% of every mix's slots are "stretch" picks:
  sampled (softmax over distance, temperature ~0.3) from KNN rank 20–60
  instead of argmax from the top-10. Seeded RNG keyed by
  `(user, archetype, day-bucket)` — stable across a day, different tomorrow.
  This alone kills most of the "stuck to its seed" feel.
- **Serving memory.** Remember the track ids served in the last K slates per
  archetype (one small table or a rolling hash); tracks served yesterday get
  a penalty today. Guarantees visible rotation even with unchanged listening.
- **Personal fill.** Fill ranks by user affinity → distance → global count
  (today it's global count only).
- **Sequencing.** Reuse the deterministic arc sequencer from the AI mix
  builder (BPM rising/waves). `key_root`/`key_mode` exist per track —
  harmonic-adjacency (Camelot ±1) as a soft tiebreak is a cheap polish.

## Cold-start ladder (prod reality: ~51 plays, 0 loves, 12% analyzed)

| Tier | Signal | Slate |
|------|--------|-------|
| cold | < 20 weighted plays | genre mixes from library composition, "Library Sampler" random-walk KNN mixes from popular seeds |
| sparse | 20–200 | every played track is precious: seed from ALL of them, wide exploration share, artist + genre + discovery archetypes |
| rich | 200+ | full slate |

Two force multipliers outside this plan's scope but worth queueing:

1. **Scrobble import** (ListenBrainz / last.fm / `.scrobbler.log`) — turns
   years of external listening history into instant affinity. The single
   biggest lever for mix quality given the sparse local history.
2. **Sonic analysis coverage** — mixes can only draw from analyzed tracks
   (12% today, alphabetical); every pump run widens the pool.

## Non-goals

- No LLM involvement — this stays pure math/SQL (fast, free, offline). The
  AI Mix Builder remains the narrative-brief tool.
- No collaborative filtering (single-digit users per server; embeddings +
  affinity carry the load).
- No new heavy infra: live generation stays (fast SQL + HNSW), just with
  seeded variety instead of determinism.

## Phasing

1. **Affinity CTE + personal fill + liked-track artist seeding** — fixes
   "seeded from the artist as a whole" and the global-count fill. Small.
2. **Exploration share + serving memory + day-bucket seeding** — fixes
   rigidity. Small.
3. **Archetype table + genre/discovery/deep-cuts/rediscovery mixes** — the
   visible feature. Medium.
4. **Mood/BPM archetypes + arc sequencing + key adjacency** — polish.
5. (separate feature) Scrobble import.
