<template>
  <div class="page-pad">
    <MusicPageHead title="My Albums">
      <template #subtitle>
        <span v-if="total !== null">{{ total.toLocaleString() }} albums you've rated</span>
        <span v-else>Loading…</span>
      </template>
    </MusicPageHead>
    <div v-if="pending" class="m-loading">Loading…</div>
    <MusicEmptyState v-else-if="!total" icon="heart" title="No rated albums yet">
      Tap the stars on an album page — anything 1★+ lands here. Start from <NuxtLink to="/music/albums">Albums</NuxtLink>.
    </MusicEmptyState>
    <!-- Random-access virtual grid — scrollbar spans every rated album,
         pages fetch wherever it lands (500-cap gone). -->
    <VirtualPosterGrid
      v-else
      :total="total ?? 0"
      :item-at="itemAt"
      :aspect="1"
      :min-card="180"
      @range="ensureRange"
    >
      <template #default="{ item: al }">
        <AppContextMenu
          :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name })"
        >
          <NuxtLink
            :to="`/music/artist/${al.artist_slug}/${al.slug}`"
            class="grid-tile"
            style="text-decoration: none; color: inherit"
          >
            <MusicCard
              :src="useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined"
              :alt="al.title"
              :title="al.title"
              :subtitle="al.artist_name + (al.year ? ' · ' + al.year : '')"
              @play="playAlbum(al)"
            />
          </NuxtLink>
        </AppContextMenu>
      </template>
    </VirtualPosterGrid>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { LovedAlbumRow } from '~/queries/music'
import { musicAlbumDetailQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const { $heya } = useNuxtApp()
const { play, queue } = usePlayerBindings()
const actions = useMusicActions()
const loadQuery = useQueryLoader()

const { total, pending, itemAt, ensureRange } = useVirtualCatalog<LovedAlbumRow>(() => ({
  key: 'me:rated:albums',
  pageSize: 120,
  fetch: async (offset, limit) => {
    const res = await $heya('/api/me/ratings/albums', {
      query: { min_rating: 1, limit, offset },
    }) as unknown as { items: LovedAlbumRow[]; total: number }
    return { items: res.items ?? [], total: res.total ?? 0 }
  },
}))

// Mirrors playLovedAlbum in my/index.vue — load the album's tracks on demand
// (the list row doesn't carry them) and queue+play the ones still on disk.
async function playAlbum(al: LovedAlbumRow) {
  try {
    const detail = await loadQuery(musicAlbumDetailQuery({ artistSlug: al.artist_slug, albumSlug: al.slug }))
    const playable = (detail.tracks ?? []).filter((t) => (t.files?.length ?? 0) > 0)
    if (!playable.length) return
    const built: Track[] = playable.map((t) => ({
      id: t.id,
      title: t.title,
      artist: al.artist_name,
      album: al.title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.id}/stream`,
      album_id: al.id,
      artist_slug: al.artist_slug,
      album_slug: al.slug,
      poster: useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined,
      source: 'my-music',
    }))
    queue.value = built
    await play(built[0]!)
  } catch {
    // fall through — outer link still navigates
  }
}
</script>

<style scoped>
.m-loading { color: var(--fg-2); padding: 24px 0; font-size: 13px; max-width: 480px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }
@media (max-width: 720px) {
  /* heya.css's .page-pad is 24px a side at this width — with 12px grid gaps
     that leaves room for exactly 2×165px tracks, not the 3 columns a phone
     grid should land. Tighten this page's own padding instance (not the
     shared heya.css rule) so 3×~111px columns fit a 390px screen. */
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
