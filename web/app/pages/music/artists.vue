<template>
  <div class="page-pad">
    <MusicPageHead title="Artists">
      <template #subtitle>{{ rows.length }} artists in your library</template>
    </MusicPageHead>
    <div v-if="pending" class="m-loading">Loading…</div>
    <MusicEmptyState v-else-if="!rows.length" icon="users" title="No artists yet">
      Add a music library from <NuxtLink to="/settings/libraries">Settings → Libraries</NuxtLink> to start building your collection.
    </MusicEmptyState>
    <div v-else class="grid-posters m-grid">
      <AppContextMenu
        v-for="a in rows"
        :key="a.id"
        :items="actions.forArtist({ id: a.id, name: a.name, slug: a.slug, media_item_id: a.media_item_id, available: a.available })"
      >
      <NuxtLink
        :to="`/music/artist/${a.slug}`"
        class="grid-tile"
        style="text-align: center; text-decoration: none; color: inherit"
      >
        <MusicCard
          variant="circle"
          :src="artistPosterUrl(a) ?? undefined"
          :alt="a.name"
          :title="a.name"
          no-play
          :missing="a.available === false"
        />
        <div class="ms-circle-label">{{ a.name }}</div>
        <div class="ms-circle-sub">{{ a.album_count }} {{ a.album_count === 1 ? 'album' : 'albums' }} · {{ a.track_count }} {{ a.track_count === 1 ? 'track' : 'tracks' }}</div>
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
// media_items.poster_path is empty, so unconditional URL emit + MusicCard's
// imgError fallback handles both populated and missing-image cases.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl({ id: a.media_item_id, public_id: a.media_item_public_id })
</script>

<style scoped>
.m-loading { color: var(--fg-2); padding: 24px 0; font-size: 13px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }

.ms-circle-label {
  margin-top: 10px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.ms-circle-sub {
  font-size: 11px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}

/* Was inline-style grid-template-columns — moved to a scoped class so the
   phone override below can win (media queries can't beat an inline style). */
.m-grid { grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); }

/* MusicCard's root is height:100% (built for uniform card grids). Here the
   grid stretches every item in a row to the tallest, the card then fills
   that stretched cell, and the name/count labels below it overflow into the
   next row. Let the card size to its art instead. */
.m-grid :deep(.mc) { height: auto; }
@media (max-width: 720px) {
  .m-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); }
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
