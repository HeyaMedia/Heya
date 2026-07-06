<template>
  <div class="ms-mb page-pad">
    <header class="ms-mb-head">
      <div>
        <h1 class="ms-mb-title">Mix Builder</h1>
        <div class="ms-mb-sub">Stack any combination of artists, albums, tracks, and vibes. We'll blend their sonic profiles into one queue.</div>
      </div>
    </header>

    <!-- Seed-kind tabs -->
    <div class="ms-mb-tabs">
      <button
        v-for="t in tabs"
        :key="t.kind"
        type="button"
        class="ms-mb-tab"
        :class="{ active: addKind === t.kind }"
        @click="setKind(t.kind)"
      >
        <Icon :name="t.icon" :size="15" />
        <span>{{ t.label }}</span>
      </button>
    </div>

    <div class="ms-mb-help">{{ activeTab.help }}</div>

    <!-- Add seed input -->
    <div class="ms-mb-seed">
      <!-- Vibe (free text) -->
      <div v-if="addKind === 'text'" class="ms-mb-input-row">
        <Icon name="sparkle" :size="16" class="ms-mb-input-icon" />
        <input
          ref="textInputEl"
          v-model="textInput"
          type="text"
          class="ms-mb-input"
          placeholder="describe a feeling — e.g. moody jazz at 2am"
          @keydown.enter.prevent="addVibeSeed"
        />
        <button
          type="button"
          class="ms-mb-add-btn"
          :disabled="!textInput.trim()"
          @click="addVibeSeed"
        >Add</button>
      </div>

      <!-- Track / Artist / Album — search-with-results -->
      <template v-else>
        <div class="ms-mb-input-row">
          <Icon name="search" :size="16" class="ms-mb-input-icon" />
          <input
            ref="searchInputEl"
            v-model="searchQ"
            type="text"
            class="ms-mb-input"
            :placeholder="`add an ${addKind}…`"
            autocomplete="off"
          />
        </div>

        <!-- Autocomplete dropdown -->
        <ul v-if="autocompleteResults.length" class="ms-mb-ac">
          <li
            v-for="r in autocompleteResults"
            :key="`${addKind}-${r.id}`"
            class="ms-mb-ac-row"
            @click="addAutocompleteSeed(r)"
          >
            <NuxtImg v-if="r.cover" :src="r.cover" :alt="r.title" loading="lazy" :class="addKind === 'artist' ? 'ac-art ac-art-round' : 'ac-art'" />
            <div v-else :class="addKind === 'artist' ? 'ac-art ac-art-round ac-art-empty' : 'ac-art ac-art-empty'"><Icon :name="activeTab.icon" :size="16" /></div>
            <div class="ms-mb-ac-meta">
              <div class="ms-mb-ac-title">{{ r.title }}</div>
              <div v-if="r.sub" class="ms-mb-ac-sub">{{ r.sub }}</div>
            </div>
            <Icon name="plus" :size="14" class="ms-mb-ac-add" />
          </li>
        </ul>
      </template>
    </div>

    <!-- Selected seed chips -->
    <div v-if="seeds.length" class="ms-mb-chips">
      <div class="ms-mb-chips-label">Mix from</div>
      <div class="ms-mb-chips-row">
        <button
          v-for="(s, i) in seeds"
          :key="`chip-${s.kind}-${i}`"
          type="button"
          class="ms-mb-chip"
          :class="`chip-${s.kind}`"
          :title="`Remove ${s.label}`"
          @click="removeSeed(i)"
        >
          <Icon :name="kindIcon(s.kind)" :size="12" />
          <span>{{ s.label }}</span>
          <Icon name="close" :size="11" class="chip-x" />
        </button>
        <button
          v-if="seeds.length > 1"
          type="button"
          class="ms-mb-chip-clear"
          @click="clearAll"
        >Clear all</button>
      </div>
    </div>

    <!-- Build controls -->
    <div class="ms-mb-controls">
      <div class="ms-mb-control">
        <label class="ms-mb-label">Tracks</label>
        <input v-model.number="trackCount" type="range" min="10" max="100" step="5" class="ms-mb-range" />
        <span class="ms-mb-count">{{ trackCount }}</span>
      </div>
      <button
        class="ms-mb-build-btn"
        :disabled="!canBuild || building"
        @click="buildMix"
      >
        <Icon name="sparkle" :size="15" />
        {{ building ? 'Building…' : 'Build Mix' }}
      </button>
    </div>

    <!-- Error -->
    <div v-if="buildError" class="ms-mb-error">{{ buildError }}</div>

    <!-- Results -->
    <section v-if="builtTracks.length" class="ms-mb-results">
      <div class="ms-mb-results-head">
        <div>
          <h2 class="section-title-lg">Your Mix</h2>
          <div class="ms-mb-results-sub">{{ builtTracks.length }} tracks · {{ formatTotalDuration(builtTracks) }}</div>
        </div>
        <div class="ms-mb-results-actions">
          <button class="ms-mb-action-btn" @click="playAll">
            <Icon name="play" :size="14" />
            <span>Play All</span>
          </button>
          <button class="ms-mb-action-btn" @click="onSaveAsPlaylist">
            <Icon name="plus" :size="14" />
            <span>Save as Playlist</span>
          </button>
          <button class="ms-mb-action-btn" @click="buildMix">
            <Icon name="refresh" :size="14" />
            <span>Re-roll</span>
          </button>
        </div>
      </div>

      <ul class="ms-mb-track-list">
        <li
          v-for="(t, i) in builtTracks"
          :key="`bt-${t.track_id}-${i}`"
          class="ms-mb-track-row"
          @click="playFrom(i)"
        >
          <div class="ms-mb-track-idx">{{ i + 1 }}</div>
          <div class="ms-mb-track-art">
            <NuxtImg :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" :width="160" :quality="80" densities="1x 2x" loading="lazy" />
            <div class="ms-mb-track-play"><Icon name="play" :size="13" /></div>
          </div>
          <div class="ms-mb-track-meta">
            <div class="ms-mb-track-title">{{ t.track_title }}</div>
            <div class="ms-mb-track-sub">{{ t.artist_name }} · {{ t.album_title }}</div>
          </div>
          <div class="ms-mb-track-dur">{{ formatDuration(t.duration) }}</div>
        </li>
      </ul>
    </section>

    <div v-if="!builtTracks.length && !building" class="ms-mb-empty">
      <Icon name="sparkle" :size="40" />
      <p>Add one or more seeds above and tap <strong>Build Mix</strong>.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'
