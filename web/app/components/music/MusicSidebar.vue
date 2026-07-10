<template>
  <aside class="music-sidebar scroll" :class="{ 'ms-cover-expanded': coverShown }">
    <!-- Primary nav -->
    <ul class="ms-nav">
      <li>
        <NuxtLink to="/music" class="ms-nav-item" :class="{ active: section === 'home' }">
          <Icon name="home" :size="20" />
          <span>Home</span>
        </NuxtLink>
      </li>

      <li>
        <NuxtLink to="/music/search" class="ms-nav-item" :class="{ active: section === 'search' }">
          <Icon name="search" :size="20" />
          <span>Search</span>
        </NuxtLink>
      </li>

      <!-- Library — the full catalog. Clicking the row opens the Library
           hub; the chevron toggles direct access to Artists / Albums / Songs. -->
      <li>
        <CollapsibleRoot v-model:open="libraryOpen">
          <div class="ms-group-row">
            <NuxtLink to="/music/library" class="ms-nav-item flex-grow" :class="{ active: libraryActive }">
              <Icon name="music" :size="20" />
              <span>Library</span>
            </NuxtLink>
            <CollapsibleTrigger class="ms-chev" :title="libraryOpen ? 'Collapse' : 'Expand'">
              <Icon name="chevright" :size="12" />
            </CollapsibleTrigger>
          </div>
          <CollapsibleContent class="ms-collapsible">
            <ul class="ms-sub">
              <li>
                <NuxtLink to="/music/artists" class="ms-sub-item" :class="{ active: section === 'artists' }">Artists</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/albums" class="ms-sub-item" :class="{ active: section === 'albums' }">Albums</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/songs" class="ms-sub-item" :class="{ active: section === 'songs' }">Songs</NuxtLink>
              </li>
            </ul>
          </CollapsibleContent>
        </CollapsibleRoot>
      </li>

      <!-- My Music — the user's saved + rated content + sound profile. -->
      <li>
        <CollapsibleRoot v-model:open="myMusicOpen">
          <div class="ms-group-row">
            <NuxtLink to="/music/my" class="ms-nav-item flex-grow" :class="{ active: myMusicActive }">
              <Icon name="user" :size="20" />
              <span>My Music</span>
            </NuxtLink>
            <CollapsibleTrigger class="ms-chev" :title="myMusicOpen ? 'Collapse' : 'Expand'">
              <Icon name="chevright" :size="12" />
            </CollapsibleTrigger>
          </div>
          <CollapsibleContent class="ms-collapsible">
            <ul class="ms-sub">
              <li>
                <NuxtLink to="/music/my/artists" class="ms-sub-item" :class="{ active: section === 'my-artists' }">Artists</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/my/albums" class="ms-sub-item" :class="{ active: section === 'my-albums' }">Albums</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/my/favorites" class="ms-sub-item" :class="{ active: section === 'my-favorites' }">My Favorites</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/stats" class="ms-sub-item" :class="{ active: section === 'stats' }">My Sound</NuxtLink>
              </li>
            </ul>
          </CollapsibleContent>
        </CollapsibleRoot>
      </li>

      <!-- Stations — replaces Browse. Hub aggregates auto-mixes, custom
           stations (Library Radio, Deep Cuts, Time Travel, Random Album
           Radio), the mix builder, and the mood/genre/tempo browse. -->
      <li>
        <CollapsibleRoot v-model:open="stationsOpen">
          <div class="ms-group-row">
            <NuxtLink to="/music/stations" class="ms-nav-item flex-grow" :class="{ active: stationsActive }">
              <Icon name="compass" :size="20" />
              <span>Stations</span>
            </NuxtLink>
            <CollapsibleTrigger class="ms-chev" :title="stationsOpen ? 'Collapse' : 'Expand'">
              <Icon name="chevright" :size="12" />
            </CollapsibleTrigger>
          </div>
          <CollapsibleContent class="ms-collapsible">
            <ul class="ms-sub">
              <li>
                <NuxtLink to="/music/stations/mixes" class="ms-sub-item" :class="{ active: section === 'stations-mixes' }">Mixes</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/stations/builder" class="ms-sub-item" :class="{ active: section === 'stations-builder' }">Mix Builder</NuxtLink>
              </li>
              <li>
                <NuxtLink to="/music/browse" class="ms-sub-item" :class="{ active: section?.startsWith('browse') }">Moods · Genres · Tempo</NuxtLink>
              </li>
            </ul>
          </CollapsibleContent>
        </CollapsibleRoot>
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
    </ul>

    <!-- Create Playlist CTA -->
    <button class="ms-create" type="button" @click="$emit('create-playlist')">
      <span class="ms-create-badge"><Icon name="plus" :size="12" /></span>
      <span>Create Playlist</span>
    </button>

    <!-- Playlist list — Loved Songs pinned as the first "system" playlist,
         followed by user-created ones. Visually unified: same row shape, just
         a gold heart tile instead of a cover image. -->
    <div class="ms-divider" />
    <div class="ms-section-label">Playlists</div>
    <ul class="ms-playlists">
      <li>
        <NuxtLink to="/music/loved" class="ms-pl-item" :class="{ active: section === 'loved' }">
          <div class="ms-pl-cover ms-pl-cover-loved">
            <Icon name="star" :size="20" weight="fill" />
          </div>
          <div class="ms-pl-meta">
            <div class="ms-pl-name">Loved Songs</div>
            <div class="ms-pl-count">Anything you've rated</div>
          </div>
        </NuxtLink>
      </li>
      <li v-for="(pl, i) in playlists" :key="pl.id">
        <NuxtLink
          :to="`/music/playlist/${pl.id}`"
          class="ms-pl-item"
          :class="{ active: section === 'playlist-' + pl.id, 'drop-target': !isCoarse && dragDrop.dragState.overPlaylistId === pl.id }"
          @dragover="!isCoarse && dragDrop.onPlaylistDragOver($event, pl.id)"
          @dragleave="!isCoarse && dragDrop.onPlaylistDragLeave()"
          @drop="!isCoarse && dragDrop.onPlaylistDrop($event, pl.id, pl.name)"
        >
          <Poster :idx="i" :src="pl.cover_path || null" aspect="1/1" class="ms-pl-cover" :width="80" />
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
import { CollapsibleRoot, CollapsibleTrigger, CollapsibleContent } from 'reka-ui'

