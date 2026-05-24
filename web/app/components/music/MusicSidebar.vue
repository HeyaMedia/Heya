<template>
  <aside class="music-sidebar scroll">
    <!-- Primary nav -->
    <ul class="ms-nav">
      <li>
        <NuxtLink to="/music" class="ms-nav-item" :class="{ active: section === 'home' }">
          <Icon name="home" :size="20" />
          <span>Home</span>
        </NuxtLink>
      </li>

      <!-- Library group: full browse across every music library -->
      <li>
        <div class="ms-group-row">
          <NuxtLink to="/music/artists" class="ms-nav-item flex-grow" :class="{ active: libraryActive }">
            <Icon name="music" :size="20" />
            <span>Library</span>
          </NuxtLink>
          <button class="ms-chev" @click="libraryOpen = !libraryOpen" :title="libraryOpen ? 'Collapse' : 'Expand'">
            <Icon name="chevright" :size="12" :style="libraryOpen ? { transform: 'rotate(90deg)' } : undefined" />
          </button>
        </div>
        <ul v-if="libraryOpen" class="ms-sub">
          <li>
            <NuxtLink to="/music/artists" class="ms-sub-item" :class="{ active: section === 'artists' }">Artists</NuxtLink>
          </li>
          <li>
            <NuxtLink to="/music/albums" class="ms-sub-item" :class="{ active: section === 'albums' }">Albums</NuxtLink>
          </li>
        </ul>
      </li>

      <!-- My Media group: the user's favorites -->
      <li>
        <div class="ms-group-row">
          <NuxtLink to="/music/my/artists" class="ms-nav-item flex-grow" :class="{ active: myMediaActive }">
            <Icon name="heart" :size="20" />
            <span>My Media</span>
          </NuxtLink>
          <button class="ms-chev" @click="myMediaOpen = !myMediaOpen" :title="myMediaOpen ? 'Collapse' : 'Expand'">
            <Icon name="chevright" :size="12" :style="myMediaOpen ? { transform: 'rotate(90deg)' } : undefined" />
          </button>
        </div>
        <ul v-if="myMediaOpen" class="ms-sub">
          <li>
            <NuxtLink to="/music/my/artists" class="ms-sub-item" :class="{ active: section === 'my-artists' }">Artists</NuxtLink>
          </li>
          <li>
            <NuxtLink to="/music/my/albums" class="ms-sub-item" :class="{ active: section === 'my-albums' }">Albums</NuxtLink>
          </li>
          <li>
            <NuxtLink to="/music/loved" class="ms-sub-item" :class="{ active: section === 'loved' }">
              <Icon name="heartfill" :size="11" class="ms-sub-icon" />
              Loved Songs
            </NuxtLink>
          </li>
        </ul>
      </li>

      <li>
        <NuxtLink to="/music/podcasts" class="ms-nav-item" :class="{ active: section === 'podcasts' }">
          <Icon name="mic" :size="20" />
          <span>Podcasts</span>
        </NuxtLink>
      </li>
      <li>
        <NuxtLink to="/music/radio" class="ms-nav-item" :class="{ active: section === 'radio' }">
          <Icon name="radio" :size="20" />
          <span>Internet Radio</span>
        </NuxtLink>
      </li>
      <li>
        <NuxtLink to="/music/search" class="ms-nav-item" :class="{ active: section === 'search' }">
          <Icon name="search" :size="20" />
          <span>Vibe Search</span>
        </NuxtLink>
      </li>
    </ul>

    <!-- Create Playlist CTA -->
    <button class="ms-create" type="button" @click="$emit('create-playlist')">
      <span class="ms-create-badge"><Icon name="plus" :size="12" /></span>
      <span>Create Playlist</span>
    </button>

    <!-- Playlist list -->
    <div class="ms-divider" />
    <div class="ms-section-label">Playlists</div>
    <ul class="ms-playlists">
      <li v-for="(pl, i) in playlists" :key="pl.id">
        <NuxtLink :to="`/music/playlist/${pl.id}`" class="ms-pl-item" :class="{ active: section === 'playlist-' + pl.id }">
          <Poster :idx="i" :src="pl.cover_path || null" aspect="1/1" class="ms-pl-cover" />
          <div class="ms-pl-meta">
            <div class="ms-pl-name">{{ pl.name }}</div>
            <div class="ms-pl-count">{{ pl.count }} tracks</div>
          </div>
        </NuxtLink>
      </li>
      <li v-if="!playlists.length" class="ms-pl-empty">
        No playlists yet
      </li>
    </ul>
  </aside>
