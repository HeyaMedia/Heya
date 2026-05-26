<template>
  <div class="ms-search page-pad">
    <div class="ms-search-head">
      <div class="ms-input-wrap">
        <Icon name="search" :size="18" class="ms-input-icon" />
        <input
          ref="inputEl"
          v-model="q"
          type="search"
          class="ms-input"
          placeholder="Search artists, albums, songs — or describe a vibe and press Enter"
          autocomplete="off"
          spellcheck="false"
          @keydown.esc="onEsc"
          @keydown.enter.prevent="onEnter"
          @keydown.down.prevent="moveActive(1)"
          @keydown.up.prevent="moveActive(-1)"
        />
        <button v-if="q" type="button" class="ms-input-clear" @click="clearInput" aria-label="Clear">
          <Icon name="close" :size="14" />
        </button>
      </div>
      <button
        type="button"
        class="ms-vibe-btn"
        :disabled="!q.trim() || vibeQuery.isFetching.value"
        :title="vibeQuery.isFetching.value ? 'Searching…' : 'Find tracks that match this vibe (CLAP)'"
        @click="runVibe"
      >
        <Icon name="sparkle" :size="14" />
        {{ vibeQuery.isFetching.value ? 'Vibing…' : 'Vibe Search' }}
      </button>
    </div>

    <!-- Empty state: recent searches + quick affordances -->
    <div v-if="!hasQuery && recentEntries.length" class="ms-recent">
      <div class="ms-recent-head">
        <div class="ms-section-label">Recent searches</div>
        <button type="button" class="ms-recent-clear" @click="recent.clear()">Clear all</button>
      </div>
      <ul class="ms-recent-list">
        <li v-for="r in recentEntries" :key="r">
          <button type="button" class="ms-recent-row" @click="useRecent(r)">
            <Icon name="clock" :size="14" />
            <span>{{ r }}</span>
            <span class="ms-recent-spacer" />
            <span class="ms-recent-remove" :title="`Remove '${r}'`" @click.stop="recent.remove(r)">
              <Icon name="close" :size="12" />
            </span>
          </button>
        </li>
      </ul>
    </div>

    <div v-else-if="!hasQuery" class="ms-empty-hint">
      <Icon name="search" :size="40" />
      <h3>Find anything in your music</h3>
      <p>Type a name to search artists, albums, and songs.<br/>Or describe a feeling — "<em>moody jazz at 2am</em>" — and press <kbd>Enter</kbd> for a Vibe Search.</p>
    </div>

    <!-- Text search results -->
    <div v-if="hasQuery" class="ms-results">
      <div v-if="quickQuery.isFetching.value && !hasAnyResults" class="ms-loading">Searching…</div>

      <!-- Artists -->
      <section v-if="artists.length" class="ms-section">
        <div class="ms-section-head">
          <h2 class="section-title-lg">Artists</h2>
          <div class="ms-section-count">{{ artistsTotal }} {{ artistsTotal === 1 ? 'match' : 'matches' }}</div>
        </div>
        <div class="ms-grid ms-grid-artists">
          <AppContextMenu
            v-for="(a, i) in artists"
            :key="`artist-${a.id}`"
            :items="actions.forArtist({ id: a.id, name: a.title, slug: a.slug })"
          >
          <NuxtLink
            :to="`/music/artist/${a.slug}`"
            class="ms-card-link"
            :class="{ 'kb-active': isActive('artist', i) }"
            :data-kb-idx="flatIdx('artist', i)"
          >
            <MusicCard
              variant="circle"
              :src="usePosterUrl(a.id) ?? undefined"
              :alt="a.title"
              :title="a.title"
              no-play
            />
            <div class="ms-circle-label">{{ a.title }}</div>
          </NuxtLink>
          </AppContextMenu>
        </div>
      </section>

      <!-- Albums -->
      <section v-if="albums.length" class="ms-section">
        <div class="ms-section-head">
          <h2 class="section-title-lg">Albums</h2>
          <div class="ms-section-count">{{ albumsTotal }} {{ albumsTotal === 1 ? 'match' : 'matches' }}</div>
        </div>
        <div class="ms-grid ms-grid-tiles">
          <AppContextMenu
            v-for="(al, i) in albums"
            :key="`album-${al.id}`"
            :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_id: al.artist_media_item_id, artist_name: al.artist_name })"
          >
          <NuxtLink
            :to="`/music/artist/${al.artist_slug}/${al.slug}`"
            class="ms-card-link"
            :class="{ 'kb-active': isActive('album', i) }"
            :data-kb-idx="flatIdx('album', i)"
          >
            <MusicCard
              :src="useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined"
              :alt="al.title"
              :title="al.title"
              :subtitle="`${al.artist_name}${al.year ? ' · ' + al.year : ''}`"
              :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
              @play="playAlbum(al)"
            />
          </NuxtLink>
          </AppContextMenu>
        </div>
      </section>

      <!-- Tracks -->
      <section v-if="tracks.length" class="ms-section">
        <div class="ms-section-head">
          <h2 class="section-title-lg">Songs</h2>
          <div class="ms-section-count">{{ tracksTotal }} {{ tracksTotal === 1 ? 'match' : 'matches' }}</div>
        </div>
        <ul class="ms-track-list">
          <AppContextMenu
            v-for="(t, i) in tracks"
            :key="`track-${t.id}`"
            :items="actions.forTrack({ id: t.id, title: t.title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_media_item_id, artist_slug: t.artist_slug, album_slug: t.album_slug })"
          >
          <li
            class="ms-track-row"
            :class="{ 'kb-active': isActive('track', i) }"
            :data-kb-idx="flatIdx('track', i)"
            @click="playTrack(i)"
          >
            <div class="ms-track-art">
              <img :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" loading="lazy" />
              <div class="ms-track-play"><Icon name="play" :size="14" /></div>
            </div>
            <div class="ms-track-meta">
              <div class="ms-track-title">{{ t.title }}</div>
              <div class="ms-track-sub">{{ t.artist_name }} · {{ t.album_title }}</div>
            </div>
            <div class="ms-track-dur">{{ formatDuration(t.duration) }}</div>
          </li>
          </AppContextMenu>
        </ul>
      </section>

      <!-- Vibe Search results — distinct section, gold-tinted -->
      <section v-if="vibeResults.length" class="ms-section ms-vibe-section">
        <div class="ms-section-head">
          <h2 class="section-title-lg">
            <Icon name="sparkle" :size="18" class="ms-vibe-icon" />
            Vibe matches
          </h2>
          <div class="ms-section-count">{{ vibeResults.length }} tracks</div>
        </div>
        <ul class="ms-track-list">
          <li v-for="(r, i) in vibeResults" :key="`vibe-${r.track_id}`" class="ms-track-row" @click="playVibe(i)">
            <div class="ms-track-art">
              <img :src="useAlbumCoverUrl(r.artist_slug, r.album_slug) ?? ''" :alt="r.album_title" loading="lazy" />
              <div class="ms-track-play"><Icon name="play" :size="14" /></div>
            </div>
            <div class="ms-track-meta">
              <div class="ms-track-title">{{ r.track_title }}</div>
              <div class="ms-track-sub">{{ r.artist_name }} · {{ r.album_title }}</div>
            </div>
            <div class="ms-track-match">{{ Math.round((1 - r.distance) * 100) }}%</div>
          </li>
        </ul>
      </section>

      <div v-if="vibeError" class="ms-error">{{ vibeError }}</div>

      <!-- Nothing found, no search in flight -->
      <div
        v-if="hasQuery && !quickQuery.isFetching.value && !hasAnyResults && !vibeResults.length"
        class="ms-no-results"
      >
        <h3>Nothing found for "{{ submitted }}"</h3>
        <p>Try a different spelling, or press <button type="button" class="ms-inline-btn" @click="runVibe">Vibe Search</button> to find tracks that match this as a feeling.</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'
