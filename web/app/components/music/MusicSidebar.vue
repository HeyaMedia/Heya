<template>
  <aside class="music-sidebar scroll">
    <div class="ms-section">
      <div class="lib-item" :class="{ active: section === 'home' }" @click="$emit('nav', 'home')">
        <Icon name="home" :size="16" />
        <span>Home</span>
      </div>
    </div>

    <div class="ms-section">
      <div class="lib-item" @click="myMediaOpen = !myMediaOpen" style="justify-content: space-between">
        <div style="display: flex; align-items: center; gap: 10px">
          <Icon name="music" :size="16" />
          <span>My Media</span>
        </div>
        <Icon :name="myMediaOpen ? 'chevdown' : 'chevright'" :size="12" />
      </div>
      <template v-if="myMediaOpen">
        <div class="lib-item sub" :class="{ active: section === 'playlists' }" @click="$emit('nav', 'playlists')">Playlists</div>
        <div class="lib-item sub" :class="{ active: section === 'artists' }" @click="$emit('nav', 'artists')">Artists</div>
        <div class="lib-item sub" :class="{ active: section === 'albums' }" @click="$emit('nav', 'albums')">Albums</div>
        <div class="lib-item sub" :class="{ active: section === 'songs' }" @click="$emit('nav', 'songs')">Songs</div>
        <div class="lib-item sub" :class="{ active: section === 'loved' }" @click="$emit('nav', 'loved')">
          <Icon name="heart" :size="14" style="color: var(--gold)" />
          Loved
        </div>
      </template>
    </div>

    <div class="ms-section">
      <div class="lib-item" :class="{ active: section === 'podcasts' }" @click="$emit('nav', 'podcasts')">
        <Icon name="mic" :size="16" />
        <span>Podcasts</span>
      </div>
      <div class="lib-item" :class="{ active: section === 'radio' }" @click="$emit('nav', 'radio')">
        <Icon name="radio" :size="16" />
        <span>Internet Radio</span>
      </div>
    </div>

    <div class="ms-divider" />

    <div class="ms-section" style="flex: 1">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 8px; font-size: 11px">Playlists</div>
      <div
        v-for="(pl, i) in playlists"
        :key="pl.id"
        class="lib-item"
        :class="{ active: section === 'playlist-' + pl.id }"
        @click="$emit('nav', 'playlist-' + pl.id)"
      >
        <div class="pl-cover">
          <Poster :idx="i" aspect="1/1" style="width: 32px; height: 32px; border-radius: 4px" />
        </div>
        <div style="flex: 1; min-width: 0">
          <div style="font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ pl.name }}</div>
          <div style="font-size: 10px; color: var(--fg-3); font-family: var(--font-mono)">{{ pl.count }} tracks</div>
        </div>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
defineProps<{
  section: string
  playlists: Array<{ id: number; name: string; count: number }>
}>()

defineEmits<{ nav: [section: string] }>()

const myMediaOpen = ref(true)
</script>

<style scoped>
.music-sidebar {
  width: var(--music-sidebar-w);
  flex-shrink: 0;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  padding: 14px 8px;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.ms-section { margin-bottom: 4px; }
.ms-divider { height: 1px; background: var(--border); margin: 8px 14px; }
.lib-item.sub { padding-left: 42px; font-size: 13px; }
</style>
