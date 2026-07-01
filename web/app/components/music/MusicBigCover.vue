<template>
  <!-- The "fold out into sidebar" cover: a big square anchored in the bottom-
       left corner of the music shell. It overlaps the bottom of the sidebar
       (which reserves space via .ms-cover-expanded) and the playbar's left cell
       (whose text shifts right via .pb-left-expanded). Toggled from the small
       cover's hover action; collapses from its own hover button. -->
  <Transition name="mbc">
    <div v-if="show" class="music-big-cover">
      <NuxtLink v-if="albumTo" :to="albumTo" class="mbc-link" aria-label="Go to album">
        <Poster :idx="currentTrack!.id" :src="cover" aspect="1/1" class="mbc-img" />
      </NuxtLink>
      <div v-else class="mbc-link">
        <Poster :idx="currentTrack!.id" :src="cover" aspect="1/1" class="mbc-img" />
      </div>

      <div class="mbc-actions">
        <AppTooltip label="Show full image">
          <button class="mbc-action" @click.prevent.stop="openLightbox"><Icon name="expand" :size="15" /></button>
        </AppTooltip>
        <AppTooltip label="Fold back in">
          <button class="mbc-action" @click.prevent.stop="collapse"><Icon name="collapse" :size="15" /></button>
        </AppTooltip>
      </div>

      <div class="mbc-meta">
        <div class="mbc-title">{{ currentTrack!.title }}</div>
        <div class="mbc-artist">{{ currentTrack!.artist }}</div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
const { currentTrack } = usePlayer()
const coverExpanded = useState('music_cover_expanded', () => false)
const lightbox = useLightbox()

const show = computed(() => coverExpanded.value && !!currentTrack.value)
const cover = computed(() => currentTrack.value?.poster ?? null)
const albumTo = computed(() =>
  currentTrack.value?.artist_slug && currentTrack.value?.album_slug
    ? `/music/artist/${currentTrack.value.artist_slug}/${currentTrack.value.album_slug}`
    : null)

function openLightbox() { if (cover.value) lightbox.open(cover.value) }
function collapse() { coverExpanded.value = false }
</script>

<style scoped>
.music-big-cover {
  position: absolute;
  left: 8px;
  bottom: 8px;
  width: calc(var(--music-sidebar-w) - 16px);
  height: calc(var(--music-sidebar-w) - 16px);
  z-index: 45;
  border-radius: var(--r-lg, 12px);
  overflow: hidden;
  box-shadow: 0 10px 34px rgba(0, 0, 0, 0.55);
  border: 1px solid var(--border);
}
.mbc-link { display: block; width: 100%; height: 100%; }
.mbc-img { width: 100%; height: 100%; }

/* Hover chrome: full-image + collapse, plus a gradient nameplate. */
.mbc-actions {
  position: absolute;
  top: 8px;
  right: 8px;
  display: flex;
  gap: 6px;
  opacity: 0;
  transition: opacity 0.15s ease;
}
.music-big-cover:hover .mbc-actions { opacity: 1; }
.mbc-action {
  width: 28px; height: 28px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  border: 0;
  color: #fff;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mbc-action:hover { background: var(--gold); color: var(--bg-0); }

.mbc-meta {
  position: absolute;
  left: 0; right: 0; bottom: 0;
  padding: 24px 12px 10px;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.85), transparent);
  pointer-events: none;
}
.mbc-title { font-size: 13px; font-weight: 700; color: #fff; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mbc-artist { font-size: 11px; color: rgba(255, 255, 255, 0.7); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.mbc-enter-active, .mbc-leave-active { transition: opacity 0.24s ease, transform 0.24s ease; }
.mbc-enter-from, .mbc-leave-to { opacity: 0; transform: translateY(12px) scale(0.96); }
</style>