import { refDebounced } from '@vueuse/core'

definePageMeta({ layout: 'default' })

const route = useRoute()
const router = useRouter()
const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()
const recent = useRecentSearches('music')
const recentEntries = recent.entries
const actions = useMusicActions()

// Source of truth: URL query param. Editing the input updates it after a
// debounce so reloads / bookmarks / shareable URLs all work consistently.
const q = ref((route.query.q as string | undefined) ?? '')
const inputEl = ref<HTMLInputElement>()

onMounted(() => {
  // Autofocus only when no query is already in the URL — if the user
  // arrived with ?q= we want the results to be readable without the input
  // stealing focus and triggering on-screen keyboards on mobile.
  if (!q.value) inputEl.value?.focus()
})

// Debounce the text input (220ms) so we don't fire a query on every keystroke.
const qDebounced = refDebounced(q, 220)
const submitted = computed(() => qDebounced.value.trim())
const hasQuery = computed(() => submitted.value.length >= 2)

// Keep the URL in sync once the debounce settles. Using replace so the back
// button doesn't have to walk through every intermediate keystroke.
watch(submitted, (s) => {
  const current = (route.query.q as string | undefined) ?? ''
  if (s === current) return
  router.replace({ query: s ? { q: s } : {} })
})

// --- Text search (live, debounced) ---
interface QuickBucket<T> { items: T[]; total: number }
interface QuickResult {
  query: string
  buckets: {
    music?: QuickBucket<{ id: number; title: string; slug: string }>
    albums?: QuickBucket<AlbumRow>
    tracks?: QuickBucket<TrackRow>
  }
}
interface AlbumRow {
  id: number
  title: string
  slug: string
  year: string
  album_type: string
  cover_path: string
  artist_media_item_id: number
  artist_name: string
  artist_slug: string
}
interface TrackRow {
  id: number
  title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  artist_media_item_id: number
  artist_name: string
  artist_slug: string
}

