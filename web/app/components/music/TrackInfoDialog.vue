<script setup lang="ts">
// Shared "Track information" modal — everything Heya knows about a track,
// rendered in the 2.0 grammar (mono section heads, hairline credit-row grid,
// monospace wrapped path). Driven by the singleton useTrackInfo() channel and
// mounted once globally (app.vue), so the central useMusicActions "Track info"
// item opens it from any track menu app-wide.
//
// Data sources — existing endpoints ONLY:
//   • /api/music/tracks/{id}  (MusicTrackDetail) — universal: title, album/
//     artist context, cover, per-file technical details.
//   • /api/music/tracks/{id}/facets (FacetsView) — optional ML/DSP analysis
//     (BPM, key, genres, mood); 404 before the analyzer reaches the track.
//   • prefetch (via useTrackInfo.prime / open(id, prefetch)) — the album page
//     supplies the richer TrackView fields MusicTrackDetail does not expose
//     yet: filesystem path, recording MBID, ISRC, explicit flag.
import type { TrackFile } from '~~/shared/types'

const { state, close } = useTrackInfo()
const { formatTime } = usePlayerBindings()

// Hoisted per gotcha #1 — never touch useNuxtApp()/useImage() inside computed.
const { $heya } = useNuxtApp()

interface MusicTrackDetailResp {
  id: number
  title: string
  track_number: number
  disc_number: number
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  album_cover_path: string
  album_integrated_lufs: string | null
  album_true_peak_db: string | null
  artist_id: number
  artist_name: string
  artist_slug: string
  lyrics_available: boolean
  lyrics_path: string
  files: TrackFile[]
}

interface FacetsResp {
  bpm?: number
  bpm_confidence?: number
  key?: { display?: string; root?: string; mode?: string; camelot?: string; clarity?: number }
  top_genres?: Array<{ name?: string; score?: number }>
  mood_tags?: Record<string, number>
  analyzed_at?: string
}

const detail = ref<MusicTrackDetailResp | null>(null)
const facets = ref<FacetsResp | null>(null)
const loading = ref(false)
const errored = ref(false)

// Fetch on every (open × trackId) change, sequence-guarded so a slow response
// for a track the user already navigated away from can't overwrite a newer one.
let seq = 0
watch(
  () => [state.value.open, state.value.trackId] as const,
  async ([open, id]) => {
    if (!open || !id) return
    const mine = ++seq
    loading.value = true
    errored.value = false
    detail.value = null
    facets.value = null
    try {
      const d = (await $heya('/api/music/tracks/{id}', { path: { id } })) as unknown as MusicTrackDetailResp
      if (mine !== seq) return
      detail.value = d
    } catch {
      if (mine !== seq) return
      errored.value = true
    } finally {
      if (mine === seq) loading.value = false
    }
    // Facets are best-effort; a 404 (unanalyzed track) just hides the section.
    try {
      const f = (await $heya('/api/music/tracks/{id}/facets', { path: { id } })) as unknown as FacetsResp
      if (mine === seq) facets.value = f
    } catch { /* no facets yet */ }
  },
  { immediate: true },
)

const prefetch = computed(() => state.value.prefetch)

// Prefer the richer prefetch files (album TrackView) when present, else the
// fetched detail's — identical shape either way.
const files = computed<TrackFile[]>(() => prefetch.value?.files ?? detail.value?.files ?? [])

const coverUrl = computed(() => {
  const d = detail.value
  if (!d?.artist_slug || !d?.album_slug) return null
  return useAlbumCoverUrl(d.artist_slug, d.album_slug)
})

const openModel = computed({
  get: () => state.value.open,
  set: (v: boolean) => { if (!v) close() },
})

// ── Formatting helpers ──────────────────────────────────────────────────────
function fmtKhz(hz?: number | null) {
  if (!hz) return null
  return `${(hz / 1000).toFixed(1).replace(/\.0$/, '')} kHz`
}
function fmtNum(s: string | null | undefined, suffix: string) {
  if (s == null || s === '') return null
  const n = parseFloat(s)
  if (Number.isNaN(n)) return null
  return `${n.toFixed(1)} ${suffix}`
}
function pct(v?: number | null) {
  if (v == null) return null
  return `${Math.round(v * 100)}%`
}

const topMood = computed(() => {
  const m = facets.value?.mood_tags
  if (!m) return [] as Array<{ k: string; v: number }>
  return Object.entries(m)
    .map(([k, v]) => ({ k, v: Number(v) }))
    .sort((a, b) => b.v - a.v)
    .slice(0, 6)
})

const hasSonic = computed(() => {
  const f = facets.value
  return !!f && ((f.bpm ?? 0) > 0 || !!f.key?.display || (f.top_genres?.length ?? 0) > 0 || topMood.value.length > 0)
})
</script>