import { refDebounced } from '@vueuse/core'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()
const playlistsApi = usePlaylists()

type SeedKind = 'text' | 'track' | 'artist' | 'album'

interface Seed {
  kind: SeedKind
  /** Resolved entity ID — unused for kind=text */
  id?: number
  /** Free-form text — used only for kind=text */
  text?: string
  /** Human-readable label rendered in the chip */
  label: string
}

const tabs: { kind: SeedKind; label: string; icon: string; help: string }[] = [
  { kind: 'text', label: 'Vibe', icon: 'sparkle', help: 'Describe a feeling. CLAP resolves it against your library.' },
  { kind: 'track', label: 'Track', icon: 'music', help: 'Pick a track. Its sonic profile shapes the mix.' },
  { kind: 'artist', label: 'Artist', icon: 'user', help: 'Pick an artist. Their sound shapes the mix.' },
  { kind: 'album', label: 'Album', icon: 'music', help: 'Pick an album. Its sonic average shapes the mix.' },
]

const addKind = ref<SeedKind>('text')
const activeTab = computed(() => tabs.find((t) => t.kind === addKind.value)!)

const textInput = ref('')
const searchQ = ref('')
const searchQDebounced = refDebounced(searchQ, 220)

const textInputEl = ref<HTMLInputElement>()
const searchInputEl = ref<HTMLInputElement>()

const seeds = ref<Seed[]>([])

const trackCount = ref(30)

// --- Autocomplete (when addKind is track/artist/album) ---
interface AcRow { id: number; title: string; sub: string; cover: string | null }

