<template>
  <section class="content-row">
    <div class="section-row-head">
      <div>
        <h2 class="section-title-lg">{{ title }}</h2>
        <div v-if="subtitle" class="row-subtitle">{{ subtitle }}</div>
      </div>
      <div style="display: flex; align-items: center; gap: 10px">
        <span v-if="more" class="more" @click="$emit('more')">{{ more }}</span>
        <button class="scroll-btn" @click="scrollBy(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" @click="scrollBy(1)"><Icon name="chevright" :size="16" /></button>
      </div>
    </div>
    <div class="row-scroll" ref="scrollEl">
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="card-tile"
        :style="{ width: `${tileWidth || 168}px`, flexShrink: 0 }"
        @click="$emit('tile', item)"
      >
        <Poster
          :idx="i"
          :src="usePosterUrl(item.id)"
          :title="item.title"
          :aspect="aspect || '2/3'"
        />
        <div class="grid-tile-meta">
          <div class="grid-tile-title">{{ item.title }}</div>
          <div class="grid-tile-sub">{{ item.year || item.sub }}</div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

defineProps<{
  title: string
  subtitle?: string
  items: (MediaItem & { sub?: string })[]
  tileWidth?: number
  aspect?: string
  more?: string
}>()

defineEmits<{
  tile: [item: MediaItem]
  more: []
}>()

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
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255,255,255,0.06);
  border: 1px solid var(--border);
  color: var(--fg-2);
  transition: all 0.15s;
}
.scroll-btn:hover {
  background: rgba(255,255,255,0.12);
  color: var(--fg-0);
}
</style>
