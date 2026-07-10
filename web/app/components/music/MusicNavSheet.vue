<!--
  MusicNavSheet — flat nav list extracted from music.vue's phone "Browse"
  AppSheet so the same markup can also back the compact-band (720.02-1200px)
  left drawer (see music.vue).

  Built as a flat list of MusicSidebar's own links rather than reusing
  <MusicSidebar/> verbatim: that component is a fixed 256px `<aside>` with
  its own collapsible groups and a `coverShown` state tied to the
  now-playing fold-out cover — overriding all of that from an unscoped
  stylesheet (required since AppSheet content is portaled) fought the
  component's own scoped CSS harder than just re-listing its ~20 links
  flatly here (docs/responsive-plan.md W1c). Tapping any link (or the
  Create Playlist row) emits `navigate` so the host AppSheet can close
  itself — works the same whether the host is the phone Browse sheet or the
  compact-band drawer.
-->
<template>
  <nav class="mnav">
    <NuxtLink to="/music" class="mnav-item" :class="{ active: currentSection === 'home' }" @click="emit('navigate')">
      <Icon name="home" :size="18" /> <span>Home</span>
    </NuxtLink>
    <NuxtLink to="/music/search" class="mnav-item" :class="{ active: currentSection === 'search' }" @click="emit('navigate')">
      <Icon name="search" :size="18" /> <span>Search</span>
    </NuxtLink>

    <div class="mnav-group-label">Library</div>
    <NuxtLink to="/music/library" class="mnav-item" :class="{ active: currentSection === 'library' }" @click="emit('navigate')">
      <Icon name="music" :size="18" /> <span>Overview</span>
    </NuxtLink>
    <NuxtLink to="/music/artists" class="mnav-item mnav-sub" :class="{ active: currentSection === 'artists' }" @click="emit('navigate')">Artists</NuxtLink>
    <NuxtLink to="/music/albums" class="mnav-item mnav-sub" :class="{ active: currentSection === 'albums' }" @click="emit('navigate')">Albums</NuxtLink>
    <NuxtLink to="/music/songs" class="mnav-item mnav-sub" :class="{ active: currentSection === 'songs' }" @click="emit('navigate')">Songs</NuxtLink>

    <div class="mnav-group-label">My Music</div>
    <NuxtLink to="/music/my" class="mnav-item" :class="{ active: currentSection === 'my' }" @click="emit('navigate')">
      <Icon name="user" :size="18" /> <span>Overview</span>
    </NuxtLink>
    <NuxtLink to="/music/my/artists" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-artists' }" @click="emit('navigate')">Artists</NuxtLink>
    <NuxtLink to="/music/my/albums" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-albums' }" @click="emit('navigate')">Albums</NuxtLink>
    <NuxtLink to="/music/my/favorites" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-favorites' }" @click="emit('navigate')">My Favorites</NuxtLink>
    <NuxtLink to="/music/stats" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stats' }" @click="emit('navigate')">My Sound</NuxtLink>

    <div class="mnav-group-label">Stations</div>
    <NuxtLink to="/music/stations" class="mnav-item" :class="{ active: currentSection === 'stations' }" @click="emit('navigate')">
      <Icon name="compass" :size="18" /> <span>Overview</span>
    </NuxtLink>
    <NuxtLink to="/music/stations/mixes" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stations-mixes' }" @click="emit('navigate')">Mixes</NuxtLink>
    <NuxtLink to="/music/stations/builder" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stations-builder' }" @click="emit('navigate')">Mix Builder</NuxtLink>
    <NuxtLink to="/music/browse" class="mnav-item mnav-sub" :class="{ active: currentSection?.startsWith('browse') }" @click="emit('navigate')">Moods · Genres · Tempo</NuxtLink>

    <NuxtLink to="/music/podcasts" class="mnav-item" :class="{ active: currentSection === 'podcasts' }" @click="emit('navigate')">
      <Icon name="mic" :size="18" /> <span>Podcasts</span>
    </NuxtLink>
    <NuxtLink to="/music/radio" class="mnav-item" :class="{ active: currentSection === 'radio' }" @click="emit('navigate')">
      <Icon name="radio" :size="18" /> <span>Internet Radio</span>
    </NuxtLink>

    <div class="mnav-group-label">Playlists</div>
    <NuxtLink to="/music/loved" class="mnav-item" :class="{ active: currentSection === 'loved' }" @click="emit('navigate')">
      <Icon name="star" :size="18" /> <span>Loved Songs</span>
    </NuxtLink>
    <NuxtLink
      v-for="pl in playlists"
      :key="pl.id"
      :to="`/music/playlist/${pl.id}`"
      class="mnav-item mnav-sub"
      :class="{ active: currentSection === 'playlist-' + pl.id }"
      @click="emit('navigate')"
    >{{ pl.name }}</NuxtLink>
    <button type="button" class="mnav-item mnav-create" @click="emit('navigate'); emit('create-playlist')">
      <Icon name="plus" :size="18" /> <span>Create Playlist</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
defineProps<{
  currentSection: string
  playlists: Array<{ id: number; name: string; count?: number; cover_path?: string }>
}>()

const emit = defineEmits<{
  navigate: []
  'create-playlist': []
}>()
</script>

<!--
  The host AppSheet's content is portaled to <body> (docs/ui.md gotcha #2 —
  same reason NowPlayingSheet/QueuePane keep their body styles unscoped), so
  `.mnav-*` lives in its own unscoped block rather than a scoped one.
-->
<style>
.mnav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mnav-group-label {
  padding: 16px 10px 4px;
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.mnav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 44px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: transparent;
  border: 0;
  color: var(--fg-1);
  font-size: 15px;
  font-weight: 500;
  text-align: left;
  text-decoration: none;
  cursor: pointer;
}
.mnav-item:active { background: rgb(var(--ink) / 0.06); }
.mnav-item.active { color: var(--gold); background: var(--gold-soft); }
.mnav-sub {
  margin-left: 28px;
  width: calc(100% - 28px);
  min-height: 40px;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-2);
}
.mnav-sub.active { color: var(--gold); }
.mnav-create { margin-top: 10px; color: var(--fg-2); }
</style>