<template>
  <AppDialog v-model="openModel" size="lg" content-class="tid" :title="detail?.title || 'Track information'">
    <div v-if="loading && !detail" class="tid-state">Loading…</div>
    <div v-else-if="errored && !detail" class="tid-state tid-state-bad">Couldn't load this track.</div>

    <div v-else-if="detail" class="tid-grid">
      <!-- Cover + identity -->
      <aside class="tid-aside">
        <div class="tid-cover">
          <Poster :idx="detail.album_id" :src="coverUrl" aspect="1/1" :width="360" class="tid-cover-img" />
        </div>
        <NuxtLink
          :to="`/music/artist/${detail.artist_slug}/${detail.album_slug}`"
          class="tid-album"
          @click="close"
        >{{ detail.album_title }}</NuxtLink>
        <NuxtLink
          :to="`/music/artist/${detail.artist_slug}`"
          class="tid-artist"
          @click="close"
        >{{ detail.artist_name }}</NuxtLink>
      </aside>

      <div class="tid-main">
        <!-- Metadata -->
        <section class="tid-sec">
          <h4 class="tid-h">Metadata</h4>
          <dl class="tid-rows">
            <div class="tid-row"><dt>Title</dt><dd>{{ detail.title }}</dd></div>
            <div class="tid-row"><dt>Artist</dt><dd>{{ detail.artist_name }}</dd></div>
            <div class="tid-row"><dt>Album</dt><dd>{{ detail.album_title }}</dd></div>
            <div v-if="detail.album_year" class="tid-row"><dt>Year</dt><dd>{{ detail.album_year }}</dd></div>
            <div class="tid-row"><dt>Track</dt><dd>{{ detail.track_number || '—' }}</dd></div>
            <div class="tid-row"><dt>Disc</dt><dd>{{ detail.disc_number || '—' }}</dd></div>
            <div class="tid-row"><dt>Duration</dt><dd>{{ formatTime(detail.duration) }}</dd></div>
            <div v-if="prefetch?.explicit" class="tid-row"><dt>Advisory</dt><dd><span class="tid-tag">Explicit</span></dd></div>
            <div v-if="prefetch?.isrc" class="tid-row"><dt>ISRC</dt><dd class="tid-mono">{{ prefetch.isrc }}</dd></div>
            <div v-if="prefetch?.recording_mbid" class="tid-row"><dt>Recording MBID</dt><dd class="tid-mono">{{ prefetch.recording_mbid }}</dd></div>
            <div class="tid-row"><dt>Lyrics</dt><dd>{{ detail.lyrics_available ? 'Available' : 'None' }}</dd></div>
          </dl>
        </section>

        <!-- File(s) -->
        <section v-if="files.length" class="tid-sec">
          <h4 class="tid-h">
            {{ files.length > 1 ? `Files` : 'File' }}
            <span v-if="files.length > 1" class="tid-h-count">{{ files.length }}</span>
          </h4>
          <div v-for="(f, i) in files" :key="f.id" class="tid-file" :class="{ 'tid-file-gap': i > 0 }">
            <div class="tid-file-head">
              <span class="tid-file-q">{{ formatTrackQuality(f) || (f.format || '').toUpperCase() || 'Unknown' }}</span>
              <span v-if="f.size_bytes" class="tid-file-sz">{{ formatBytes(f.size_bytes) }}</span>
            </div>
            <dl class="tid-rows">
              <div v-if="f.format" class="tid-row"><dt>Codec</dt><dd>{{ f.format.toUpperCase() }}</dd></div>
              <div v-if="f.bitrate_kbps" class="tid-row"><dt>Bitrate</dt><dd>{{ f.bitrate_kbps }} kbps</dd></div>
              <div v-if="fmtKhz(f.sample_rate_hz)" class="tid-row"><dt>Sample rate</dt><dd>{{ fmtKhz(f.sample_rate_hz) }}</dd></div>
              <div v-if="f.bit_depth" class="tid-row"><dt>Bit depth</dt><dd>{{ f.bit_depth }}-bit</dd></div>
              <div v-if="f.channels" class="tid-row"><dt>Channels</dt><dd>{{ f.channels }}</dd></div>
              <div v-if="fmtNum(f.integrated_lufs, 'LUFS')" class="tid-row"><dt>Loudness</dt><dd>{{ fmtNum(f.integrated_lufs, 'LUFS') }}<span v-if="fmtNum(f.true_peak_db, 'dBTP')" class="tid-dim"> · {{ fmtNum(f.true_peak_db, 'dBTP') }}</span></dd></div>
            </dl>
          </div>
        </section>

        <!-- Filesystem path — only when a payload carried it (album page).
             MusicTrackDetail does not expose it; see report/DEFERRED. Shown to
             all users, mono + wrapping (matches the un-gated technical info the
             video stream-info panel surfaces). -->
        <section v-if="prefetch?.file_path" class="tid-sec">
          <h4 class="tid-h">Location</h4>
          <div class="tid-path">{{ prefetch.file_path }}</div>
        </section>

        <!-- Sonic analysis (facets) -->
        <section v-if="hasSonic" class="tid-sec">
          <h4 class="tid-h">Sonic analysis</h4>
          <dl class="tid-rows">
            <div v-if="(facets?.bpm ?? 0) > 0" class="tid-row">
              <dt>Tempo</dt>
              <dd>{{ Math.round(facets!.bpm!) }} BPM<span v-if="pct(facets?.bpm_confidence)" class="tid-dim"> · {{ pct(facets?.bpm_confidence) }} conf.</span></dd>
            </div>
            <div v-if="facets?.key?.display" class="tid-row">
              <dt>Key</dt>
              <dd>{{ facets.key.display }}<span v-if="facets.key.camelot" class="tid-dim"> · {{ facets.key.camelot }}</span></dd>
            </div>
          </dl>
          <div v-if="(facets?.top_genres?.length ?? 0) > 0" class="tid-chips">
            <span v-for="g in facets!.top_genres!.slice(0, 6)" :key="g.name" class="tid-chip">
              {{ g.name }}<small v-if="g.score">{{ Math.round((g.score ?? 0) * 100) }}</small>
            </span>
          </div>
          <div v-if="topMood.length" class="tid-chips tid-chips-mood">
            <span v-for="m in topMood" :key="m.k" class="tid-chip tid-chip-ghost">{{ m.k }}</span>
          </div>
        </section>
      </div>
    </div>
  </AppDialog>