const quickQuery = useQuery({
  queryKey: ['search', 'quick', 'music', submitted],
  queryFn: async () => {
    const res = await $heya('/api/search/quick', {
      query: { q: submitted.value },
    }) as unknown as QuickResult
    return res
  },
  enabled: () => hasQuery.value,
  staleTime: 1000 * 30,
})

const artists = computed(() => quickQuery.data.value?.buckets?.music?.items ?? [])
const artistsTotal = computed(() => quickQuery.data.value?.buckets?.music?.total ?? 0)
const albums = computed(() => quickQuery.data.value?.buckets?.albums?.items ?? [])
const albumsTotal = computed(() => quickQuery.data.value?.buckets?.albums?.total ?? 0)
const tracks = computed(() => quickQuery.data.value?.buckets?.tracks?.items ?? [])
const tracksTotal = computed(() => quickQuery.data.value?.buckets?.tracks?.total ?? 0)
const hasAnyResults = computed(() => artists.value.length + albums.value.length + tracks.value.length > 0)

// Record into recent-searches once a query has produced results — avoids
// littering history with typos that never resolved into anything.
watch([hasAnyResults, submitted], ([any, s]) => {
  if (any && s) recent.record(s)
})

// --- Vibe search (button-triggered, NOT live) ---
interface VibeRow {
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
  distance: number
}

const vibeTrigger = ref('')
const vibeQuery = useQuery({
  queryKey: ['search', 'vibe', vibeTrigger],
  queryFn: async () => {
    const res = await $heya('/api/music/search-sonic', {
      query: { q: vibeTrigger.value, limit: 24 },
    }) as unknown as { items: VibeRow[] }
    return res.items ?? []
  },
  enabled: () => vibeTrigger.value.length > 0,
  staleTime: 1000 * 60,
})
const vibeResults = computed<VibeRow[]>(() => vibeQuery.data.value ?? [])
const vibeError = computed(() => {
  const e = vibeQuery.error.value as { data?: { error?: string }; statusCode?: number } | null
  if (!e) return null
  if (e.statusCode === 503) return 'Vibe Search is warming up — the CLAP text model is still loading. Try again in a few seconds.'
  return e?.data?.error ?? null
})

function runVibe() {
  const trimmed = q.value.trim()
  if (!trimmed) return
  vibeTrigger.value = trimmed
  recent.record(trimmed)
}

// --- Keyboard navigation ---
// Flattened ordering: Artists → Albums → Tracks. activeIdx is the position
// in the flat list, or null when nothing is highlighted yet.
const activeIdx = ref<number | null>(null)

const flatTotal = computed(() => artists.value.length + albums.value.length + tracks.value.length)

// Reset highlight whenever the result set changes substantially.
watch([submitted, artists, albums, tracks], () => {
  activeIdx.value = null
})

function flatIdx(kind: 'artist' | 'album' | 'track', i: number): number {
  if (kind === 'artist') return i
  if (kind === 'album') return artists.value.length + i
  return artists.value.length + albums.value.length + i
}

function isActive(kind: 'artist' | 'album' | 'track', i: number): boolean {
  return activeIdx.value === flatIdx(kind, i)
}

