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
         fetches that page (no more 500-artist cap). metaHeight reserves the
         name + counts block under each circle. -->
    <VirtualPosterGrid
      v-else
      :total="total ?? 0"
      :item-at="itemAt"
      :aspect="1"
      :meta-height="48"
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
      </template>
    </VirtualPosterGrid>
  </div>
</template>

<script setup lang="ts">
import type { MusicArtistRow } from '~~/shared/types'

definePageMeta({ layout: 'default' })

const { $heya } = useNuxtApp()
const actions = useMusicActions()

const { total, pending, itemAt, ensureRange } = useVirtualCatalog<MusicArtistRow>(() => ({
  key: 'music:artists:list',
  pageSize: 120,
  fetch: async (offset, limit) => {
    const res = await $heya('/api/music/artists', { query: { limit, offset } }) as unknown as {
      items: MusicArtistRow[]
      total: number
    }
    return { items: res.items ?? [], total: res.total ?? 0 }
  },
}))

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

/* MusicCard's root is height:100% (built for uniform card grids). Here the
   virtual row stretches cells to the row height, the card would fill that
   stretched cell, and the name/count labels below it would overflow. Let
   the card size to its art instead. */
:deep(.mc) { height: auto; }

@media (max-width: 720px) {
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
