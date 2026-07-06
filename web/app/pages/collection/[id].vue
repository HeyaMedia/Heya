<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 340px; background: var(--bg-2)" />
  </div>

  <div v-else-if="collection" class="scroll" style="height: 100%">
    <!-- Hero with backdrop -->
    <div class="col-hero" v-if="collection.backdrop_path">
      <img :src="collection.backdrop_path" class="col-hero-bg" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
      <div class="col-hero-fade" />
    </div>

    <div class="page-pad" style="position: relative; z-index: 2" :style="collection.backdrop_path ? 'margin-top: -120px' : ''">
      <div class="col-header">
        <div v-if="collection.poster_path" class="col-poster">
          <img :src="collection.poster_path" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
        </div>
        <div class="col-info">
          <div class="col-eyebrow">Collection</div>
          <h1 class="col-title">{{ collection.name }}</h1>
          <p v-if="collection.overview" class="col-overview">{{ collection.overview }}</p>
          <div class="col-meta">
            <template v-if="parts.length">You own {{ ownedCount }} of {{ parts.length }} film<span v-if="parts.length !== 1">s</span></template>
            <template v-else>{{ movies.length }} movie<span v-if="movies.length !== 1">s</span></template>
          </div>
        </div>
      </div>

      <!-- Full franchise membership from heya.media: owned films link to the
           local movie, missing ones render dimmed with a badge. Falls back to
           just the local movies until a member is enriched with collection
           parts (progressive during the metadata backfill). -->
      <div v-if="parts.length" class="grid-posters" style="margin-top: 32px; padding-bottom: 80px">
        <component
          :is="p.local_media_item_id ? 'NuxtLink' : 'div'"
          v-for="(p, i) in parts"
          :key="p.tmdb_id || p.title"
          :to="p.local_media_item_id ? mediaUrl({ id: p.local_media_item_id, title: p.title, slug: p.local_slug ?? undefined, media_type: 'movie' }) : undefined"
          class="grid-tile card-tile"
          :class="{ 'part-missing': !p.local_media_item_id }"
        >
          <MediaCard
            :idx="i"
            :src="partPoster(p)"
            aspect="2/3"
            :title="p.title"
            :subtitle="p.year ? String(p.year) : ''"
            :badge-tr="p.vote_average ? `★ ${p.vote_average.toFixed(1)}` : ''"
            :badge-tr-gold="false"
          >
            <template v-if="!p.local_media_item_id" #badges>
              <div class="part-missing-badge">Not in library</div>
            </template>
          </MediaCard>
        </component>
      </div>

      <div v-else-if="movies.length" class="grid-posters" style="margin-top: 32px; padding-bottom: 80px">
        <NuxtLink
          v-for="(item, i) in movies"
          :key="item.id"
          :to="mediaUrl(item)"
          class="grid-tile card-tile"
        >
          <Poster :idx="i" :src="usePosterUrl(item.id)" aspect="2/3" :title="item.title" />
          <div class="grid-tile-meta">
            <div class="grid-tile-title">{{ item.title }}</div>
            <div class="grid-tile-sub">{{ item.year }}</div>
          </div>
        </NuxtLink>
      </div>
    </div>
  </div>

  <div v-else class="scroll" style="height: 100%; display: flex; align-items: center; justify-content: center">
    <div style="text-align: center; color: var(--fg-2)">
      <p style="font-size: 18px">Collection not found</p>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

interface CollectionDetail {
  id: number
  name: string
  overview: string
  poster_path: string
  backdrop_path: string
}

// One franchise film, resolved server-side to a local movie (owned) or not
// (missing). local_media_item_id/local_slug are set only when owned.
interface CollectionPart {
  title: string
  year?: number
  tmdb_id?: number
  poster_path?: string
  vote_average?: number
  local_media_item_id?: number | null
  local_slug?: string | null
}

const route = useRoute()
const id = computed(() => route.params.id as string)

const collection = ref<CollectionDetail | null>(null)
const movies = ref<MediaItem[]>([])
const parts = ref<CollectionPart[]>([])
const ownedCount = ref(0)
const loading = ref(true)

// Owned films use our local artwork; missing ones use heya.media's CDN poster
// (already an absolute URL — the image provider passes it through).
function partPoster(p: CollectionPart): string {
  if (p.local_media_item_id) return usePosterUrl(p.local_media_item_id) ?? ''
  return p.poster_path || ''
}

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/collections/{id}', { path: { id: Number(id.value) } }) as {
      collection: CollectionDetail; movies: MediaItem[]; parts: CollectionPart[]; owned_count: number
    }
    collection.value = res.collection
    movies.value = res.movies || []
    parts.value = res.parts || []
    ownedCount.value = res.owned_count || 0
  } catch { /* empty */ }
  loading.value = false
})
</script>

<style scoped>
.col-hero { position: relative; height: 340px; overflow: hidden; }
.col-hero-bg { width: 100%; height: 100%; object-fit: cover; }
.col-hero-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to top, var(--bg-1) 0%, transparent 60%),
    linear-gradient(to right, var(--bg-1) 0%, transparent 40%);
}

.col-header { display: flex; gap: 32px; align-items: flex-end; }
.col-poster {
  width: 180px; flex-shrink: 0; border-radius: var(--r-md); overflow: hidden;
  box-shadow: 0 16px 48px rgba(0,0,0,0.5);
}
.col-poster img { width: 100%; display: block; }
.col-info { display: flex; flex-direction: column; gap: 4px; }
.col-eyebrow {
  font-size: 10px; font-family: var(--font-mono); font-weight: 700;
  letter-spacing: 0.18em; text-transform: uppercase; color: var(--gold);
}
.col-title { font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.col-overview { font-size: 14px; line-height: 1.65; color: var(--fg-1); max-width: 600px; margin: 8px 0 0; }
.col-meta { font-size: 12px; font-family: var(--font-mono); color: var(--fg-3); margin-top: 4px; }

/* Films in the franchise the library doesn't have: dimmed, non-interactive,
   with a badge. The badge lives in MediaCard's slot so it stays in this page's
   scope (per the shared-slot-scope rule in docs/ui.md). */
.part-missing { cursor: default; }
.part-missing:hover { transform: none; }
.part-missing :deep(img) { filter: grayscale(0.85); opacity: 0.5; }
.part-missing-badge {
  position: absolute; top: 8px; left: 8px; z-index: 3;
  font-family: var(--font-mono); font-size: 9px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.05em;
  padding: 3px 7px; border-radius: 4px;
  background: rgba(0, 0, 0, 0.68); backdrop-filter: blur(6px);
  color: rgba(255, 255, 255, 0.82);
}

/* Folded from the previous 700px breakpoint onto the ratified 720px phone
   convention (docs/ui.md "Responsive conventions") — page-pad's own 16px
   side padding overrides heya.css's global .page-pad here since this page
   is a grid page per the W3c convention for collection/genre/keyword/lists. */
@media (max-width: 720px) {
  .page-pad { padding: 20px 16px 60px; }
  .col-header { flex-direction: column; align-items: flex-start; gap: 16px; }
  .col-poster { width: 120px; }
  .col-title { font-size: 26px; }
}
</style>
