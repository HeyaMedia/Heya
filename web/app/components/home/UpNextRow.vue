<template>
  <section v-if="items.length" class="content-row">
    <div class="section-row-head">
      <div>
        <h2 class="section-title-lg">Up Next</h2>
        <div class="row-subtitle">Pick up where you left off</div>
      </div>
      <div style="display: flex; align-items: center; gap: 10px">
        <button class="scroll-btn" @click="scrollBy(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" @click="scrollBy(1)"><Icon name="chevright" :size="16" /></button>
      </div>
    </div>
    <div class="row-scroll" ref="scrollEl">
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="un-tile"
        :style="{ width: '168px' }"
      >
        <!-- Poster: clicking plays the next episode directly. The MediaCard
             paints the overlay; we wrap the entire tile in a button so the
             whole card is the play target and the overlay info reflects what
             will start. -->
        <button
          class="un-poster"
          :aria-label="`Play ${item.title} ${item.episode_label}`"
          @click="$emit('play', item)"
        >
          <MediaCard
            :idx="i"
            :src="usePosterUrl(item.id)"
            :title="item.title"
            :subtitle="item.episode_label"
            aspect="2/3"
          />
          <div class="un-play-overlay">
            <div class="un-play-btn"><Icon name="play" :size="18" /></div>
          </div>
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
export interface UpNextItem {
  id: number
  title: string
  slug: string
  season_number: number
  episode_number: number
  episode_label: string
  play_file_id: number
  // Episode primary key — let the watch route surface "S01E03 · Episode
  // title" in the activity panel via entity_type=episode + entity_id.
  episode_id?: number
}

defineProps<{ items: UpNextItem[] }>()
defineEmits<{ play: [item: UpNextItem] }>()

const scrollEl = ref<HTMLElement>()

function scrollBy(dir: number) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * 600, behavior: 'smooth' })
}
</script>

<style scoped>
.content-row { margin-bottom: 40px; }
.row-subtitle {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 2px;
  letter-spacing: 0.04em;
}

.row-scroll {
  display: flex;
  gap: 18px;
  overflow-x: auto;
  overflow-y: hidden;
  scroll-snap-type: x mandatory;
  padding-bottom: 4px;
  scrollbar-width: none;
}
.row-scroll::-webkit-scrollbar { display: none; }
.row-scroll > * { scroll-snap-align: start; }

.scroll-btn {
  width: 32px; height: 32px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgba(255,255,255,0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.scroll-btn:hover { background: rgba(255,255,255,0.12); color: var(--fg-0); }

.un-tile { flex-shrink: 0; }

.un-poster {
  position: relative;
  display: block;
  width: 100%;
  padding: 0;
  border: 0;
  border-radius: var(--r-md);
  background: transparent;
  cursor: pointer;
  text-align: left;
}
.un-play-overlay {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35);
  opacity: 0;
  transition: opacity 0.15s;
  pointer-events: none;
  border-radius: var(--r-md);
  z-index: 4;
}
.un-poster:hover .un-play-overlay,
.un-poster:focus-visible .un-play-overlay { opacity: 1; }
.un-play-btn {
  width: 44px; height: 44px; border-radius: 50%;
  background: rgba(255,255,255,0.18);
  backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
  color: #fff;
}
</style>
