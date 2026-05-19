<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero backdrop (cycles through all backdrops) -->
    <div class="detail-hero">
      <img
        v-if="currentBackdrop"
        :src="currentBackdrop"
        :key="backdropIdx"
        style="width: 100%; height: 100%; object-fit: cover; transition: opacity 0.8s ease"
        @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
      />
      <div class="detail-hero-fade" />
      <button class="back-btn" @click="$router.back()">
        <Icon name="back" :size="16" />
        Back
      </button>
    </div>

    <div class="detail-body">
      <div class="detail-grid">
        <div class="detail-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
        </div>

        <div class="detail-info">
          <div class="detail-badges">
            <Chip gold>{{ mediaTypeLabel(detail.media_item.media_type) }}</Chip>
            <Chip v-if="detail.media_item.year">{{ detail.media_item.year }}</Chip>
            <Chip v-if="detail.movie?.runtime_minutes">{{ detail.movie.runtime_minutes }} min</Chip>
            <Chip v-if="detail.tv_series?.status">{{ detail.tv_series.status }}</Chip>
          </div>

          <h1 class="detail-title">{{ detail.media_item.title }}</h1>
          <p v-if="detail.movie?.tagline" class="detail-tagline">{{ detail.movie.tagline }}</p>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 16px">
            <Chip v-for="g in genres" :key="g">{{ g }}</Chip>
          </div>

          <div class="hero-meta-row" v-if="rating">
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span>{{ rating }}/10</span>
            <template v-if="detail.movie?.runtime_minutes">
              <span class="dot" />
              <span>{{ Math.floor(detail.movie.runtime_minutes / 60) }}h {{ detail.movie.runtime_minutes % 60 }}m</span>
            </template>
          </div>

          <div class="detail-actions">
            <button class="btn btn-primary"><Icon name="play" :size="16" /> Play</button>
            <button class="btn btn-secondary"><Icon name="plus" :size="16" /> My List</button>
            <button class="btn-icon" style="color: var(--fg-1)"><Icon name="heart" :size="20" /></button>
            <button class="btn-icon" style="color: var(--fg-1)"><Icon name="download" :size="20" /></button>
          </div>

          <p v-if="detail.media_item.description" class="detail-synopsis">{{ detail.media_item.description }}</p>

          <!-- Credits -->
          <div v-if="crew.length" class="detail-credits">
            <div v-for="c in crew" :key="c.label" class="credit-row">
              <div class="credit-label">{{ c.label }}</div>
              <div class="credit-val">{{ c.value }}</div>
            </div>
          </div>
        </div>
      </div>

      <!-- Cast -->
      <div v-if="cast.length" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Cast</h3>
        <div class="cast-grid">
          <div v-for="c in cast.slice(0, 12)" :key="c.name" class="cast-card">
            <div class="cast-avatar">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
            <div class="cast-name">{{ c.name }}</div>
            <div class="cast-role">{{ c.character }}</div>
          </div>
        </div>
      </div>

      <!-- Extras -->
      <div v-if="groupedExtras.length" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Extras</h3>
        <div v-for="group in groupedExtras" :key="group.type" style="margin-bottom: 24px">
          <div class="section-title" style="font-size: 11px; margin-bottom: 10px">{{ formatExtraType(group.type) }}</div>
          <div class="extras-grid">
            <div v-for="e in group.items" :key="e.id" class="extra-card">
              <div class="extra-thumb"><Icon name="play" :size="20" /></div>
              <div class="extra-info">
                <div class="extra-title">{{ e.title }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Media Info -->
      <div v-if="detail.movie" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Media Info</h3>
        <div class="media-info">
          <div v-if="detail.movie.original_title" class="mi-row">
            <div>Original Title</div>
            <div>{{ detail.movie.original_title }}</div>
          </div>
          <div v-if="detail.movie.original_language" class="mi-row">
            <div>Language</div>
            <div>{{ detail.movie.original_language.toUpperCase() }}</div>
          </div>
          <div v-if="detail.movie.budget" class="mi-row">
            <div>Budget</div>
            <div>${{ (detail.movie.budget / 1_000_000).toFixed(0) }}M</div>
          </div>
          <div v-if="detail.movie.revenue" class="mi-row">
            <div>Revenue</div>
            <div>${{ (detail.movie.revenue / 1_000_000).toFixed(0) }}M</div>
          </div>
        </div>
      </div>

      <!-- Seasons -->
      <div v-if="detail.seasons?.length" class="detail-section">
        <h3 class="section-title" style="margin-bottom: 16px">Seasons</h3>
        <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 16px">
          <div v-for="s in detail.seasons" :key="s.id" class="card-tile">
            <Poster :idx="s.season_number" :src="usePosterUrl(undefined)" aspect="2/3" :title="s.name" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ s.name }}</div>
              <div class="grid-tile-sub">{{ s.episode_count }} episodes</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div v-else class="scroll" style="height: 100%; display: flex; align-items: center; justify-content: center">
    <div style="text-align: center; color: var(--fg-2)">
      <p style="font-size: 18px">Media not found</p>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail, MediaExtra } from '~~/shared/types'

