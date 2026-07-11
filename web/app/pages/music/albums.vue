<template>
  <div class="page-pad">
    <h2 class="m-h2">Albums</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">No albums yet</div>
    <div v-else class="grid-posters m-grid">
      <AppContextMenu
        v-for="al in rows"
        :key="al.id"
        :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name, available: al.available })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="grid-tile card-tile"
        style="text-decoration: none; color: inherit"
      >
        <Poster :idx="al.id" :src="useAlbumCoverUrl(al.artist_slug, al.slug)" aspect="1/1" :class="{ 'poster--missing': al.available === false }">
          <MediaMissingBadge v-if="al.available === false" />
        </Poster>
        <div class="grid-tile-meta">
          <div class="grid-tile-title">{{ al.title }}</div>
          <div class="grid-tile-sub">{{ al.artist_name }}{{ al.year ? ' · ' + al.year : '' }}</div>
        </div>
      </NuxtLink>
      </AppContextMenu>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import { musicAlbumsQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const actions = useMusicActions()
const albumsQuery = useQuery(musicAlbumsQuery())
await waitForQuery(albumsQuery)
const pending = computed(() => albumsQuery.isPending.value)
const rows = computed(() => albumsQuery.data.value?.items ?? [])
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1); }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
/* Was inline-style grid-template-columns — moved to a scoped class so the
   phone override below can win (media queries can't beat an inline style). */
.m-grid { grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); }
@media (max-width: 720px) {
  .m-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); }
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
