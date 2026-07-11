<template>
  <div class="page-pad">
    <h2 class="m-h2">Artists</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">No artists yet — add a music library to get started.</div>
    <div v-else class="grid-posters m-grid">
      <AppContextMenu
        v-for="a in rows"
        :key="a.id"
        :items="actions.forArtist({ id: a.id, name: a.name, slug: a.slug, media_item_id: a.media_item_id, available: a.available })"
      >
      <NuxtLink
        :to="`/music/artist/${a.slug}`"
        class="grid-tile card-tile"
        style="text-align: center; text-decoration: none; color: inherit"
      >
        <!-- Badge sits outside the circular Poster (which clips its overflow)
             so it isn't masked; it anchors to the position:relative .card-tile. -->
        <Poster :idx="a.id" :src="artistPosterUrl(a)" aspect="1/1" style="border-radius: 50%" :class="{ 'poster--missing': a.available === false }" />
        <MediaMissingBadge v-if="a.available === false" />
        <div class="art-meta">
          <div class="art-name">{{ a.name }}</div>
          <div class="art-count">{{ a.album_count }} / {{ a.track_count }}</div>
        </div>
      </NuxtLink>
      </AppContextMenu>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicArtistRow } from '~~/shared/types'
import { useQuery } from '@pinia/colada'
import { musicArtistsQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const actions = useMusicActions()
const artistsQuery = useQuery(musicArtistsQuery())
await waitForQuery(artistsQuery)
const pending = computed(() => artistsQuery.isPending.value)
const rows = computed(() => artistsQuery.data.value?.items ?? [])

// See MusicHome.vue — the endpoint falls back through media_assets when
// media_items.poster_path is empty, so unconditional URL emit + Poster's
// imgError gradient handles both populated and missing-image cases.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl({ id: a.media_item_id, public_id: a.media_item_public_id })
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1); }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
.art-meta { margin-top: 10px; }
.art-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.art-count {
  font-size: 11px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
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
