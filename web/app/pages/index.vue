<template>
  <div class="scroll" style="height: 100%">
    <HeroA :items="heroItems" :movies="movieDetails" :play-info="heroPlayInfo" @play="onHeroPlay" />

    <div class="page-pad">
      <ContinueWatchingRow
        v-if="continueWatching.length"
        :items="continueWatching"
        @play="onPlayContinue"
      />

      <UpNextRow
        v-if="upNextItems.length"
        :items="upNextItems"
        @play="onPlayUpNext"
      />

      <ContentRow
        v-if="favoriteItems.length"
        title="Your Favorites"
        :items="favoriteItems"
        @tile="(item) => navigateTo(mediaUrl(item))"
      />

      <ContentRow
        v-if="recommendedItems.length"
        title="Recommended For You"
        subtitle="Based on your library"
        :items="recommendedItems"
        @tile="(item) => navigateTo(mediaUrl(item))"
      />

      <ContentRow
        v-if="recentMovies.length"
        title="Recently Added Films"
        subtitle="Across all libraries"
        :items="recentMovies"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/movies')"
      />

      <ContentRow
        v-if="recentTV.length"
        title="Recently Added TV Shows"
        subtitle="Across all libraries"
        :items="recentTV"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/tv')"
      />

      <ContentRow
        v-if="recentMusic.length"
        title="Recently Added Music"
        subtitle="Across all libraries"
        :items="recentMusic"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/music')"
      />

      <ContentRow
        v-if="recentBooks.length"
        title="Recently Added Books"
        subtitle="Across all libraries"
        :items="recentBooks"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/books')"
      />

      <div v-if="!loading && !hasContent" class="empty-home">
        <h2>Welcome to Heya</h2>
        <p>Add a library and scan it to see your media here.</p>
        <NuxtLink to="/libraries" class="btn btn-primary" style="margin-top: 16px">
          <Icon name="plus" :size="16" />
          Add Library
        </NuxtLink>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, MediaDetail, MediaType, Movie } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'
import type { HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { UpNextItem } from '~/components/home/UpNextRow.vue'

const recentMovies = ref<MediaItem[]>([])
const recentTV = ref<MediaItem[]>([])
const recentMusic = ref<MediaItem[]>([])
const recentBooks = ref<MediaItem[]>([])
const movieDetails = ref<Record<number, Movie>>({})
const heroPlayInfo = ref<Record<number, HeroPlayInfo>>({})
const continueWatching = ref<ContinueWatchingItem[]>([])
const upNextItems = ref<UpNextItem[]>([])
const favoriteItems = ref<MediaItem[]>([])
const recommendedItems = ref<MediaItem[]>([])
const loading = ref(true)

const heroItems = computed(() => {
  const combined = [
    ...recentMovies.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
    ...recentTV.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
  ]
  combined.sort((a, b) => b._sort - a._sort)
  return combined.slice(0, 5)
})

const hasContent = computed(() =>
  recentMovies.value.length + recentTV.value.length + recentMusic.value.length + recentBooks.value.length > 0
)

function onPlayContinue(item: ContinueWatchingItem) {
  if (item.entity_type === 'episode') {
    navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, media_type: item.media_type as any } as MediaItem))
  } else {
    navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, media_type: item.media_type as any } as MediaItem))
  }
}