function moveActive(delta: number) {
  if (flatTotal.value === 0) return
  const cur = activeIdx.value
  let next: number
  if (cur === null) {
    next = delta > 0 ? 0 : flatTotal.value - 1
  } else {
    next = cur + delta
    if (next < 0) next = flatTotal.value - 1
    if (next >= flatTotal.value) next = 0
  }
  activeIdx.value = next
  // Scroll the highlighted row into view. The data-kb-idx attribute is
  // stamped on every result element so we can target it without refs.
  nextTick(() => {
    const el = document.querySelector(`[data-kb-idx="${next}"]`) as HTMLElement | null
    el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  })
}

function onEnter() {
  // If a row is highlighted, navigate/play it instead of running the vibe search.
  if (activeIdx.value === null) {
    runVibe()
    return
  }
  const idx = activeIdx.value
  if (idx < artists.value.length) {
    navigateTo(`/music/artist/${artists.value[idx]!.slug}`)
    return
  }
  const albumIdx = idx - artists.value.length
  if (albumIdx < albums.value.length) {
    const al = albums.value[albumIdx]!
    navigateTo(`/music/artist/${al.artist_slug}/${al.slug}`)
    return
  }
  const trackIdx = albumIdx - albums.value.length
  if (trackIdx >= 0 && trackIdx < tracks.value.length) {
    playTrack(trackIdx)
  }
}

function useRecent(r: string) {
  q.value = r
  inputEl.value?.focus()
}

function clearInput() {
  q.value = ''
  vibeTrigger.value = ''
  inputEl.value?.focus()
}

function onEsc() {
  if (q.value) clearInput()
  else inputEl.value?.blur()
}

function formatDuration(sec: number): string {
  if (!sec || sec < 0) return ''
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
}

// --- Play actions ---
async function playAlbum(al: AlbumRow) {
  try {
    const res = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: al.artist_slug, album_slug: al.slug },
    }) as unknown as { tracks?: Array<{ id: number; title: string; artist_name: string; duration: number; disc_number: number; track_number: number }> }
    const list = res.tracks ?? []
    if (!list.length) return
    const built: Track[] = list.map((t) => ({
      id: t.id,
      title: t.title,
      artist: t.artist_name || al.artist_name,
      album: al.title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.id}/stream`,
      album_id: al.id,
      artist_id: al.artist_media_item_id,
      artist_slug: al.artist_slug,
      album_slug: al.slug,
      poster: useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined,
      source: 'album',
    }))
    queue.value = built
    await play(built[0]!)
  } catch {
    // outer link still navigates to the album page
  }
}

async function playTrack(startIdx: number) {
  const built: Track[] = tracks.value.map((t) => ({
    id: t.id,
    title: t.title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.id}/stream`,
    album_id: t.album_id,
    artist_id: t.artist_media_item_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
    source: 'search',
  }))
  queue.value = built
  await play(built[startIdx]!)
}

async function playVibe(startIdx: number) {
  const built: Track[] = vibeResults.value.map((r) => ({
    id: r.track_id,
    title: r.track_title,
    artist: r.artist_name,
    album: r.album_title,
    duration: r.duration,
    stream_url: `/api/music/tracks/${r.track_id}/stream`,
    album_id: r.album_id,
    artist_id: r.artist_id,
    artist_slug: r.artist_slug,
    album_slug: r.album_slug,
    poster: useAlbumCoverUrl(r.artist_slug, r.album_slug) ?? undefined,
    source: 'vibe',
  }))
  queue.value = built
  await play(built[startIdx]!)
}
</script>

<style scoped>
.ms-search { max-width: 1200px; }

/* ---- Search bar ---- */
.ms-search-head {
  display: flex; align-items: stretch; gap: 10px;
  margin-bottom: 28px;
}
.ms-input-wrap {
  position: relative;
  flex: 1;
  display: flex; align-items: center;
}
.ms-input-icon {
  position: absolute; left: 14px;
  color: var(--fg-3);
  pointer-events: none;
}
.ms-input {
  width: 100%;
  padding: 14px 44px 14px 42px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--fg-0);
  font-size: 15px;
  outline: none;
  transition: border-color 0.15s, background 0.15s;
}
.ms-input::placeholder { color: var(--fg-3); }
.ms-input:focus { border-color: var(--gold); background: rgba(255,255,255,0.06); }
.ms-input-clear {
  position: absolute; right: 8px;
  width: 28px; height: 28px;
  display: flex; align-items: center; justify-content: center;
  border: 0; background: transparent;
  color: var(--fg-3);
  border-radius: 50%;
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.ms-input-clear:hover { color: var(--fg-0); background: rgba(255,255,255,0.06); }

.ms-vibe-btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 0 18px;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  letter-spacing: 0.02em;
  transition: filter 0.15s;
}
.ms-vibe-btn:hover:not(:disabled) { filter: brightness(1.1); }
.ms-vibe-btn:disabled { opacity: 0.4; cursor: default; }