const props = defineProps<{
  section: string
  playlists: Array<{ id: number; name: string; count: number; cover_path?: string }>
}>()

defineEmits<{ 'create-playlist': [] }>()

// When the now-playing cover folds out (MusicBigCover), it overlaps the bottom
// of the sidebar. Reserve space so the menu scrolls/sits above it rather than
// hiding behind it. Gate on a track being present too — the cover only renders
// when coverExpanded AND a track is loaded, so without this the sidebar would
// stay shrunk (empty gap, no cover) after playback stops with the mode still on.
const { currentTrack } = usePlayer()
const coverExpanded = useState('music_cover_expanded', () => false)
const coverShown = computed(() => coverExpanded.value && !!currentTrack.value)

// Desktop drag-and-drop onto playlist rows — touch keeps the long-press
// context menu as the only "add to playlist" path (docs/ui.md responsive
// conventions: gate on pointer coarseness, not viewport width).
const { isCoarse } = useViewport()
const dragDrop = useMusicDragDrop()

// Auto-open the group that contains the active section. User can still
// collapse manually after — these are open by default if the user happens
// to be inside the group.
const libraryOpen = ref(true)
const myMusicOpen = ref(true)
const stationsOpen = ref(true)

const librarySections = ['library', 'artists', 'albums', 'songs']
const myMusicSections = ['my', 'my-artists', 'my-albums', 'my-favorites', 'stats']
const stationsSections = ['stations', 'stations-mixes', 'stations-builder']

const libraryActive = computed(() => librarySections.includes(props.section))
const myMusicActive = computed(() => myMusicSections.includes(props.section))
const stationsActive = computed(() =>
  stationsSections.includes(props.section) || props.section?.startsWith('browse'),
)

