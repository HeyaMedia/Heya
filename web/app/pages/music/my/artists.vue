<template>
  <div class="page-pad">
    <h2 class="m-h2">My Artists</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">
      No favorited artists yet — tap the heart on an artist's page to add them here.
    </div>
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

interface LovedArtistRow extends MusicArtistRow { loved_at: string }

const myArtistsRes = await useHeya('/api/me/loved/artists', { query: { limit: 500 } })
const data = myArtistsRes.data as unknown as Ref<MusicListPage<LovedArtistRow> | null>
const pending = myArtistsRes.pending
const rows = computed(() => data.value?.items ?? [])

// See MusicHome.vue — endpoint falls back through media_assets when
// media_items.poster_path is empty.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl(a.media_item_id)
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; max-width: 480px; }
</style>