</template>

<script setup lang="ts">
const props = defineProps<{
  section: string
  playlists: Array<{ id: number; name: string; count: number; cover_path?: string }>
}>()

defineEmits<{ 'create-playlist': [] }>()

// Auto-open the group that contains the active section. User can still
// collapse manually after — these are open by default if the user happens
// to be inside the group.
const libraryOpen = ref(true)
const myMediaOpen = ref(true)

const libraryActive = computed(() => ['artists', 'albums'].includes(props.section))
const myMediaActive = computed(() => ['my-artists', 'my-albums', 'loved'].includes(props.section))

watch(() => props.section, (s) => {
  if (['artists', 'albums'].includes(s)) libraryOpen.value = true
  if (['my-artists', 'my-albums', 'loved'].includes(s)) myMediaOpen.value = true
})
</script>

<style scoped>
.music-sidebar {
  width: var(--music-sidebar-w);
  flex-shrink: 0;
  background: var(--bg-1);
  border-right: 1px solid var(--border);
  padding: 16px 8px 12px;
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 4px;
}

.ms-nav { display: flex; flex-direction: column; gap: 2px; }

.ms-nav-item {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  padding: 0 12px;
  height: 40px;
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--fg-2);
  font-size: 14px;
  font-weight: 600;
  text-align: left;
  cursor: pointer;
  position: relative;
  text-decoration: none;
  transition: color 0.15s, background 0.15s;
}
.ms-nav-item:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }
.ms-nav-item.active {
  color: var(--gold);
  background: var(--gold-soft);
}
.ms-nav-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}

/* Group row: nav item + chevron button beside it. */
.ms-group-row { display: flex; align-items: center; gap: 2px; }
.flex-grow { flex: 1; }
.ms-chev {
  width: 28px;
  height: 36px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  border-radius: var(--r-sm);
  transition: color 0.15s, background 0.15s;
}
.ms-chev:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }
.ms-chev :deep(svg) { transition: transform 0.2s; }

.ms-sub {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 2px 0 4px 30px;
}
.ms-sub-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  height: 32px;
  border-radius: var(--r-sm);
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-2);
  text-decoration: none;
  transition: color 0.15s, background 0.15s;
}
.ms-sub-item:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }
.ms-sub-item.active { color: var(--gold); background: var(--gold-soft); }
.ms-sub-icon { color: var(--gold); }

.ms-create {
  margin-top: 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  height: 40px;
  padding: 0 12px;
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--fg-2);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.ms-create:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }
.ms-create-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  color: var(--gold);
}
.ms-create:hover .ms-create-badge { background: var(--gold); color: var(--bg-0); }

.ms-divider {
  height: 1px;
  background: var(--border);
  margin: 12px 12px 8px;
}
.ms-section-label {
  padding: 0 14px 6px;
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}

.ms-playlists {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-height: 0;
}
.ms-pl-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  color: var(--fg-1);
  text-decoration: none;
  cursor: pointer;
  transition: background 0.15s;
}
.ms-pl-item:hover { background: rgba(255,255,255,0.04); }
.ms-pl-item.active { background: var(--gold-soft); }
.ms-pl-cover {
  width: 40px;
  height: 40px;
  border-radius: var(--r-sm);
  flex-shrink: 0;
}
.ms-pl-meta { flex: 1; min-width: 0; }
.ms-pl-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-pl-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 1px;
}
.ms-pl-empty {
  padding: 16px 14px;
  font-size: 12px;
  color: var(--fg-3);
  text-align: center;
}
</style>
