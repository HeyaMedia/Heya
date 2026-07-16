<template>
  <div class="page-pad">
    <MusicPageHead title="Artists">
      <template #subtitle>
        <span v-if="total !== null">{{ total.toLocaleString() }} artists in your library</span>
        <span v-else>Loading…</span>
      </template>
    </MusicPageHead>
    <div v-if="pending" class="m-loading">Loading…</div>
    <MusicEmptyState v-else-if="!total" icon="users" title="No artists yet">
      Add a music library from <NuxtLink to="/settings/libraries">Settings → Libraries</NuxtLink> to start building your collection.
    </MusicEmptyState>
    <!-- Random-access virtual grid: sized to the full artist count up front,
         so the scrollbar spans the whole roster and dragging anywhere
         fetches that page (no more 500-artist cap). Title/subtitle paint
         on the art itself, so no reserved meta space under the tile. -->
    <VirtualPosterGrid
      v-else
      :total="total ?? 0"
      :item-at="itemAt"
      :aspect="1"
      :min-card="160"
      @range="ensureRange"
    >
      <template #default="{ item: a }">
        <AppContextMenu
          :items="actions.forArtist({ id: a.id, name: a.name, slug: a.slug, media_item_id: a.media_item_id, available: a.available })"
        >
          <NuxtLink
            :to="`/music/artist/${a.slug}`"
            class="grid-tile"
            style="text-decoration: none; color: inherit"
          >
            <MusicCard
              variant="square"
              :src="artistPosterUrl(a) ?? undefined"
              :alt="a.name"
              :title="a.name"
              :subtitle="`${a.album_count} ${a.album_count === 1 ? 'album' : 'albums'} · ${a.track_count} ${a.track_count === 1 ? 'track' : 'tracks'}`"
              :hearted="(artistRatingValues.get(a.id) ?? 0) >= 9"
              no-play
              :missing="a.available === false"
            />
          </NuxtLink>
        </AppContextMenu>
      </template>
    </VirtualPosterGrid>
  </div>
</template>

<script setup lang="ts">
import type { MusicArtistRow } from '~~/shared/types'

definePageMeta({ layout: 'default' })

const { $heya } = useNuxtApp()
const actions = useMusicActions()
const artistRatings = useRatings('artist')
const artistRatingValues = artistRatings.ratings

const { total, pending, itemAt, ensureRange } = useVirtualCatalog<MusicArtistRow>(() => ({
  key: 'music:artists:list',
  pageSize: 120,
  fetch: async (offset, limit) => {
    const res = await $heya('/api/music/artists', { query: { limit, offset } }) as unknown as {
      items: MusicArtistRow[]
      total: number
    }
    const items = res.items ?? []
    if (items.length) void artistRatings.primeBulk(items.map(a => a.id))
    return { items, total: res.total ?? 0 }
  },
}))

// See MusicHome.vue — the endpoint falls back through media_assets when
// media_items.poster_path is empty, so unconditional URL emit + MusicCard's
// imgError fallback handles both populated and missing-image cases.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl({ id: a.media_item_id, public_id: a.media_item_public_id })
</script>

<style scoped>
.m-loading { color: var(--fg-2); padding: 24px 0; font-size: 13px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }

@media (max-width: 720px) {
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
