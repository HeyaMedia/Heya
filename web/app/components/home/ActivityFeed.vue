<template>
  <section v-if="items?.length" class="activity-feed">
    <div class="section-row-head">
      <h2 class="section-title-lg">Recent Activity</h2>
    </div>
    <div class="feed-list">
      <div
        v-for="item in items"
        :key="`${item.type}-${item.timestamp}-${item.media_id}`"
        class="feed-item"
        :class="{ clickable: item.slug }"
        @click="item.slug && navigateTo(mediaPath(item))"
      >
        <div class="feed-icon" :class="item.type">
          <Icon :name="iconFor(item.type)" :size="14" />
        </div>
        <NuxtImg
          v-if="item.image_url"
          :src="item.image_url"
          :width="120"
          :quality="80"
          class="feed-poster"
          @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
        />
        <div class="feed-text">
          <div class="feed-title">{{ item.title }}</div>
          <div class="feed-meta">
            <span class="feed-type-label">{{ labelFor(item.type) }}</span>
            <span class="feed-dot">&middot;</span>
            <span>{{ timeAgo(item.timestamp) }}</span>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
interface ActivityItem {
  type: string
  timestamp: string
  title: string
  subtitle?: string
  media_id?: number
  media_type?: string
  slug?: string
  image_url?: string
}

const items = ref<ActivityItem[]>([])

function iconFor(type: string) {
  switch (type) {
    case 'media_added': return 'plus'
    case 'scan_completed': return 'refresh'
    default: return 'info'
  }
}

function labelFor(type: string) {
  switch (type) {
    case 'media_added': return 'Added'
    case 'scan_completed': return 'Scan'
    default: return ''
  }
}

function mediaPath(item: ActivityItem) {
  if (!item.slug || !item.media_type) return '/'
  const base = item.media_type === 'movie' ? '/movies' : item.media_type === 'tv' ? '/tv' : `/${item.media_type}s`
  return `${base}/${item.slug}`
}

// timeAgo comes from useFormat.ts (auto-imported).

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    // Guard against a null payload — a nil slice would crash the v-if.
    items.value = (await $heya('/api/activity') as ActivityItem[] | null) ?? []
  } catch { /* empty */ }
})
</script>

<style scoped>
.activity-feed { margin-bottom: 40px; }

.feed-list {
  display: flex; flex-direction: column; gap: 2px;
  background: var(--bg-2); border: 1px solid var(--border); border-radius: 12px;
  overflow: hidden; max-height: 360px; overflow-y: auto;
  scrollbar-width: thin; scrollbar-color: var(--border) transparent;
}

.feed-item {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 16px; transition: background 0.12s;
}
.feed-item.clickable { cursor: pointer; }
.feed-item.clickable:hover { background: rgba(255,255,255,0.03); }

.feed-icon {
  width: 28px; height: 28px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.feed-icon.media_added { background: rgba(74,180,130,0.12); color: rgb(74,180,130); }
.feed-icon.scan_completed { background: rgba(100,150,230,0.12); color: rgb(100,150,230); }

.feed-poster {
  width: 32px; height: 48px; border-radius: 4px; object-fit: cover; flex-shrink: 0;
  background: var(--bg-3);
}

.feed-text { flex: 1; min-width: 0; }
.feed-title { font-size: 13px; font-weight: 500; color: var(--fg-0); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.feed-meta { font-size: 11px; color: var(--fg-3); display: flex; align-items: center; gap: 6px; margin-top: 2px; }
.feed-type-label { font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; font-size: 10px; }
.feed-dot { color: var(--fg-3); }
</style>
