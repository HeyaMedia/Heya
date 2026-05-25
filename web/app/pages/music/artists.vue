<template>
  <div class="page-pad">
    <h2 class="m-h2">Artists</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">No artists yet — add a music library to get started.</div>
    <div v-else class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(160px, 1fr))">
      <NuxtLink
        v-for="a in rows"
        :key="a.id"
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
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicArtistRow, MusicListPage } from '~~/shared/types'

definePageMeta({ layout: 'default' })

const artistsRes = await useHeya('/api/music/artists', { query: { limit: 500 } })
const data = artistsRes.data as unknown as Ref<MusicListPage<MusicArtistRow> | null>
const pending = artistsRes.pending
const rows = computed(() => data.value?.items ?? [])

// See MusicHome.vue — the endpoint falls back through media_assets when
// media_items.poster_path is empty, so unconditional URL emit + Poster's
// imgError gradient handles both populated and missing-image cases.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl(a.media_item_id)
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
</style>