const autocompleteQuery = useQuery({
  queryKey: ['mix-builder', 'autocomplete', addKind, searchQDebounced],
  queryFn: async () => {
    if (searchQDebounced.value.length < 2) return [] as AcRow[]
    const r = await $heya('/api/search/quick', { query: { q: searchQDebounced.value } }) as unknown as {
      buckets: {
        music?: { items: { id: number; title: string }[] }
        albums?: { items: { id: number; title: string; year: string; artist_name: string; artist_slug: string; slug: string }[] }
        tracks?: { items: { id: number; title: string; album_title: string; artist_name: string; artist_slug: string; album_slug: string }[] }
      }
    }
    if (addKind.value === 'artist') {
      return (r.buckets?.music?.items ?? []).slice(0, 8).map((a) => ({
        id: a.id, title: a.title, sub: '', cover: usePosterUrl(a.id),
      } as AcRow))
    }
    if (addKind.value === 'album') {
      return (r.buckets?.albums?.items ?? []).slice(0, 8).map((al) => ({
        id: al.id,
        title: al.title,
        sub: `${al.artist_name}${al.year ? ' · ' + al.year : ''}`,
        cover: useAlbumCoverUrl(al.artist_slug, al.slug),
      } as AcRow))
    }
    if (addKind.value === 'track') {
      return (r.buckets?.tracks?.items ?? []).slice(0, 8).map((t) => ({
        id: t.id,
        title: t.title,
        sub: `${t.artist_name} · ${t.album_title}`,
        cover: useAlbumCoverUrl(t.artist_slug, t.album_slug),
      } as AcRow))
    }
    return [] as AcRow[]
  },
  enabled: () => addKind.value !== 'text' && searchQDebounced.value.length >= 2,
  staleTime: 1000 * 30,
})
const autocompleteResults = computed<AcRow[]>(() => autocompleteQuery.data.value ?? [])

function setKind(k: SeedKind) {
  addKind.value = k
  searchQ.value = ''
  nextTick(() => {
    if (k === 'text') textInputEl.value?.focus()
    else searchInputEl.value?.focus()
  })
}

function kindIcon(k: SeedKind): string {
  return tabs.find((t) => t.kind === k)?.icon ?? 'sparkle'
}

function addVibeSeed() {
  const t = textInput.value.trim()
  if (!t) return
  seeds.value.push({ kind: 'text', text: t, label: t })
  textInput.value = ''
}

function addAutocompleteSeed(r: AcRow) {
  // De-dupe — same kind + same id should add only once.
  if (seeds.value.some((s) => s.kind === addKind.value && s.id === r.id)) return
  const label = r.sub ? `${r.title} — ${r.sub.split(' · ')[0]}` : r.title
  seeds.value.push({ kind: addKind.value, id: r.id, label })
  searchQ.value = ''
  searchInputEl.value?.focus()
}

function removeSeed(i: number) {
  seeds.value.splice(i, 1)
}
function clearAll() {
  seeds.value = []
}

const canBuild = computed(() => seeds.value.length > 0)

// --- Build ---
interface RichTrackRow {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  distance?: number
}

const building = ref(false)
const buildError = ref<string | null>(null)
const builtTracks = ref<RichTrackRow[]>([])

async function buildMix() {
  if (!canBuild.value || building.value) return
  building.value = true
  buildError.value = null
  try {
    const payloadSeeds = seeds.value.map(seedToPayload)
    // Backend requires `seed` even when `seeds` is the source of truth — Huma
    // refuses to validate an object missing the field. Pass the first one as
    // a no-op placeholder; the resolver ignores it when seeds[] is non-empty.
    const body = {
      limit: trackCount.value,
      seed: payloadSeeds[0] as never,
      seeds: payloadSeeds as never,
    }
    const res = await $heya('/api/music/radio', {
      method: 'POST',
      body,
    }) as unknown as { seed_track_id: number; tracks: RichTrackRow[] }
    builtTracks.value = res.tracks ?? []
    if (!builtTracks.value.length) {
      buildError.value = 'No tracks came back — try different seeds or grow your library.'
    }
  } catch (e) {
    const err = e as { data?: { error?: string }; statusCode?: number; message?: string }
    if (err.statusCode === 503) {
      buildError.value = 'The CLAP audio model is still loading. Try again in a few seconds.'
    } else if (err.statusCode === 404) {
      buildError.value = err.data?.error ?? "Heya hasn't analyzed enough tracks yet for these seeds."
    } else {
      buildError.value = err.data?.error ?? err.message ?? 'Build failed.'
    }
    builtTracks.value = []
  } finally {
    building.value = false
  }
}

function seedToPayload(s: Seed) {
  if (s.kind === 'text') return { kind: 'text', text: s.text }
  if (s.kind === 'track') return { kind: 'track', track_id: s.id }
  if (s.kind === 'artist') return { kind: 'artist', artist_id: s.id }
  return { kind: 'album', album_id: s.id }
}