/* ---- Recent searches ---- */
.ms-recent { margin-top: 8px; max-width: 640px; }
.ms-recent-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 10px; }
.ms-section-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.ms-recent-clear {
  background: transparent; border: 0;
  font-size: 11px; color: var(--fg-3);
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: color 0.15s;
}
.ms-recent-clear:hover { color: var(--fg-1); }
.ms-recent-list {
  display: flex; flex-direction: column; gap: 2px;
}
.ms-recent-row {
  display: flex; align-items: center; gap: 12px;
  width: 100%;
  padding: 10px 12px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s, color 0.15s;
}
.ms-recent-row:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }
.ms-recent-row :deep(svg) { color: var(--fg-3); flex-shrink: 0; }
.ms-recent-spacer { flex: 1; }
.ms-recent-remove {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px;
  border-radius: 50%;
  color: var(--fg-3);
  transition: background 0.15s, color 0.15s;
}
.ms-recent-row:hover .ms-recent-remove:hover { background: rgba(255,255,255,0.08); color: var(--fg-0); }

/* ---- Empty hint ---- */
.ms-empty-hint {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-empty-hint :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-empty-hint h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-empty-hint p { font-size: 13px; line-height: 1.6; max-width: 440px; margin: 0 auto; }
.ms-empty-hint em { color: var(--fg-1); font-style: normal; }
.ms-empty-hint kbd {
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 1px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: rgba(255,255,255,0.04);
  color: var(--fg-1);
}

/* ---- Results ---- */
.ms-loading { color: var(--fg-3); padding: 20px 0; font-size: 13px; }
.ms-section { margin-bottom: 36px; }
.ms-section-head {
  display: flex; align-items: baseline; justify-content: space-between;
  margin-bottom: 14px;
}
.ms-section-count {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  letter-spacing: 0.04em;
}

.ms-card-link { text-decoration: none; color: inherit; display: block; }
.kb-active { outline: 2px solid var(--gold); outline-offset: 2px; border-radius: var(--r-sm); }

.ms-grid { display: grid; gap: 16px; }
.ms-grid-tiles { grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); }
.ms-grid-artists { grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); }
.ms-circle-label {
  text-align: center;
  margin-top: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Track list */
.ms-track-list {
  display: flex; flex-direction: column; gap: 2px;
}
.ms-track-row {
  display: grid;
  grid-template-columns: 44px 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-track-row:hover { background: rgba(255,255,255,0.04); }
.ms-track-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.ms-track-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-track-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-track-row:hover .ms-track-play { opacity: 1; }
.ms-track-meta { min-width: 0; }
.ms-track-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-dur, .ms-track-match {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}
.ms-track-match { color: var(--gold); font-weight: 700; }

/* Vibe section visual delineation */
.ms-vibe-section {
  padding-top: 28px;
  border-top: 1px solid var(--border);
}
.ms-vibe-icon { color: var(--gold); margin-right: 4px; vertical-align: -2px; }

.ms-no-results {
  text-align: center;
  padding: 40px 20px;
  color: var(--fg-3);
}
.ms-no-results h3 { font-size: 16px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-inline-btn {
  background: transparent; border: 0; padding: 0;
  color: var(--gold); cursor: pointer;
  font-size: inherit; font-weight: 600;
  text-decoration: underline; text-underline-offset: 3px;
}
.ms-inline-btn:hover { filter: brightness(1.2); }

.ms-error {
  color: #ff7676;
  font-size: 13px;
  padding: 12px 14px;
  border-radius: var(--r-sm);
  background: rgba(255, 118, 118, 0.06);
  border: 1px solid rgba(255, 118, 118, 0.2);
  margin-top: 12px;
}
</style>
