<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero with crossfade backdrops -->
    <div class="hero-section">
      <div class="hero-bg">
        <img v-if="backdropA" :src="backdropA" class="hero-bg-img" :class="{ visible: showA }" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
        <img v-if="backdropB" :src="backdropB" class="hero-bg-img" :class="{ visible: !showA }" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <div class="hero-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
        </div>

        <div class="hero-info">
          <div class="detail-badges">
            <Chip gold>TV Show</Chip>
            <Chip v-if="certification">{{ certification }}</Chip>
            <Chip v-if="detail.media_item.year">{{ detail.media_item.year }}</Chip>
            <Chip v-if="detail.tv_series?.status">{{ detail.tv_series.status }}</Chip>
          </div>

          <h1 class="detail-title">{{ detail.media_item.title }}</h1>

          <div class="hero-meta-row" v-if="rating">
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span style="color: var(--gold)">{{ rating }}/10</span>
            <span class="dot" />
            <span>{{ detail.tv_series?.number_of_seasons }} season{{ detail.tv_series?.number_of_seasons !== 1 ? 's' : '' }}</span>
            <span class="dot" />
            <span>{{ detail.tv_series?.number_of_episodes }} episodes</span>
          </div>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin: 12px 0">
            <Chip v-for="g in genres" :key="g">{{ g }}</Chip>
          </div>

          <div class="detail-actions">
            <button class="btn btn-primary"><Icon name="play" :size="16" /> Play</button>
            <button class="btn btn-secondary"><Icon name="plus" :size="16" /> My List</button>
            <button class="btn-icon" style="color: var(--fg-1)"><Icon name="heart" :size="20" /></button>
          </div>

          <p v-if="detail.media_item.description" class="detail-synopsis">{{ detail.media_item.description }}</p>

          <div class="info-grid">
            <template v-if="detail.tv_series?.networks?.length">
              <div class="info-label">Network</div>
              <div class="info-value">{{ detail.tv_series.networks.join(', ') }}</div>
            </template>
            <template v-if="detail.tv_series?.created_by?.length">
              <div class="info-label">Created By</div>
              <div class="info-value">{{ detail.tv_series.created_by.join(', ') }}</div>
            </template>
            <template v-if="detail.production_companies?.length">
              <div class="info-label">Studio</div>
              <div class="info-value">{{ detail.production_companies.map((c: any) => c.name).join(', ') }}</div>
            </template>
            <template v-if="detail.tv_series?.first_air_date">
              <div class="info-label">First Aired</div>
              <div class="info-value">{{ formatDate(detail.tv_series.first_air_date) }}</div>
            </template>
          </div>

          <div v-if="detail.keywords?.length" style="display: flex; gap: 5px; flex-wrap: wrap; margin-top: 16px">
            <span v-for="k in detail.keywords" :key="k.id" class="keyword-tag">{{ k.name }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="detail-body-below">
      <!-- Seasons -->
      <div class="detail-section">
        <div class="section-row-head">
          <h3 class="section-title-lg">Seasons</h3>
        </div>
        <div class="seasons-grid">
          <NuxtLink
            v-for="s in displaySeasons"
            :key="s.season_number"
            :to="seasonUrl(s)"
            class="season-card card-tile"
          >
            <Poster :idx="s.season_number" :src="seasonPosterUrl(s)" :title="seasonLabel(s)" aspect="2/3" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ seasonLabel(s) }} <span class="ep-count">({{ s.episodes?.length || 0 }} eps)</span></div>
              <div class="grid-tile-sub" v-if="s.air_date">{{ formatYear(s.air_date) }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>

      <!-- Cast & Crew -->
      <div v-if="detail.cast?.length || detail.crew?.length" class="detail-section">
        <div class="section-row-head" style="margin-bottom: 0">
          <div class="tab-bar" style="margin-bottom: 0">
            <button class="tab-btn" :class="{ active: peopleTab === 'cast' }" @click="peopleTab = 'cast'">
              Cast <span class="tab-count">{{ detail.cast?.length || 0 }}</span>
            </button>
            <button class="tab-btn" :class="{ active: peopleTab === 'crew' }" @click="peopleTab = 'crew'">
              Crew <span class="tab-count">{{ detail.crew?.length || 0 }}</span>
            </button>
          </div>
        </div>

        <div v-if="peopleTab === 'cast'" class="hscroll" style="margin-top: 16px">
          <NuxtLink v-for="c in detail.cast" :key="c.id" :to="personUrl(c)" class="cast-card">
            <img v-if="c.profile_path && !c.profile_path.startsWith('http')" :src="`/api/person/${c.id}/image`" class="cast-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
            <div v-else class="cast-avatar">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
            <div class="cast-name">{{ c.name }}</div>
            <div class="cast-role">{{ c.character }}</div>
          </NuxtLink>
        </div>

        <div v-if="peopleTab === 'crew'" style="margin-top: 16px">
          <div v-for="dept in crewByDepartment" :key="dept.name" style="margin-bottom: 24px">
            <div class="section-title" style="font-size: 11px; margin-bottom: 10px">{{ dept.name }}</div>
            <div class="crew-dept-grid">
              <NuxtLink v-for="c in dept.members" :key="`${c.id}-${c.job}`" :to="personUrl(c)" class="crew-card">
                <img v-if="c.profile_path && !c.profile_path.startsWith('http')" :src="`/api/person/${c.id}/image`" class="crew-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                <div v-else class="crew-initials">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
                <div>
                  <div class="crew-name">{{ c.name }}</div>
                  <div class="crew-job">{{ c.job }}</div>
                </div>
              </NuxtLink>
            </div>
          </div>
        </div>
      </div>

      <!-- Videos -->
      <div v-if="detail.videos?.length" class="detail-section">
        <div class="section-row-head"><h3 class="section-title-lg">Videos</h3></div>
        <div class="hscroll">
          <a v-for="v in detail.videos" :key="v.id" :href="`https://www.youtube.com/watch?v=${v.video_key}`" target="_blank" class="video-card">
            <div class="video-thumb">
              <img :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
              <div class="video-play"><Icon name="play" :size="20" /></div>
            </div>
            <div class="video-name">{{ v.name }}</div>
            <div class="video-type">{{ v.video_type }}</div>
          </a>
        </div>
      </div>

      <!-- Recommendations -->
      <div v-if="detail.recommendations?.length" class="detail-section">
        <div class="section-row-head"><h3 class="section-title-lg">More Like This</h3></div>
        <div class="hscroll">
          <NuxtLink v-for="r in detail.recommendations" :key="r.id" :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, title: r.title, year: '', media_type: r.media_type }) : ''" class="rec-card" :class="{ dimmed: !r.local_media_item_id }">
            <Poster :idx="r.recommended_tmdb_id" :src="r.local_poster_path ? `/api/media/${r.local_media_item_id}/image/poster` : ''" aspect="2/3" :title="r.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ r.title }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>

      <!-- External ratings -->
      <div v-if="detail.external_ratings?.length" class="detail-section">
        <div class="section-row-head"><h3 class="section-title-lg">Ratings</h3></div>
        <div style="display: flex; gap: 12px; flex-wrap: wrap">
          <div v-for="r in detail.external_ratings" :key="r.source" class="rating-card">
            <div class="rating-source">{{ formatRatingSource(r.source) }}</div>
            <div class="rating-value">{{ r.value }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const peopleTab = ref<'cast' | 'crew'>('cast')

// Crossfade backdrops
const showA = ref(true)
const backdropA = ref<string | null>(null)
const backdropB = ref<string | null>(null)
const backdropIdx = ref(0)

const backdropAssets = computed(() => {
  if (!detail.value?.assets) return []
  const seen = new Set<number>()
  return detail.value.assets
    .filter(a => a.asset_type === 'backdrop' && a.sort_order < 1000)
    .sort((a, b) => a.sort_order - b.sort_order)
    .filter(a => { if (seen.has(a.sort_order)) return false; seen.add(a.sort_order); return true })
})

function getBackdropUrl(idx: number) {
  if (backdropAssets.value.length > 0) {
    const asset = backdropAssets.value[idx % backdropAssets.value.length]
    return `/api/media/${detail.value?.media_item.id}/image/backdrop?sort=${asset.sort_order}`
  }
  return detail.value ? useBackdropUrl(detail.value.media_item.id) : null
}

function advanceBackdrop() {
  if (backdropAssets.value.length <= 1) return
  backdropIdx.value = (backdropIdx.value + 1) % backdropAssets.value.length
  const url = getBackdropUrl(backdropIdx.value)
  if (showA.value) { backdropB.value = url } else { backdropA.value = url }
  showA.value = !showA.value
}

const displaySeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

const rating = computed(() => {
  const r = detail.value?.tv_series?.rating
  if (!r) return null
  const n = parseFloat(r)
  return isNaN(n) || n === 0 ? null : n.toFixed(1)
})

const certification = computed(() => {
  const certs = detail.value?.certifications
  if (!certs?.length) return null
  const us = certs.find((c: any) => c.country === 'US')
  return (us || certs[0])?.certification || null
})

const genres = computed(() => detail.value?.tv_series?.genres || [])

const crewByDepartment = computed(() => {
  const crew = detail.value?.crew || []
  const depts = new Map<string, any[]>()
  for (const c of crew) {
    const d = c.department || 'Other'
    if (!depts.has(d)) depts.set(d, [])
    depts.get(d)!.push(c)
  }
  return Array.from(depts.entries()).map(([name, members]) => ({ name, members }))
})

function seasonUrl(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function seasonPosterUrl(s: any) {
  const num = String(s.season_number).padStart(2, '0')
  return `/api/media/${detail.value?.media_item.id}/image/poster?label=season${num}-poster`
}

function seasonLabel(s: any) {
  if (s.season_number === 0) return 'Specials'
  return s.title || `Season ${s.season_number}`
}

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

function formatYear(d: string) { return d?.slice(0, 4) || '' }

function formatRatingSource(s: string) {
  const map: Record<string, string> = { imdb: 'IMDb', rotten_tomatoes: 'Rotten Tomatoes', metacritic: 'Metacritic' }
  return map[s] || s
}

let timer: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${slug.value}`)
    await nextTick()
    backdropA.value = getBackdropUrl(0)
    if (backdropAssets.value.length > 1) {
      timer = setInterval(advanceBackdrop, 8000)
    }
  } catch { navigateTo('/tv') }
  loading.value = false
})

onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
/* Hero — matches movie detail page */
.hero-section { position: relative; min-height: 520px; }
.hero-bg { position: absolute; inset: 0; overflow: hidden; }
.hero-bg-img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; opacity: 0; transition: opacity 1.5s ease; }
.hero-bg-img.visible { opacity: 1; }
.hero-bg-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.7) 40%, rgba(12,12,16,0.4) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 50%);
}
.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 240px 1fr;
  gap: 40px; padding: 40px 40px 48px; max-width: 1300px;
}
.hero-poster { align-self: start; }
.hero-info { display: flex; flex-direction: column; justify-content: center; }
.detail-badges { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.detail-synopsis { font-size: 14px; line-height: 1.65; color: var(--fg-1); max-width: 640px; margin: 12px 0 0; display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 8px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }
.detail-actions { display: flex; align-items: center; gap: 10px; margin: 16px 0; }
.btn-icon { background: none; border: none; cursor: pointer; padding: 4px; }
.info-grid { display: grid; grid-template-columns: auto 1fr; gap: 4px 20px; font-size: 12px; margin-top: 16px; max-width: 500px; }
.info-label { color: var(--fg-3); font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.06em; font-size: 10px; padding-top: 2px; }
.info-value { font-size: 13px; color: var(--fg-1); line-height: 1.5; }
.keyword-tag { font-size: 10px; padding: 3px 10px; border-radius: 100px; background: var(--bg-3); color: var(--fg-2); font-family: var(--font-mono); }

/* Seasons grid */
.seasons-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 18px; }
.season-card { text-decoration: none; color: inherit; }
.season-card:hover .grid-tile-title { color: var(--gold); }
.ep-count { font-size: 11px; color: var(--fg-3); font-weight: 400; }

/* Body */
.detail-body-below { padding: 0 48px 80px; }
.detail-section { margin-top: 36px; }
.section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }

/* Tabs + Cast + Crew + Videos + Recs — same as movie page */
.tab-bar { display: flex; gap: 4px; }
.tab-btn { padding: 8px 16px; border-radius: var(--r-md); font-size: 13px; font-weight: 500; color: var(--fg-2); background: none; border: none; cursor: pointer; transition: all 0.15s; }
.tab-btn:hover { background: rgba(255,255,255,0.04); }
.tab-btn.active { background: var(--bg-3); color: var(--fg-0); font-weight: 600; }
.tab-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-left: 4px; }
.hscroll { display: flex; gap: 14px; overflow-x: auto; scrollbar-width: none; padding-bottom: 4px; }
.hscroll::-webkit-scrollbar { display: none; }
.cast-card { width: 110px; flex-shrink: 0; text-decoration: none; color: inherit; text-align: center; }
.cast-card:hover .cast-name { color: var(--gold); }
.cast-photo { width: 80px; height: 80px; border-radius: 50%; object-fit: cover; }
.cast-avatar { width: 80px; height: 80px; border-radius: 50%; margin: 0 auto; background: linear-gradient(135deg, var(--bg-4), var(--bg-3)); display: flex; align-items: center; justify-content: center; font-size: 20px; font-weight: 600; color: var(--fg-2); }
.cast-name { font-size: 12px; font-weight: 500; margin-top: 8px; transition: color 0.15s; }
.cast-role { font-size: 10px; color: var(--fg-3); margin-top: 2px; }
.crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px; }
.crew-card { display: flex; align-items: center; gap: 10px; padding: 8px; border-radius: var(--r-md); text-decoration: none; color: inherit; transition: background 0.15s; }
.crew-card:hover { background: rgba(255,255,255,0.04); }
.crew-photo { width: 36px; height: 36px; border-radius: 50%; object-fit: cover; flex-shrink: 0; }
.crew-initials { width: 36px; height: 36px; border-radius: 50%; flex-shrink: 0; background: var(--bg-4); display: flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 600; color: var(--fg-2); }
.crew-name { font-size: 13px; font-weight: 500; }
.crew-job { font-size: 11px; color: var(--fg-3); }
.video-card { width: 280px; flex-shrink: 0; text-decoration: none; color: inherit; }
.video-card:hover .video-name { color: var(--gold); }
.video-thumb { position: relative; aspect-ratio: 16/9; border-radius: var(--r-md); overflow: hidden; background: var(--bg-3); }
.video-thumb img { width: 100%; height: 100%; object-fit: cover; }
.video-play { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.4); opacity: 0; transition: opacity 0.15s; color: #fff; }
.video-card:hover .video-play { opacity: 1; }
.video-name { font-size: 12px; font-weight: 500; margin-top: 8px; transition: color 0.15s; }
.video-type { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); text-transform: uppercase; }
.rec-card { width: 130px; flex-shrink: 0; text-decoration: none; color: inherit; }
.rec-card:hover .grid-tile-title { color: var(--gold); }
.rec-card.dimmed { opacity: 0.4; pointer-events: none; }
.rating-card { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); padding: 14px 20px; text-align: center; min-width: 120px; }
.rating-source { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; }
.rating-value { font-size: 20px; font-weight: 700; margin-top: 4px; }

@media (max-width: 900px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 20px 24px; }
  .hero-poster { display: none; }
  .detail-title { font-size: 32px; }
  .detail-body-below { padding: 0 20px 60px; }
  .seasons-grid { grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 12px; }
}
</style>
