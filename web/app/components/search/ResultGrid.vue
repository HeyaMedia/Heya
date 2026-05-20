<template>
  <div v-if="sectionKey === 'tracks'" class="track-list">
    <button v-for="item in items" :key="item.id" class="track-row" @click="go(item)">
      <div class="track-thumb">
        <img v-if="item.artist_media_item_id" :src="`/api/media/${item.artist_media_item_id}/image/poster`" loading="lazy" />
        <Icon v-else name="music" :size="16" />
      </div>
      <div class="track-body">
        <div class="track-title">{{ item.title }}</div>
        <div class="track-sub">{{ item.artist_name }} · {{ item.album_title }}</div>
      </div>
      <div v-if="item.duration_ms" class="track-duration">{{ fmtDuration(item.duration_ms) }}</div>
    </button>
  </div>

  <div v-else class="result-grid" :class="{ 'large': large }">
    <template v-if="sectionKey === 'people'">
      <button
        v-for="item in items"
        :key="item.id"
        class="person-card"
        @click="go(item)"
      >
        <div class="person-avatar">
          <img :src="personImageUrl(item.id)" loading="lazy" @error="onImgError" />
        </div>
        <div class="person-name">{{ item.name }}</div>
        <div class="person-sub">
          <span v-if="item.cast_count">{{ item.cast_count }} role{{ item.cast_count === 1 ? '' : 's' }}</span>
          <span v-if="item.crew_count">{{ item.crew_count }} credit{{ item.crew_count === 1 ? '' : 's' }}</span>
        </div>
      </button>
    </template>

    <template v-else>
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="grid-tile card-tile"
        @click="go(item)"
      >
        <Poster
          :idx="i"
          :src="posterUrl(item)"
          :aspect="aspectFor()"
          :title="title(item)"
        />
        <div class="grid-tile-meta">
          <div class="grid-tile-title">{{ title(item) }}</div>
          <div v-if="subtitle(item)" class="grid-tile-sub">{{ subtitle(item) }}</div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  sectionKey: string
  items: any[]
  large?: boolean
}>()

function aspectFor() {
  if (props.sectionKey === 'music' || props.sectionKey === 'albums') return '1/1'
  return '2/3'
}

function posterUrl(item: any): string | null {
  switch (props.sectionKey) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return `/api/media/${item.id}/image/poster`
    case 'albums':
      return item.artist_media_item_id ? `/api/media/${item.artist_media_item_id}/image/poster` : null
    case 'collections':
      return null
  }
  return null
}

function title(item: any): string {
  if (props.sectionKey === 'collections') return item.name
  return item.title
}

function subtitle(item: any): string {
  switch (props.sectionKey) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return item.year || ''
    case 'albums':
      return [item.artist_name, item.year].filter(Boolean).join(' · ')
    case 'collections':
      return ''
  }
  return ''
}

function go(item: any) {
  switch (props.sectionKey) {
    case 'movies':
      return navigateTo(`/movies/${item.slug || slugify(item.title)}`)
    case 'tv':
      return navigateTo(`/tv/${item.slug || slugify(item.title)}`)
    case 'music':
      return navigateTo(`/music/${item.slug || slugify(item.title)}`)
    case 'books':
      return navigateTo(`/books/${item.slug || slugify(item.title)}`)
    case 'people':
      return navigateTo(`/person/${item.slug || slugify(item.name)}`)
    case 'albums':
      return navigateTo(`/music/${item.artist_slug || slugify(item.artist_name)}#album-${item.id}`)
    case 'tracks':
      return navigateTo(`/music/${item.artist_slug || slugify(item.artist_name)}#track-${item.id}`)
  }
}

function fmtDuration(ms: number): string {
  const total = Math.round(ms / 1000)
  const m = Math.floor(total / 60)
  const s = (total % 60).toString().padStart(2, '0')
  return `${m}:${s}`
}

function onImgError(e: Event) {
  const img = e.target as HTMLImageElement
  img.style.display = 'none'
}
</script>

<style scoped>
.result-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 16px;
}
.result-grid.large {
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 20px;
}

.result-grid.people-list {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

.grid-tile {
  display: flex;
  flex-direction: column;
  gap: 8px;
  cursor: pointer;
}
.grid-tile-meta { padding: 0 2px; }
.grid-tile-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  line-height: 1.3;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.grid-tile-sub {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 2px;
}

.person-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 14px 8px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  text-align: center;
  transition: border-color 0.15s ease, transform 0.15s ease;
}
.person-card:hover {
  border-color: var(--gold-soft);
  transform: translateY(-2px);
}
.person-avatar {
  width: 80px; height: 80px;
  border-radius: 50%;
  background: var(--bg-3);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}
.person-avatar img {
  width: 100%; height: 100%; object-fit: cover; display: block;
}
.person-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  line-height: 1.3;
}
.person-sub {
  display: flex;
  flex-direction: column;
  gap: 1px;
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.track-row {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  text-align: left;
  color: var(--fg-0);
  transition: background 0.12s ease;
}
.track-row:hover { background: rgba(255,255,255,0.03); }
.track-thumb {
  width: 40px; height: 40px;
  background: var(--bg-3);
  border-radius: var(--r-xs);
  overflow: hidden;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-4);
  flex-shrink: 0;
}
.track-thumb img { width: 100%; height: 100%; object-fit: cover; }
.track-body { flex: 1; min-width: 0; }
.track-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.track-sub {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.track-duration {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.track-list { display: flex; flex-direction: column; }
</style>