const props = defineProps<{ mediaId: number }>()

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const backdropIdx = ref(0)

const backdropAssets = computed(() => {
  if (!detail.value?.assets) return []
  return detail.value.assets
    .filter(a => a.asset_type === 'backdrop')
    .sort((a, b) => a.sort_order - b.sort_order)
})

const currentBackdrop = computed(() => {
  if (backdropAssets.value.length > 0) {
    const asset = backdropAssets.value[backdropIdx.value % backdropAssets.value.length]
    return `/api/media/${detail.value?.media_item.id}/image/backdrop?sort=${asset.sort_order}`
  }
  return detail.value ? useBackdropUrl(detail.value.media_item.id) : null
})

const genres = computed(() => {
  if (!detail.value) return []
  return detail.value.movie?.genres || detail.value.tv_series?.genres || detail.value.book?.genres || []
})

const rating = computed(() => {
  const r = detail.value?.movie?.rating || detail.value?.tv_series?.rating || detail.value?.book?.rating
  return r ? parseFloat(String(r)).toFixed(1) : ''
})

const cast = computed(() => {
  if (!detail.value?.movie?.cast_data) return []
  try { return JSON.parse(detail.value.movie.cast_data as any) || [] } catch { return [] }
})

const crew = computed(() => {
  if (!detail.value?.movie?.crew_data) return []
  try {
    const data = JSON.parse(detail.value.movie.crew_data as any) || []
    const directors = data.filter((c: any) => c.job === 'Director').map((c: any) => c.name)
    const writers = data.filter((c: any) => c.job === 'Writer' || c.job === 'Screenplay').map((c: any) => c.name)
    const producers = data.filter((c: any) => c.job === 'Producer').map((c: any) => c.name)
    const result = []
    if (directors.length) result.push({ label: 'Director', value: directors.join(', ') })
    if (writers.length) result.push({ label: 'Writer', value: writers.join(', ') })
    if (producers.length) result.push({ label: 'Producer', value: producers.slice(0, 3).join(', ') })
    return result
  } catch { return [] }
})

const groupedExtras = computed(() => {
  if (!detail.value?.extras?.length) return []
  const groups: Record<string, MediaExtra[]> = {}
  for (const e of detail.value.extras) {
    if (!groups[e.extra_type]) groups[e.extra_type] = []
    groups[e.extra_type].push(e)
  }
  const order = ['trailer', 'behind_the_scenes', 'featurette', 'other', 'teaser', 'deleted_scene', 'interview']
  return order
    .filter(t => groups[t])
    .map(t => ({ type: t, items: groups[t] }))
})

function formatExtraType(t: string) {
  const labels: Record<string, string> = {
    trailer: 'Trailers',
    behind_the_scenes: 'Behind the Scenes',
    featurette: 'Featurettes',
    other: 'Other',
    teaser: 'Teasers',
    deleted_scene: 'Deleted Scenes',
    interview: 'Interviews',
  }
  return labels[t] || t
}

let backdropInterval: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${props.mediaId}`)
  } catch { /* empty */ }
  loading.value = false

  if (backdropAssets.value.length > 1) {
    backdropInterval = setInterval(() => {
      backdropIdx.value = (backdropIdx.value + 1) % backdropAssets.value.length
    }, 8000)
  }
})

onUnmounted(() => {
  if (backdropInterval) clearInterval(backdropInterval)
})
</script>

<style scoped>
.cast-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 20px;
}
.cast-card { text-align: center; }
.cast-avatar {
  width: 72px;
  height: 72px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--bg-4), var(--bg-3));
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 8px;
  font-size: 18px;
  font-weight: 600;
  color: var(--fg-2);
}
.cast-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.cast-role { font-size: 11px; color: var(--fg-2); margin-top: 2px; }

.extras-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 10px;
}
.extra-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: background 0.12s;
}
.extra-card:hover { background: var(--bg-3); }
.extra-thumb {
  width: 44px;
  height: 44px;
  border-radius: var(--r-sm);
  background: var(--bg-4);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-2);
  flex-shrink: 0;
}
.extra-title { font-size: 13px; font-weight: 500; color: var(--fg-0); }
</style>