function trackRowToTrack(t: RichTrackRow): Track {
  return {
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.track_id}/stream`,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
    source: 'mix-builder',
  }
}

async function playAll() {
  if (!builtTracks.value.length) return
  const built = builtTracks.value.map(trackRowToTrack)
  queue.value = built
  await play(built[0]!)
}

async function playFrom(i: number) {
  const built = builtTracks.value.map(trackRowToTrack)
  queue.value = built
  await play(built[i]!)
}

async function onSaveAsPlaylist() {
  if (!builtTracks.value.length) return
  const summary = seeds.value.map((s) => s.label).slice(0, 3).join(' + ')
  const name = prompt('Playlist name', `Mix — ${summary || 'untitled'}`)
  if (!name) return
  try {
    const desc = `Built from: ${seeds.value.map((s) => `${s.kind}:${s.label}`).join(', ')}`
    const created = await playlistsApi.create(name, desc)
    for (const t of builtTracks.value) {
      await playlistsApi.addTrack(created.id, t.track_id)
    }
    navigateTo(`/music/playlist/${created.id}`)
  } catch {
    buildError.value = 'Could not save playlist.'
  }
}

function formatTotalDuration(rows: RichTrackRow[]): string {
  const total = rows.reduce((acc, r) => acc + (r.duration || 0), 0)
  const m = Math.round(total / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return `${h}h ${rm}m`
}
</script>

<style scoped>
.ms-mb { max-width: 900px; }

.ms-mb-head { margin-bottom: 24px; }
.ms-mb-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.ms-mb-sub { color: var(--fg-3); font-size: 14px; margin-top: 4px; max-width: 640px; }

/* Tabs */
.ms-mb-tabs {
  display: flex; gap: 4px;
  padding: 4px;
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 12px;
  width: fit-content;
}
.ms-mb-tab {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 8px 16px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-mb-tab:hover { color: var(--fg-0); }
.ms-mb-tab.active {
  background: var(--gold-soft);
  color: var(--gold);
}

.ms-mb-help {
  font-size: 12px;
  color: var(--fg-3);
  margin-bottom: 16px;
}

/* Seed add input */
.ms-mb-seed { margin-bottom: 16px; }
.ms-mb-input-row {
  position: relative;
  display: flex; align-items: center;
  gap: 8px;
}
.ms-mb-input-icon {
  position: absolute; left: 14px;
  color: var(--fg-3);
  pointer-events: none;
}
.ms-mb-input {
  flex: 1;
  padding: 12px 14px 12px 40px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--fg-0);
  font-size: 14px;
  outline: none;
  transition: border-color 0.15s, background 0.15s;
}
.ms-mb-input::placeholder { color: var(--fg-3); }
.ms-mb-input:focus { border-color: var(--gold); background: rgba(255,255,255,0.06); }
.ms-mb-add-btn {
  padding: 10px 20px;
  background: var(--gold-soft);
  color: var(--gold);
  border: 0;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-mb-add-btn:hover:not(:disabled) { background: var(--gold); color: var(--bg-0); }
.ms-mb-add-btn:disabled { opacity: 0.4; cursor: default; }

/* Autocomplete */
.ms-mb-ac {
  margin-top: 8px;
  padding: 4px;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  display: flex; flex-direction: column; gap: 2px;
}
.ms-mb-ac-row {
  display: grid;
  grid-template-columns: 40px 1fr auto;
  gap: 10px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-mb-ac-row:hover { background: rgba(255,255,255,0.04); }
.ms-mb-ac-row:hover .ms-mb-ac-add { color: var(--gold); }
.ms-mb-ac-add { color: var(--fg-3); transition: color 0.15s; }
.ac-art {
  width: 40px; height: 40px;
  border-radius: 4px;
  object-fit: cover;
  background: var(--bg-3);
}
.ac-art-round { border-radius: 50%; }
.ac-art-empty {
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
}
.ms-mb-ac-meta { min-width: 0; }
.ms-mb-ac-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-mb-ac-sub {
  font-size: 11px;
  color: var(--fg-3);
  margin-top: 1px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

/* Chips */
.ms-mb-chips {
  display: flex; align-items: flex-start; gap: 12px;
  margin-bottom: 20px;
  padding: 12px 14px;
  background: rgba(255,255,255,0.03);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
.ms-mb-chips-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding-top: 6px;
  flex-shrink: 0;
}
.ms-mb-chips-row {
  flex: 1;
  display: flex; flex-wrap: wrap; gap: 6px;
}
.ms-mb-chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 10px;
  background: rgba(255,255,255,0.06);
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
  max-width: 280px;
}
.ms-mb-chip span {
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-mb-chip:hover { background: rgba(255, 118, 118, 0.12); border-color: #ff7676; color: #ff7676; }
.ms-mb-chip .chip-x { opacity: 0.5; transition: opacity 0.15s; }
.ms-mb-chip:hover .chip-x { opacity: 1; }
.chip-text :deep(svg) { color: var(--gold); }
.chip-track :deep(svg) { color: #8a9bff; }
.chip-artist :deep(svg) { color: #ff9bcb; }
.chip-album :deep(svg) { color: #8ad6c2; }
.ms-mb-chip-clear {
  margin-left: auto;
  padding: 5px 12px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 11px;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: color 0.15s;
}
.ms-mb-chip-clear:hover { color: var(--fg-1); }

/* Controls */
.ms-mb-controls {
  display: flex; align-items: center; gap: 24px;
  padding-top: 8px;
  margin-bottom: 24px;
}
.ms-mb-control { display: flex; align-items: center; gap: 12px; flex: 1; }
.ms-mb-label {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.ms-mb-range {
  flex: 1;
  accent-color: var(--gold);
}
.ms-mb-count {
  min-width: 28px;
  text-align: right;
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 700;
  color: var(--gold);
}
.ms-mb-build-btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 22px;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.02em;
  cursor: pointer;
  transition: filter 0.15s;
}
.ms-mb-build-btn:hover:not(:disabled) { filter: brightness(1.1); }
.ms-mb-build-btn:disabled { opacity: 0.4; cursor: default; }

.ms-mb-error {
  color: #ff7676;
  font-size: 13px;
  padding: 12px 14px;
  border-radius: var(--r-sm);
  background: rgba(255, 118, 118, 0.06);
  border: 1px solid rgba(255, 118, 118, 0.2);
  margin-bottom: 16px;
}

/* Results */
.ms-mb-results { margin-top: 8px; }
.ms-mb-results-head {
  display: flex; align-items: flex-end; justify-content: space-between;
  margin-bottom: 14px;
}
.ms-mb-results-sub {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 4px;
  letter-spacing: 0.04em;
}
.ms-mb-results-actions { display: flex; gap: 6px; }
.ms-mb-action-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 12px;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-mb-action-btn:hover { background: rgba(255,255,255,0.09); border-color: var(--fg-3); }

.ms-mb-track-list { display: flex; flex-direction: column; gap: 2px; }
.ms-mb-track-row {
  display: grid;
  grid-template-columns: 28px 44px 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-mb-track-row:hover { background: rgba(255,255,255,0.04); }
.ms-mb-track-idx {
  text-align: right;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.ms-mb-track-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.ms-mb-track-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-mb-track-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-mb-track-row:hover .ms-mb-track-play { opacity: 1; }
.ms-mb-track-meta { min-width: 0; }
.ms-mb-track-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-mb-track-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-mb-track-dur {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}

.ms-mb-empty {
  text-align: center;
  padding: 60px 20px;
  color: var(--fg-3);
}
.ms-mb-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-mb-empty p { font-size: 13px; }
.ms-mb-empty strong { color: var(--fg-1); }

/* ── Phone (<=720px) ──────────────────────────────────────────────────
   This page is already a single vertical flow (tabs → seed input → chips
   → controls → results) — there's no side-by-side two-pane layout to
   restack, just the usual overflow/tap-target fixes. Functional over
   pretty per docs/responsive-plan.md W2c. */
@media (max-width: 720px) {
  /* music.vue's phone header already reads "Mix Builder" — the
     description line right below stays, it's not duplicated elsewhere. */
  .ms-mb-title { display: none; }
  .ms-mb-head { margin-bottom: 18px; }

  /* 4 pill tabs at `width: fit-content` overflow a 390px viewport — scroll
     the strip horizontally instead of blowing out the page width. */
  .ms-mb-tabs { width: 100%; max-width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; scrollbar-width: none; }
  .ms-mb-tabs::-webkit-scrollbar { display: none; }
  .ms-mb-tab { flex-shrink: 0; }

  .ms-mb-controls { flex-direction: column; align-items: stretch; gap: 14px; }
  .ms-mb-control { width: 100%; }
  .ms-mb-build-btn { justify-content: center; width: 100%; height: 44px; }

  .ms-mb-results-head { flex-direction: column; align-items: stretch; gap: 10px; }
  .ms-mb-results-actions { flex-wrap: wrap; }

  .ms-mb-track-row { padding: 10px 8px; }
}
</style>
