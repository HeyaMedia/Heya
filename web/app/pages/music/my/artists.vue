<template>
  <div class="page-pad">
    <h2 class="m-h2">My Artists</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">
      No rated artists yet — rate an artist from their page to add them here.
    </div>
    <div v-else class="grid-posters m-grid">
      <AppContextMenu
        v-for="a in rows"
        :key="a.id"
        :items="actions.forArtist({ id: a.id, name: a.name, slug: a.slug, media_item_id: a.media_item_id })"
      >
      <NuxtLink
        :to="`/music/artist/${a.slug}`"
        class="grid-tile card-tile"
        style="text-align: center; text-decoration: none; color: inherit"
      >
        <Poster :idx="a.id" :src="artistPosterUrl(a)" aspect="1/1" style="border-radius: 50%" />
        <div style="margin-top: 10px">
          <div style="font-size: 13px; font-weight: 500">{{ a.name }}</div>
          <div style="font-size: 11px; color: var(--fg-3); font-family: var(--font-mono)">{{ a.album_count }} / {{ a.track_count }}</div>
        </div>
      </NuxtLink>
      </AppContextMenu>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicArtistRow, MusicListPage } from '~~/shared/types'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface LovedArtistRow extends MusicArtistRow { loved_at: string }

const { $heya } = useNuxtApp()
const actions = useMusicActions()
const myArtistsQuery = useQuery({
  queryKey: ['me', 'loved', 'artists', { limit: 500 }],
  queryFn: async () => (await $heya('/api/me/ratings/artists', { query: { min_rating: 1, limit: 500 } })) as unknown as MusicListPage<LovedArtistRow>,
  staleTime: 1000 * 30,
})
const pending = computed(() => myArtistsQuery.isPending.value)
const rows = computed(() => myArtistsQuery.data.value?.items ?? [])

// See MusicHome.vue — endpoint falls back through media_assets when
// media_items.poster_path is empty.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl(a.media_item_id)
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; max-width: 480px; }
/* Was inline-style grid-template-columns — moved to a scoped class so the
   phone override below can win (media queries can't beat an inline style). */
.m-grid { grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); }
@media (max-width: 720px) {
  .m-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); }
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
