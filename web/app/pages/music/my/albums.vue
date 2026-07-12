<template>
  <div class="page-pad">
    <MusicPageHead title="My Albums">
      <template #subtitle>{{ rows.length }} albums you've rated</template>
    </MusicPageHead>
    <div v-if="pending" class="m-loading">Loading…</div>
    <MusicEmptyState v-else-if="!rows.length" icon="heart" title="No rated albums yet">
      Tap the stars on an album page — anything 1★+ lands here. Start from <NuxtLink to="/music/albums">Albums</NuxtLink>.
    </MusicEmptyState>
    <div v-else class="grid-posters m-grid">
      <AppContextMenu
        v-for="al in rows"
        :key="al.id"
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
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { LovedAlbumRow } from '~/queries/music'
import { useQuery } from '@pinia/colada'
import { lovedAlbumsQuery, musicAlbumDetailQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayerBindings()
const actions = useMusicActions()
const loadQuery = useQueryLoader()
const myAlbumsQuery = useQuery(lovedAlbumsQuery(500))
await waitForQuery(myAlbumsQuery)
const pending = computed(() => myAlbumsQuery.isPending.value)
const rows = computed(() => myAlbumsQuery.data.value?.items ?? [])

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