</template>

<style scoped>
.tid-state { padding: 28px 4px; color: rgb(var(--ink) / 0.55); font-size: 14px; }
.tid-state-bad { color: var(--bad); }

.tid-grid {
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  gap: 26px;
  align-items: start;
}

/* aside: cover + album/artist links */
.tid-aside { position: sticky; top: 0; }
.tid-cover { position: relative; }
.tid-cover-img {
  border-radius: var(--r-md);
  box-shadow: var(--shadow-card);
}
.tid-album {
  display: block;
  margin-top: 14px;
  font: 650 15px var(--font-sans);
  color: rgb(var(--ink) / 0.92);
  text-decoration: none;
  line-height: 1.25;
}
.tid-album:hover { color: var(--tone); }
.tid-artist {
  display: block;
  margin-top: 3px;
  font: 500 11.5px var(--font-mono);
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.55);
  text-decoration: none;
}
.tid-artist:hover { color: var(--tone); }

.tid-main { min-width: 0; }
.tid-sec + .tid-sec { margin-top: 26px; }

/* mono section head */
.tid-h {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin: 0 0 12px;
  padding-bottom: 9px;
  border-bottom: 1px solid var(--hair);
  font: 600 11px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.6);
}
.tid-h-count { font-size: 11px; color: var(--tone); letter-spacing: 0.1em; }

/* credit-row grid (heya2.css .credits) */
.tid-rows { margin: 0; }
.tid-row {
  display: grid;
  grid-template-columns: 132px minmax(0, 1fr);
  gap: 16px;
  padding: 8px 0;
  border-bottom: 1px solid var(--hair);
  align-items: baseline;
}
.tid-row:last-child { border-bottom: 0; }
.tid-row dt {
  font: 600 10px var(--font-mono);
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
}
.tid-row dd {
  margin: 0;
  font-size: 13.5px;
  color: rgb(var(--ink) / 0.88);
  overflow-wrap: anywhere;
}
.tid-mono { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--ink) / 0.72); }
.tid-dim { color: rgb(var(--ink) / 0.5); }
.tid-tag {
  display: inline-block;
  font: 650 9.5px var(--font-mono);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  padding: 2px 7px;
  border-radius: 4px;
  color: var(--bad);
  background: rgb(var(--ink) / 0.06);
}

/* per-file block */
.tid-file-gap { margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--hair); }
.tid-file-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 4px;
}
.tid-file-q {
  font: 650 12px var(--font-mono);
  letter-spacing: 0.06em;
  color: var(--tone);
}
.tid-file-sz { font: 500 11.5px var(--font-mono); color: rgb(var(--ink) / 0.5); }

/* filesystem path */
.tid-path {
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.55;
  color: rgb(var(--ink) / 0.78);
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--hair);
  border-radius: var(--r-sm);
  padding: 10px 12px;
  overflow-wrap: anywhere;
  word-break: break-all;
}

/* sonic chips */
.tid-chips { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.tid-chips-mood { margin-top: 8px; }
.tid-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
  padding: 5px 11px;
  border-radius: 999px;
  font: 600 11px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--ink) / 0.82);
  background: rgb(var(--tone-rgb) / 0.1);
  border: 1px solid rgb(var(--tone-rgb) / 0.22);
}
.tid-chip small { font-size: 9px; color: rgb(var(--ink) / 0.45); }
.tid-chip-ghost {
  background: rgb(var(--ink) / 0.05);
  border-color: var(--hair);
  color: rgb(var(--ink) / 0.62);
  text-transform: capitalize;
}

@media (max-width: 620px) {
  .tid-grid { grid-template-columns: 1fr; gap: 18px; }
  .tid-aside {
    position: static;
    display: grid;
    grid-template-columns: 96px minmax(0, 1fr);
    gap: 14px;
    align-items: center;
  }
  .tid-cover { grid-row: span 2; }
  .tid-album { margin-top: 0; align-self: end; }
  .tid-row { grid-template-columns: 108px minmax(0, 1fr); gap: 12px; }
}
</style>