async function loadMedia() {
  const { $heya } = useNuxtApp()
  // Type-specific feeds first so the cross-cutting endpoints below can dedupe
  // against them.
  const typeRefsTuple: [MediaType, Ref<MediaItem[]>][] = [
    ['movie', recentMovies], ['tv', recentTV], ['music', recentMusic], ['book', recentBooks],
  ]
  await Promise.allSettled(typeRefsTuple.map(async ([t, target]) => {
    try {
      target.value = await $heya('/api/media', { query: { type: t, limit: 20 } }) as MediaItem[]
    } catch (e) {
      console.warn(`Failed to load ${t}:`, e)
    }
  }))

  // Cross-cutting personal data — keep as a typed 4-tuple so each result keeps
  // its own value shape after destructuring.
  type RecentlyWatchedRow = {
    media_item_id: number
    title: string
    poster_path: string
    slug: string
    media_type: string
  }
  const [cwRes, rwRes, favRes, recRes] = await Promise.allSettled([
    $heya('/api/me/watch/continue') as Promise<ContinueWatchingItem[]>,
    $heya('/api/me/watch/recent') as Promise<RecentlyWatchedRow[]>,
    $heya('/api/me/state', {
      method: 'POST',
      body: { scope: 'movies' } as any,
    }) as Promise<{ favorited: number[] }>,
    $heya('/api/recommendations', { query: { limit: 20 } }) as Promise<{ local_media_item_id: number | null }[]>,
  ])

  if (cwRes.status === 'fulfilled') {
    continueWatching.value = cwRes.value || []
  }

  // Up Next: for each unique TV series the user has watch history on,
  // resolve the next unwatched episode. We never show movies here — a
  // completed movie has no "next" and Continue Watching already covers
  // a partway-through movie.
  if (rwRes.status === 'fulfilled' && rwRes.value?.length) {
    const tvSeries = new Map<number, RecentlyWatchedRow>()
    for (const row of rwRes.value) {
      if (row.media_type !== 'tv') continue
      if (!tvSeries.has(row.media_item_id)) tvSeries.set(row.media_item_id, row)
    }
    const resolved = await Promise.allSettled(
      Array.from(tvSeries.values()).map(async row => {
        const up = await $heya('/api/media/{id}/up-next', { path: { id: row.media_item_id as any } }) as {
          has_next: boolean; file_id?: number
          season_number?: number; episode_number?: number; episode_title?: string
        }
        return { row, up }
      })
    )
    const entries: UpNextItem[] = []
    for (const r of resolved) {
      if (r.status !== 'fulfilled') continue
      const { row, up } = r.value
      if (!up?.has_next || !up.file_id) continue
      const sNum = up.season_number ?? 0
      const eNum = up.episode_number ?? 0
      const s = String(sNum).padStart(2, '0')
      const e = String(eNum).padStart(2, '0')
      const label = up.episode_title ? `S${s}E${e} · ${up.episode_title}` : `S${s}E${e}`
      entries.push({
        id: row.media_item_id,
        title: row.title,
        slug: row.slug,
        season_number: sNum,
        episode_number: eNum,
        episode_label: label,
        play_file_id: up.file_id,
      })
    }
    upNextItems.value = entries.slice(0, 20)
  }

  if (favRes.status === 'fulfilled') {
    const favIDs = new Set(favRes.value?.favorited || [])
    if (favIDs.size > 0) {
      const allMedia = [...recentMovies.value, ...recentTV.value]
      favoriteItems.value = allMedia.filter(m => favIDs.has(m.id))
    }
  }

  if (recRes.status === 'fulfilled' && recRes.value?.length) {
    const allMedia = [...recentMovies.value, ...recentTV.value]
    const mediaMap = new Map(allMedia.map(m => [m.id, m]))
    const localRecs = recRes.value
      .filter(r => r.local_media_item_id !== null)
      .map(r => mediaMap.get(r.local_media_item_id as number))
      .filter((m): m is MediaItem => !!m)
    const existingIds = new Set([
      ...favoriteItems.value.map(m => m.id),
      ...upNextItems.value.map(m => m.id),
    ])
    recommendedItems.value = localRecs.filter(m => !existingIds.has(m.id)).slice(0, 20)
  }

  for (const item of heroItems.value) {
    try {
      // /api/media/{id} accepts slug or numeric ID — spec types id as string.
      const detail = await $heya('/api/media/{id}', { path: { id: String(item.id) } }) as MediaDetail
      if (detail.movie) {
        movieDetails.value[item.id] = detail.movie
        const fileId = detail.files?.[0]?.id ?? null
        if (fileId) heroPlayInfo.value[item.id] = { fileId }
      } else if (detail.tv_series) {
        // Hero only reads a small subset (genres, rating, release_date) so a
        // minimal Movie-shaped projection is enough.
        movieDetails.value[item.id] = {
          id: 0, media_item_id: item.id,
          runtime_minutes: 0, tagline: '', genres: detail.tv_series.genres || [],
          rating: detail.tv_series.rating, release_date: detail.tv_series.first_air_date,
          original_title: '', original_language: '', budget: 0, revenue: 0,
        }
        // Resolve next-unwatched episode separately so Play jumps straight
        // into the show, matching the series page's "Play SXXEXX - Title"
        // button verbatim.
        try {
          const up = await $heya('/api/media/{id}/up-next', { path: { id: item.id as any } }) as {
            has_next: boolean; file_id?: number
            season_number?: number; episode_number?: number; episode_title?: string
          }
          if (up?.has_next && up.file_id) {
            const s = String(up.season_number ?? 0).padStart(2, '0')
            const e = String(up.episode_number ?? 0).padStart(2, '0')
            const base = `S${s}E${e}`
            const label = up.episode_title ? `${base} - ${up.episode_title}` : base
            heroPlayInfo.value[item.id] = { fileId: up.file_id, label }
          }
        } catch { /* empty */ }
      }
    } catch { /* empty */ }
  }

  loading.value = false
}

