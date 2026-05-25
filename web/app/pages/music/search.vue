<template>
  <div class="sonic-search page-pad">
    <h1 class="ss-title">Audio Vibe Search</h1>
    <p class="ss-sub">Describe a sound. Find tracks that fit.</p>

    <form class="ss-form" @submit.prevent="runSearch">
      <input
        v-model="q"
        type="search"
        class="ss-input"
        placeholder="e.g. hard dark industrial techno"
        autocomplete="off"
        autofocus
      />
      <button type="submit" class="ss-btn" :disabled="!q.trim() || loading">
        {{ loading ? 'Searching…' : 'Search' }}
      </button>
    </form>

    <div v-if="error" class="ss-error">{{ error }}</div>

    <div v-if="results.length" class="ss-results">
      <div
        v-for="(row, i) in results"
        :key="row.id"
        class="ss-row card-tile"
        @click="playRow(row, i)"
      >
        <Poster :idx="row.id" :src="useAlbumCoverUrl(row.album_id)" aspect="1/1" class="ss-art" />
        <div class="ss-meta">
          <div class="ss-rtitle">{{ row.title }}</div>
          <div class="ss-rsub">match {{ ((1 - row.distance) * 100).toFixed(0) }}%</div>
        </div>
      </div>
    </div>

    <div v-else-if="searched && !loading" class="ss-empty">
      No matches yet. Try a different vibe.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'

definePageMeta({ layout: 'default' })

interface SonicTrackResult {
  id: number
  title: string
  album_id: number
  artist_id: number
  file_path: string
  distance: number
}

const route = useRoute()
const router = useRouter()
const q = ref((route.query.q as string | undefined) ?? '')
const loading = ref(false)
const error = ref<string | null>(null)
const results = ref<SonicTrackResult[]>([])
const searched = ref(false)
const { play, queue } = usePlayer()

async function runSearch() {
  const trimmed = q.value.trim()
  if (!trimmed) return
  router.replace({ query: { q: trimmed } })
  loading.value = true
  error.value = null
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/music/search-sonic', {
      query: { q: trimmed, limit: 24 },
    }) as { items: SonicTrackResult[] }
    results.value = res.items ?? []
  } catch (e: any) {
    error.value = e?.data?.error ?? 'Search failed (analyzer model may still be loading).'
    results.value = []
  } finally {
    loading.value = false
    searched.value = true
  }
}

async function playRow(row: SonicTrackResult, startIdx: number) {
  const tracks: Track[] = results.value.map((r) => ({
    id: r.id,
    title: r.title,
    artist: '',
    album: '',
    duration: 0,
    stream_url: `/api/tracks/${r.id}/stream`,
    album_id: r.album_id,
    artist_id: r.artist_id,
  }))
  queue.value = tracks
  await play(tracks[startIdx])
}

// Auto-run when arriving with ?q= in the URL.
onMounted(() => {
  if (q.value.trim()) runSearch()
})
</script>

<style scoped>
.sonic-search { max-width: 900px; }
.ss-title { font-size: 28px; font-weight: 700; margin-bottom: 4px; letter-spacing: -0.01em; }
.ss-sub { color: var(--fg-2); font-size: 14px; margin-bottom: 20px; }
.ss-form { display: flex; gap: 8px; margin-bottom: 24px; }
.ss-input {
  flex: 1;
  padding: 12px 14px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--fg-0);
  font-size: 14px;
  outline: none;
}
.ss-input:focus { border-color: var(--gold); }
.ss-btn {
  padding: 0 18px;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}
.ss-btn:disabled { opacity: 0.5; cursor: default; }
.ss-error { color: #ff7676; font-size: 13px; padding: 12px 0; }
.ss-empty { color: var(--fg-3); font-size: 13px; padding: 24px 0; }
.ss-results {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 8px;
}
.ss-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: 6px;
  background: rgba(255,255,255,0.03);
  cursor: pointer;
  transition: background 0.15s;
}
.ss-row:hover { background: rgba(255,255,255,0.07); }
.ss-art { width: 48px; height: 48px; border-radius: 4px; }
.ss-meta { flex: 1; min-width: 0; }
.ss-rtitle { font-size: 13px; font-weight: 500; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ss-rsub { font-size: 11px; color: var(--fg-2); margin-top: 2px; font-family: var(--font-mono); }
</style>