watch(() => props.section, (s) => {
  if (librarySections.includes(s)) libraryOpen.value = true
  if (myMusicSections.includes(s)) myMusicOpen.value = true
  if (stationsSections.includes(s) || s?.startsWith('browse')) stationsOpen.value = true
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
  transition: height 0.28s ease;
}
/* The fold-out cover (bottom:8px, height: sidebar-w − 16px → top at sidebar-w −
   8px from the shell bottom) rises into the sidebar's lower area. Shrink the
   sidebar's height so its whole scroll region — track and content — ends above
   the cover; the menu (playlists included) can then scroll into the reduced
   viewport instead of hiding behind the art. Flat calc (no nested parens, which
   silently invalidated the declaration and left height at 100%). +16px of
   breathing room above the cover. */
.ms-cover-expanded {
  height: calc(100% - var(--music-sidebar-w) + var(--playbar-h) - 12px);
}
/* The playlists live in their OWN bottom-docked scroll region (.ms-playlists is
   flex:1 + overflow-y:auto), so shrinking the outer sidebar never moved them —
   that flex:1 child just kept filling down behind the cover. When the cover is
   out, collapse that nested scroll into the main sidebar scroll: pin the direct
   children to natural height (no flex-shrink) and turn .ms-playlists into a
   plain block (no grow, no inner scroll). Now the whole sidebar scrolls as one
   reduced region that ends above the cover, so every playlist is reachable. */
.ms-cover-expanded > * { flex-shrink: 0; }
.ms-cover-expanded .ms-playlists { flex: 0 0 auto; overflow: visible; min-height: auto; }

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
.ms-nav-item:hover { background: rgb(var(--ink) / 0.04); color: var(--fg-0); }
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
.ms-chev:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.04); }
.ms-chev :deep(svg) { transition: transform 0.2s; }
/* Rotate the chevron when the collapsible underneath is open. Reka stamps
   data-state on the CollapsibleTrigger button, so a CSS rule is enough —
   no manual :style binding needed. */
.ms-chev[data-state="open"] :deep(svg) { transform: rotate(90deg); }

/* Smooth open/close: reka exposes the resolved content height as a CSS
   var on the CollapsibleContent element, so we can transition height
   without measuring in JS. Without this the content snaps in/out. */
.ms-collapsible {
  overflow: hidden;
}
.ms-collapsible[data-state="open"] {
  animation: ms-collapse-down 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
.ms-collapsible[data-state="closed"] {
  animation: ms-collapse-up 0.18s cubic-bezier(0.4, 0, 1, 1);
}
@keyframes ms-collapse-down {
  from { height: 0; opacity: 0; }
  to   { height: var(--reka-collapsible-content-height); opacity: 1; }
}
@keyframes ms-collapse-up {
  from { height: var(--reka-collapsible-content-height); opacity: 1; }
  to   { height: 0; opacity: 0; }
}

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
.ms-sub-item:hover { background: rgb(var(--ink) / 0.04); color: var(--fg-0); }
.ms-sub-item.active { color: var(--gold); background: var(--gold-soft); }

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
.ms-create:hover { background: rgb(var(--ink) / 0.04); color: var(--fg-0); }
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
.ms-pl-item:hover { background: rgb(var(--ink) / 0.04); }
.ms-pl-item.active { background: var(--gold-soft); }
.ms-pl-cover {
  width: 40px;
  height: 40px;
  border-radius: var(--r-sm);
  flex-shrink: 0;
}
.ms-pl-cover-loved {
  display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, var(--gold), color-mix(in oklab, var(--gold) 60%, #c8501c));
  color: #fff; /* icon on the gold gradient tile — stays literal */
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

/* Drag-and-drop target state — matches LibrarySidebar's gold dashed
   treatment for movie/TV "add to list" drops. */
.ms-pl-item.drop-target {
  background: color-mix(in srgb, var(--gold) 10%, transparent);
  border: 1px dashed var(--gold);
  border-radius: var(--r-sm);
}
</style>
