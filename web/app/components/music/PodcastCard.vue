<template>
  <NuxtLink
    :to="detailLink"
    class="pc-card card-tile"
  >
    <div class="pc-art" :class="{ 'pc-art-fallback': !podcast.artwork_url }">
      <img
        v-if="podcast.artwork_url"
        :src="podcast.artwork_url"
        :alt="podcast.title"
        loading="lazy"
        @error="imgError = true"
        v-show="!imgError"
      />
      <Icon v-if="!podcast.artwork_url || imgError" name="mic" :size="40" />
    </div>
    <div class="pc-meta">
      <div class="pc-title" :title="podcast.title">{{ podcast.title }}</div>
      <div v-if="podcast.author" class="pc-author">{{ podcast.author }}</div>
      <div v-if="(podcast.episode_count ?? 0) > 0" class="pc-count mono">
        {{ (podcast.episode_count ?? 0).toLocaleString() }} episodes
      </div>
    </div>
  </NuxtLink>
</template>

<script setup lang="ts">
interface Podcast {
  id?: number
  feed_url: string
  title: string
  author: string
  artwork_url: string
  episode_count?: number
}

const props = defineProps<{ podcast: Podcast }>()
const imgError = ref(false)

// The podcast detail page is keyed by URL-encoded feed URL — feed_url is
// the only stable identifier across PI search/trending and our own
// subscription rows (PI's numeric id only exists in their catalog).
const detailLink = computed(() => `/music/podcasts/feed?feed=${encodeURIComponent(props.podcast.feed_url)}`)
</script>

<style scoped>
.pc-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  text-decoration: none;
  color: inherit;
}
.pc-art {
  aspect-ratio: 1 / 1;
  border-radius: var(--r-md);
  background: var(--bg-3);
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  box-shadow: 0 8px 18px rgba(0, 0, 0, 0.35);
}
.pc-art img { width: 100%; height: 100%; object-fit: cover; }
.pc-art-fallback {
  background: linear-gradient(135deg, rgba(99, 102, 241, 0.18), rgba(99, 102, 241, 0.04));
}
.pc-meta { display: flex; flex-direction: column; gap: 2px; padding: 0 2px; }
.pc-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  line-height: 1.3;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.pc-author {
  font-size: 11px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pc-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); }
.mono { font-family: var(--font-mono); }
</style>
