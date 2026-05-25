<template>
  <div class="page-pad">
    <h2 class="m-h2">My Albums</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">
      No favorited albums yet — tap the heart on an album page to add it here.
    </div>
    <div v-else class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(180px, 1fr))">
      <NuxtLink
        v-for="al in rows"
        :key="al.id"
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="grid-tile card-tile"
        style="text-decoration: none; color: inherit"
      >
        <Poster :idx="al.id" :src="useAlbumCoverUrl(al.id)" aspect="1/1" />
        <div class="grid-tile-meta">
          <div class="grid-tile-title">{{ al.title }}</div>
          <div class="grid-tile-sub">{{ al.artist_name }}{{ al.year ? ' · ' + al.year : '' }}</div>
        </div>
      </NuxtLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicAlbumRow, MusicListPage } from '~~/shared/types'

definePageMeta({ layout: 'default' })

interface LovedAlbumRow extends MusicAlbumRow { loved_at: string }

const myAlbumsRes = await useHeya('/api/me/loved/albums', { query: { limit: 500 } })
const data = myAlbumsRes.data as unknown as Ref<MusicListPage<LovedAlbumRow> | null>
const pending = myAlbumsRes.pending
const rows = computed(() => data.value?.items ?? [])
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; max-width: 480px; }
</style>
