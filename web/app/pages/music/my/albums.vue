<template>
  <div class="page-pad">
    <h2 class="m-h2">My Albums</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">
      No favorited albums yet — tap the heart on an album page to add it here.
    </div>
    <div v-else class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(180px, 1fr))">
      <AppContextMenu
        v-for="al in rows"
        :key="al.id"
        :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="grid-tile card-tile"
        style="text-decoration: none; color: inherit"
      >
        <Poster :idx="al.id" :src="useAlbumCoverUrl(al.artist_slug, al.slug)" aspect="1/1" />
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
import type { MusicAlbumRow, MusicListPage } from '~~/shared/types'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface LovedAlbumRow extends MusicAlbumRow { loved_at: string }

const { $heya } = useNuxtApp()
const actions = useMusicActions()
const myAlbumsQuery = useQuery({
  queryKey: ['me', 'loved', 'albums', { limit: 500 }],
  queryFn: async () => (await $heya('/api/me/ratings/albums', { query: { min_rating: 1, limit: 500 } })) as unknown as MusicListPage<LovedAlbumRow>,
  staleTime: 1000 * 30,
})
const pending = computed(() => myAlbumsQuery.isPending.value)
const rows = computed(() => myAlbumsQuery.data.value?.items ?? [])
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; max-width: 480px; }
</style>