function onHeroPlay(item: MediaItem) {
  const info = heroPlayInfo.value[item.id]
  if (!info?.fileId) return
  const titleSuffix = info.label ? ` - ${info.label}` : ''
  const params = new URLSearchParams({
    media_item_id: String(item.id),
    title: `${item.title}${titleSuffix}`,
  })
  navigateTo(`/watch/${info.fileId}?${params}`)
}

function onPlayUpNext(entry: UpNextItem) {
  const s = String(entry.season_number).padStart(2, '0')
  const e = String(entry.episode_number).padStart(2, '0')
  const params = new URLSearchParams({
    media_item_id: String(entry.id),
    title: `${entry.title} - S${s}E${e}`,
  })
  navigateTo(`/watch/${entry.play_file_id}?${params}`)
}

const { on } = useEventBus()
const mediaRefreshTimers: Record<string, ReturnType<typeof setTimeout>> = {}
const typeRefs: Record<string, Ref<MediaItem[]>> = {
  movie: recentMovies, tv: recentTV, music: recentMusic, book: recentBooks,
}

onMounted(() => {
  loadMedia()

  const { $heya } = useNuxtApp()
  const unsubs = [
    on('media.added', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      const target = mt ? typeRefs[mt] : undefined
      if (!mt || !target) return
      const existing = mediaRefreshTimers[mt]
      if (existing) clearTimeout(existing)
      mediaRefreshTimers[mt] = setTimeout(() => {
        ($heya('/api/media', { query: { type: mt as any, limit: 20 } }) as Promise<MediaItem[]>)
          .then(items => { target.value = items })
          .catch(() => {})
      }, 2000)
    }),
    on('media.updated', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      const target = mt ? typeRefs[mt] : undefined
      if (!mt || !target) return
      const existing = mediaRefreshTimers[mt]
      if (existing) clearTimeout(existing)
      mediaRefreshTimers[mt] = setTimeout(() => {
        ($heya('/api/media', { query: { type: mt as any, limit: 20 } }) as Promise<MediaItem[]>)
          .then(items => { target.value = items })
          .catch(() => {})
      }, 3000)
    }),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    Object.values(mediaRefreshTimers).forEach(t => clearTimeout(t))
  })
})
</script>

<style scoped>
.empty-home {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 0;
  text-align: center;
  color: var(--fg-2);
}
.empty-home h2 {
  font-size: 28px;
  font-weight: 600;
  color: var(--fg-0);
  margin-bottom: 8px;
}
.empty-home p {
  font-size: 15px;
}
</style>
