<template>
  <div v-if="loading" class="m-loading page-pad">Loading…</div>
  <div v-else-if="!artist" class="m-empty page-pad">Artist not found.</div>
  <div v-else class="artist-page">
    <!-- Hero (Plexify style): full-bleed backdrop with a circular poster
         on the left, bio + tags inline beside the name, and a stats line
         with rating below. Floating round actions on the right. -->
    <section class="hero" :class="{ 'ambient-extended': ambientEnabled }">
      <div class="hero-backdrop" :style="backdropStyle" />
      <div class="hero-fade" />
      <!-- "Around the web" dropdown — ONE instance serving both layouts:
           desktop pins it to the hero's top-right (absolute), phone leaves
           it in flow, where the hero's column layout renders it as a
           full-width bar across the top. -->
      <div v-if="linkGroups.length" class="hero-atw">
        <AppMenu :width="320" trigger-class="atw-trigger" trigger-title="Around the web">
          <template #trigger>
            <Icon name="link" :size="12" />
            <span>Around the web</span>
            <span class="atw-count">{{ linkTotal }}</span>
            <Icon name="chevdown" :size="10" />
          </template>
          <div class="atw-scroll">
            <template v-for="group in linkGroups" :key="group.label">
              <div class="surface-section-label">{{ group.label }}</div>
              <DropdownMenuItem
                v-for="(l, i) in group.links"
                :key="`${group.label}-${i}`"
                class="surface-item atw-item"
                as-child
              >
                <a :href="l.url" target="_blank" rel="noopener">
                  <span class="atw-host">{{ l.label }}</span>
                  <span v-if="l.sub" class="atw-type">{{ l.sub }}</span>
                </a>
              </DropdownMenuItem>
            </template>
          </div>
        </AppMenu>
      </div>
      <!-- Desktop identity block: the logotype (or name) + aliases float
           on the hero's right flank, clearing the left column for bio and
           stats. Phone keeps the title in the meta flow below. -->
      <div v-if="!brandInFlow" class="hero-brand">
        <h1 v-if="logoUrl && !logoFailed" class="hero-title hero-title-logo">
          <NuxtImg
            :src="logoUrl"
            :alt="artist.name"
            class="hero-logo"
            :width="640"
            @error="logoFailed = true"
          />
        </h1>
        <h1 v-else class="hero-title">{{ artist.name }}</h1>
        <div
          v-if="heroAliases"
          class="hero-aka"
          :title="`Also known as: ${artist.aliases!.join(', ')}`"
        >
          <span class="hero-aka-label">a.k.a.</span> {{ heroAliases }}
        </div>
      </div>
      <div class="hero-content">
        <div class="hero-left">
          <Poster :idx="artist.id" :src="artistPosterUrl" aspect="1/1" class="hero-poster" :width="320" />
        </div>
        <div class="hero-meta">
          <template v-if="brandInFlow">
            <h1 v-if="logoUrl && !logoFailed" class="hero-title hero-title-logo">
              <NuxtImg
                :src="logoUrl"
                :alt="artist.name"
                class="hero-logo"
                :width="640"
                @error="logoFailed = true"
              />
            </h1>
            <h1 v-else class="hero-title">{{ artist.name }}</h1>
            <div
              v-if="heroAliases"
              class="hero-aka"
              :title="`Also known as: ${artist.aliases!.join(', ')}`"
            >
              <span class="hero-aka-label">a.k.a.</span> {{ heroAliases }}
            </div>
          </template>
          <div v-if="(artist.tags?.length ?? 0) > 0" class="tag-row">
            <NuxtLink
              v-for="tag in (artist.tags ?? []).slice(0, 8)"
              :key="tag"
              :to="`/music/browse/genre/${encodeURIComponent(tag)}`"
              class="tag-chip"
            >{{ tag }}</NuxtLink>
          </div>
          <p v-if="artist.biography" class="hero-bio" :class="{ collapsed: !bioOpen && artist.biography.length > 320 }">
            {{ artist.biography }}
          </p>
          <button v-if="artist.biography && artist.biography.length > 320" class="hero-bio-toggle" @click="bioOpen = !bioOpen">
            {{ bioOpen ? 'Less' : 'More' }}
          </button>
          <div class="hero-stats">
            <div class="hero-stats-stars" @click.stop>
              <StarRating
                :model-value="artistRatings.get(artist.id) ?? 0"
                size="sm"
                @update:model-value="(v) => onRateArtist(artist!.id, v)"
              />
            </div>
            <template v-if="(artist.listeners ?? 0) > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ formatBigInt(artist.listeners!) }} listeners</span>
            </template>
            <template v-if="(artist.playcount ?? 0) > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ formatBigInt(artist.playcount!) }} plays</span>
            </template>
            <template v-if="lifecycleLabel">
              <span class="stat-dot">·</span>
              <span class="stat">{{ artist.artist_type === 'Group' ? 'Active' : 'Born' }} {{ lifecycleLabel }}</span>
            </template>
            <template v-if="originLabel">
              <span class="stat-dot">·</span>
              <span class="stat">{{ originLabel }}</span>
            </template>
            <template v-if="totalAlbums > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ totalAlbums }} {{ totalAlbums === 1 ? 'release' : 'releases' }} · {{ totalTracks }} tracks</span>
            </template>
          </div>
          <ExternalLinks
            kind="artist"
            :external-ids="detail?.media_item?.external_ids ?? {}"
            class="hero-ext"
          />
        </div>
      </div>
      <!-- Floating round actions -->
      <div class="hero-floating-actions">
        <span v-if="!artistPlayable" class="hero-missing"><Icon name="trash" :size="13" /> Missing on disk</span>
        <button class="hero-round hero-round-primary" :style="heroToneStyle" :disabled="!artistPlayable" @click="playAll(false)" title="Play">
          <Icon name="play" :size="22" />
        </button>
        <button class="hero-round" :disabled="!artistPlayable" @click="playAll(true)" title="Shuffle">
          <Icon name="shuffle" :size="18" />
        </button>
        <button class="hero-round" :disabled="!artistPlayable" @click="addAllToQueue" title="Add to queue">
          <Icon name="plus" :size="18" />
        </button>
        <button
          class="hero-round"
          @click="startArtistRadio"
          :disabled="radio.starting.value || !artistPlayable"
          title="Start radio from this artist"
        >
          <Icon name="radio" :size="18" />
        </button>
        <button v-if="isAdmin" class="hero-round hero-edit" title="Edit Metadata" @click="showMetadataEditor = true">
          <Icon name="pencil" :size="17" />
        </button>
      </div>
    </section>

    <!-- Popular Tracks: Plexify-style numbered list with star + duration -->
    <section v-if="topTracks.length" class="top-tracks artist-section">
      <div class="section-row-head tt-head">
        <h2 class="section-title-lg">Popular Tracks</h2>
        <button class="pill-btn" :style="heroToneStyle" @click="playTopAll(false)" :disabled="!hasPlayableTopTracks">
          <Icon name="play" :size="13" /><span>Play</span>
        </button>
        <button class="pill-btn pill-btn-ghost" @click="playTopAll(true)" :disabled="!hasPlayableTopTracks">
          <Icon name="shuffle" :size="13" /><span>Shuffle</span>
        </button>
      </div>
      <ol class="tt-list">
        <!-- AppContextMenu is as-child (no wrapper element), so the <li>s
             stay direct children of the <ol>. Right-click on desktop,
             long-press on touch — and on phone the row also gets a visible
             ⋯ (ActionSheet) since the star widget is hidden there and the
             menu is the rating/queue path. -->
        <AppContextMenu
          v-for="(t, idx) in topTracks.slice(0, ttExpanded ? topTracks.length : 8)"
          :key="`tt-${t.local_track_id}-${idx}`"
          :items="ttMenuItems(t)"
        >
        <!-- role="button": reka wrappers pointer-capture taps on plain
             elements and retarget the click away from the row (same gotcha
             as the drawer rows — see LibrarySidebar) — button/a/[role=button]
             targets are exempt. Also honest a11y: the row IS a play button. -->
        <li
          class="tt-row"
          role="button"
          :class="{ 'tt-row-missing': !isTopTrackPlayable(t), 'tt-row-active': isTopTrackActive(t) }"
          :draggable="!isCoarse && !!t.local_track_id"
          @click="onTtRowTap(t)"
          @dragstart="t.local_track_id && onDragStart($event, { kind: 'track', track: { id: t.local_track_id, title: t.title } })"
          @dragend="onDragEnd"
        >
          <div class="tt-leader">
            <!-- Currently-playing row: equalizer bars stand in for the rank
                 (and suppress the hover-play, which would overlap them). -->
            <VuMeter v-if="isTopTrackActive(t)" :playing="playing" class="tt-vu" />
            <span v-else-if="isTopTrackPlayable(t)" class="tt-rank">{{ idx + 1 }}</span>
            <Icon v-else name="trash" :size="12" class="tt-missing-icon" :title="`${t.title} — missing on disk`" />
            <!-- .stop: on touch this button is opacity-0 but still hit-
                 testable; without it a tap here fires both this handler and
                 the row-tap handler (playTopTrack twice, racing). -->
            <button
              v-if="isTopTrackPlayable(t) && !isTopTrackActive(t)"
              class="tt-hover-play"
              type="button"
              @click.stop="playTopTrack(t)"
              :title="`Play ${t.title}`"
            >
              <Icon name="play" :size="12" />
            </button>
          </div>
          <div class="tt-meta">
            <!-- Row = play, words = navigate: both the song title and the
                 album name link to the album page (.stop so the row's own
                 play handler doesn't also fire). Titles without a local
                 album stay plain text. -->
            <NuxtLink
              v-if="t.local_album_slug"
              :to="`/music/artist/${route.params.slug}/${t.local_album_slug}`"
              class="tt-title tt-title-link"
              @click.stop
            >{{ t.title }}</NuxtLink>
            <span v-else class="tt-title">{{ t.title }}</span>
            <template v-if="t.local_album_title">
              <span class="tt-album-sep">·</span>
              <NuxtLink
                :to="`/music/artist/${route.params.slug}/${t.local_album_slug}`"
                class="tt-album"
                @click.stop
              >{{ t.local_album_title }}</NuxtLink>
            </template>
          </div>
          <div class="tt-stars" @click.stop>
            <StarRating
              :model-value="trackRatings.get(t.local_track_id!) ?? 0"
              size="sm"
              @update:model-value="(v) => onRateTrack(t.local_track_id!, v)"
            />
          </div>
          <div v-if="t.local_duration" class="tt-duration">{{ formatTime(t.local_duration) }}</div>
          <div v-else class="tt-duration" />
          <button
            type="button"
            class="tt-phone-more"
            aria-label="More actions"
            @click.stop="openTtSheet(t)"
          >
            <Icon name="more" :size="18" />
          </button>
        </li>
        </AppContextMenu>
      </ol>
      <button v-if="topTracks.length > 8" class="tt-more" @click="ttExpanded = !ttExpanded">
        {{ ttExpanded ? 'Show fewer' : `See all ${topTracks.length}` }}
      </button>
    </section>

    <!-- Band lifecycle: members of this group / groups this person plays
         in. Chips link to the member's own page (with portrait) when they
         are themselves a library artist — matched server-side by MBID or
         name (matchArtistMembersLocal). -->
    <section v-if="(artist.members?.length ?? 0) > 0" class="members artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Members</h2>
        <span class="more">{{ artist.members!.length }}</span>
      </div>
      <div class="member-grid">
        <component
          :is="m.local_slug ? 'NuxtLink' : 'div'"
          v-for="m in artist.members"
          :key="`mem-${m.name}`"
          :to="m.local_slug ? `/music/artist/${m.local_slug}` : undefined"
          class="member-chip"
          :class="{ 'member-linked': !!m.local_slug }"
        >
          <Poster v-if="m.local_slug" :idx="0" :src="`/api/media/${m.local_slug}/image/poster`" aspect="1/1" :width="80" class="member-avatar" />
          <div class="member-text">
            <div class="member-name">{{ m.name }}</div>
            <div v-if="m.begin_year || m.end_year" class="member-years">
              {{ m.begin_year || '?' }}–{{ m.end_year || 'present' }}
            </div>
          </div>
        </component>
      </div>
    </section>

    <section v-if="(artist.groups?.length ?? 0) > 0" class="members artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Member of</h2>
        <span class="more">{{ artist.groups!.length }}</span>
      </div>
      <div class="member-grid">
        <component
          :is="g.local_slug ? 'NuxtLink' : 'div'"
          v-for="g in artist.groups"
          :key="`grp-${g.name}`"
          :to="g.local_slug ? `/music/artist/${g.local_slug}` : undefined"
          class="member-chip"
          :class="{ 'member-linked': !!g.local_slug }"
        >
          <Poster v-if="g.local_slug" :idx="0" :src="`/api/media/${g.local_slug}/image/poster`" aspect="1/1" :width="80" class="member-avatar" />
          <div class="member-text">
            <div class="member-name">{{ g.name }}</div>
            <div v-if="g.begin_year || g.end_year" class="member-years">
              {{ g.begin_year || '?' }}–{{ g.end_year || 'present' }}
            </div>
          </div>
        </component>
      </div>
    </section>

    <!-- Discography by release kind -->
    <section
      v-for="group in groupedDiscography"
      :key="group.kind"
      class="discog artist-section"
    >
      <div class="section-row-head">
        <h2 class="section-title-lg">{{ group.label }}</h2>
        <span class="more">{{ group.albums.length }}</span>
      </div>
      <div class="discog-grid">
        <AppContextMenu
          v-for="album in group.albums"
          :key="album.id"
          :items="discogMenuItems(album)"
        >
          <NuxtLink
            :to="`/music/artist/${route.params.slug}/${album.slug}`"
            class="discog-tile card-tile"
            :class="{ 'discog-missing': !albumPlayable(album), 'discog-active': isAlbumActive(album) }"
            :draggable="!isCoarse"
            @dragstart="onDragStart($event, discogDragPayload(album))"
            @dragend="onDragEnd"
          >
            <div class="discog-art-wrap">
              <Poster :idx="album.id" :src="useAlbumCoverUrl(route.params.slug as string, album.slug)" aspect="1/1" class="discog-art" />
              <MediaMissingBadge v-if="!albumPlayable(album)" />
              <!-- Now-playing badge: this album has the currently-playing track. -->
              <div v-if="isAlbumActive(album)" class="discog-nowplaying"><VuMeter :playing="playing" /></div>
              <!-- span, not <button>: this tile is a NuxtLink (below), and a
                   real button nested inside an anchor is invalid
                   interactive-in-interactive HTML — see MusicCard.vue's
                   .mc-play for the same fix + reasoning. -->
              <span
                v-if="albumPlayable(album)"
                role="button"
                tabindex="0"
                class="discog-play"
                :style="discogPlayStyle(album)"
                aria-label="Play album"
                title="Play album"
                @click.stop.prevent="playAlbum(album, false)"
                @keydown.enter.stop.prevent="playAlbum(album, false)"
                @keydown.space.stop.prevent="playAlbum(album, false)"
              >
                <Icon name="play" :size="14" />
              </span>
            </div>
            <div class="discog-meta">
              <div class="discog-title">{{ album.title }}</div>
              <div class="discog-sub">
                {{ album.year || '—' }}
                <span v-if="album.tracks.length" class="dot">·</span>
                <span v-if="album.tracks.length">{{ album.tracks.length }} tracks</span>
              </div>
            </div>
          </NuxtLink>
        </AppContextMenu>
      </div>
    </section>

    <!-- Sonic similar — local pgvector centroids -->
    <section v-if="sonicSimilar.length" class="similar artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Sounds Like</h2>
        <span class="more">{{ sonicSimilar.length }}</span>
      </div>
      <div class="similar-row">
        <NuxtLink
          v-for="row in sonicSimilar"
          :key="row.id"
          :to="`/music/artist/${row.media_slug}`"
          class="similar-tile card-tile"
          :title="`${row.name} — cosine distance ${row.distance.toFixed(3)}`"
        >
          <Poster :idx="row.id" :src="usePosterUrl({ id: row.media_item_id, public_id: row.media_item_public_id })" aspect="1/1" :width="200" style="border-radius: 50%" />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">sonic match</div>
        </NuxtLink>
      </div>
    </section>

    <!-- Similar artists — Last.fm + ListenBrainz via heya.media. Gated by
         the same Appearance switch as movie/TV recommendations: with
         "show unavailable" off, only artists we can reliably link (in the
         library) render. Local rows use the library portrait — the
         upstream image URLs are mostly dead (Last.fm stopped shipping
         them), which left a row of placeholder stars. -->
    <section v-if="visibleSimilar.length" class="similar artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Similar Artists</h2>
        <span class="more">{{ visibleSimilar.length }}</span>
      </div>
      <div class="similar-row">
        <component
          :is="row.local_slug ? 'NuxtLink' : 'div'"
          v-for="(row, i) in visibleSimilar"
          :key="row.name + i"
          :to="row.local_slug ? `/music/artist/${row.local_slug}` : undefined"
          class="similar-tile card-tile"
          :class="{ 'similar-external': !row.local_slug }"
          :title="row.local_slug ? `Open ${row.name}` : `${row.name} (not in library)`"
        >
          <Poster
            :idx="i"
            :src="row.local_slug ? `/api/media/${row.local_slug}/image/poster` : row.image"
            aspect="1/1"
            :width="200"
            style="border-radius: 50%"
          />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">{{ row.source }}</div>
        </component>
      </div>
    </section>

    <MetadataEditorModal
      v-if="detail"
      :media-id="detail.media_item.id"
      :show="showMetadataEditor"
      @close="onEditorClose"
    />

    <!-- Phone ⋯ target for Popular Tracks rows (play/queue/rate/navigate). -->
    <ActionSheet
      v-model:open="ttSheetOpen"
      :items="ttSheetTrack ? ttMenuItems(ttSheetTrack) : []"
      :title="ttSheetTrack?.title"
    />
  </div>
</template>

<script setup lang="ts">
import type { AlbumView, Artist, ArtistTopTrackRow, MediaDetail, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import type { DragAlbumPayload } from '~/composables/useMusicDragDrop'
import { DropdownMenuItem } from 'reka-ui'
import { useQuery, useQueryCache } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

// slug keys + addresses the detail query so it shares the Pinia Colada cache
// entry with the parent page's ['media','detail',slug] fetch — keying by
// mediaId created a second cache entry and re-ran the heaviest endpoint on
// every artist page view, sequentially after the page's own copy.
const props = defineProps<{ mediaId: number; slug: string }>()

const route = useRoute()
const { play, queue, currentTrack, playing, formatTime } = usePlayerBindings()
const radio = useRadio()

// Now-playing markers. A Popular Tracks row lights up when the playing track
// is it; a discography tile lights up when the playing track belongs to that
// album (album ids are globally unique, so an id match is unambiguous). Both
// read the shared usePlayerBindings() state, so they react live as playback advances.
function isTopTrackActive(t: ArtistTopTrackRow) {
  const id = currentTrack.value?.id
  return id != null && id === t.local_track_id
}
function isAlbumActive(al: AlbumView) {
  const albumId = currentTrack.value?.album_id
  return albumId != null && albumId > 0 && albumId === al.id
}
const { isPhone, isCompact, isCoarse } = useViewport()

// The right-flank identity block is a WIDE-desktop composition: on the
// compact band (721-1200px) a long bio would run underneath the absolute
// overlay, so tablets keep the in-flow title like phones do.
const brandInFlow = computed(() => isPhone.value || isCompact.value)
const { onDragStart, onDragEnd } = useMusicDragDrop()
// Popular Tracks context/⋯ items — the phone rows hide the star widget, so
// this menu (Rate lives in it) is the rating path there.
const trackMenuActions = useMusicActions()

const artistRatings = useRatings('artist')
const trackRatings = useRatings('track')
async function onRateArtist(id: number, v: number) {
  try { await artistRatings.set(id, v) } catch { /* rollback handled */ }
}
async function onRateTrack(id: number, v: number) {
  try { await trackRatings.set(id, v) } catch { /* rollback handled */ }
}

async function startArtistRadio() {
  await radio.startRadio({ kind: 'artist', artist_slug: route.params.slug as string })
}

const bioOpen = ref(false)
const ttExpanded = ref(false)

const { user } = useAuth()
const isAdmin = computed(() => user.value?.is_admin === true)
const showMetadataEditor = ref(false)
const queryClient = useQueryCache()

function onEditorClose() {
  showMetadataEditor.value = false
  // Edits and refreshes land server-side; drop the cached detail so the
  // page (and this component) re-reads the updated artist.
  queryClient.invalidateQueries({ key: ['media', 'detail', props.slug] })
}

interface SimilarArtistRow {
  name: string
  mbid?: string
  image?: string
  score: number
  source: string
  url?: string
  local_slug?: string
  local_artist_id?: number
}

interface SonicSimilarArtistRow {
  id: number
  name: string
  media_item_id: number
  media_item_public_id?: string
  media_slug: string
  distance: number
}

const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => mediaDetailQuery(props.slug))
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

const artistSlugForQueries = computed(() => detail.value?.media_item?.slug ?? (route.params.slug as string | undefined) ?? '')

const similarQuery = useQuery({
  key: () => ['music', 'artist', 'similar', artistSlugForQueries.value],
  query: async () => (await $heya('/api/music/artists/{slug}/similar', { path: { slug: artistSlugForQueries.value } })) as SimilarArtistRow[],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
const similar = computed<SimilarArtistRow[]>(() => similarQuery.data.value ?? [])

const sonicSimilarQuery = useQuery({
  key: () => ['music', 'artist', 'sonic-similar', artistSlugForQueries.value, { limit: 12 }],
  query: async () => ((await $heya('/api/music/artists/{slug}/sonic-similar', { path: { slug: artistSlugForQueries.value }, query: { limit: 12 } })) as { items: SonicSimilarArtistRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
const sonicSimilar = computed<SonicSimilarArtistRow[]>(() => sonicSimilarQuery.data.value ?? [])

const topTracksQuery = useQuery({
  key: () => ['music', 'artist', 'top-tracks', artistSlugForQueries.value, { limit: 25 }],
  query: async () => ((await $heya('/api/music/artists/{slug}/top-tracks', { path: { slug: artistSlugForQueries.value }, query: { limit: 25 } })) as { items: ArtistTopTrackRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
// Owned-only filter — Last.fm rows we can't play are noise on a library page.
// External links to Last.fm still live in the "Around the web" section.
// Deduped by local_track_id so "Usseewa" + "うっせぇわ" (which both resolve
// to the same recording) collapse to one rail entry.
const topTracks = computed<ArtistTopTrackRow[]>(() => {
  const seen = new Set<number>()
  const out: ArtistTopTrackRow[] = []
  for (const t of topTracksQuery.data.value ?? []) {
    if (!t.local_track_id || seen.has(t.local_track_id)) continue
    seen.add(t.local_track_id)
    out.push(t)
  }
  return out
})

const hasPlayableTopTracks = computed(() => topTracks.value.some(isTopTrackPlayable))

const artist = computed<Artist | null>(() => detail.value?.artist ?? null)
watch(artist, (a) => {
  if (a?.id && a.id > 0) artistRatings.load(a.id).catch(() => 0)
}, { immediate: true })

// Prime the per-track rating cache once the top-tracks list lands so the
// star widgets paint at correct values rather than starting at 0.
watch(topTracks, (rows) => {
  const ids = rows.filter((r) => r.local_track_id).map((r) => r.local_track_id!) as number[]
  if (ids.length) trackRatings.primeBulk(ids).catch(() => 0)
})

const albums = computed<AlbumView[]>(() => detail.value?.albums ?? [])

// Playability — a track needs a live file (TrackView.files is server-filtered
// to live files), an album needs a playable track, the artist needs a playable
// album. Missing items still render but can't be played.
function isTrackPlayable(t: TrackView) { return t.files.length > 0 }
function albumPlayable(al: AlbumView) { return al.tracks.some(isTrackPlayable) }
const artistPlayable = computed(() => albums.value.some(albumPlayable))
const playableTrackIds = computed(() => {
  const s = new Set<number>()
  for (const al of albums.value) for (const t of al.tracks) if (isTrackPlayable(t)) s.add(t.id)
  return s
})
function isTopTrackPlayable(t: ArtistTopTrackRow) {
  return !!t.local_track_id && playableTrackIds.value.has(t.local_track_id)
}

// Discography tile drag payload — album.tracks is already loaded (detail
// query), so this carries trackIds straight through and the sidebar drop
// handler skips the album-detail re-fetch.
function discogDragPayload(album: AlbumView): DragAlbumPayload {
  return {
    kind: 'album',
    title: album.title,
    artist_slug: route.params.slug as string,
    album_slug: album.slug,
    trackIds: album.tracks.filter(isTrackPlayable).map((t) => t.id),
  }
}

const artistPosterUrl = computed(() => {
  if (!detail.value?.media_item) return null
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/poster`
})
// Logotype instead of the name when the artist has a logo asset — the
// assets list in the detail payload says so up front (no probing).
const logoFailed = ref(false)
const logoUrl = computed(() => {
  if (!detail.value?.media_item) return null
  if (!detail.value.assets?.some((as) => as.asset_type === 'logo')) return null
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/logo`
})
watch(logoUrl, () => { logoFailed.value = false })
const backdropUrl = computed(() => {
  if (!detail.value?.media_item) return null
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/backdrop`
})
const backdropStyle = computed(() => (backdropUrl.value ? { backgroundImage: `url(${backdropUrl.value})` } : {}))

// Ambient extension: with the ambient background on, this artist's backdrop
// becomes the full-page layer (the hero image "extends" down the whole
// page) — the local `.hero-backdrop` hides via .ambient-extended and only
// the softened fade stays for text legibility. Off = classic scoped hero,
// untouched.
const { ambientEnabled, prefs } = useAppearance()

// Similar Artists honors the same Appearance switch as the movie/TV
// recommendation rails: off = only artists we can link into the library.
// Deduped by identity — Last.fm and ListenBrainz often both suggest the
// same artist and the raw feed rendered them twice.
const visibleSimilar = computed(() => {
  const rows = prefs.value.showUnavailableRecs
    ? similar.value
    : similar.value.filter((r) => r.local_slug)
  const seen = new Set<string>()
  return rows.filter((r) => {
    const key = r.local_slug || r.mbid || r.name.toLowerCase()
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
})
const background = useBackground()
watch([backdropUrl, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url)
  else background.clear()
}, { immediate: true })

// Tone-adaptive primary actions — sample the hero art (backdrop first,
// poster fallback) and paint the Play buttons in the artist's palette,
// same pattern as the movie/TV detail heroes. Sequence-guarded against a
// slow sample landing after the route already changed artists.
const heroToneStyle = ref<Record<string, string> | undefined>()
let heroToneSeq = 0
watch(() => backdropUrl.value || artistPosterUrl.value, (src) => {
  const seq = ++heroToneSeq
  if (!src) { heroToneStyle.value = undefined; return }
  sampleImageTone(src).then((t) => {
    if (seq !== heroToneSeq) return
    heroToneStyle.value = t ? { background: t.main, color: t.ink } : undefined
  })
}, { immediate: true })

const totalAlbums = computed(() => albums.value.length)
const totalTracks = computed(() => albums.value.reduce((sum, al) => sum + al.tracks.length, 0))

// Hero aside: the first few aliases, deduped against the display name
// (MusicBrainz often lists the name itself as an alias). Full list lives
// in the tooltip.
const heroAliases = computed(() => {
  const name = artist.value?.name?.toLowerCase()
  const list = (artist.value?.aliases ?? []).filter((a) => a.toLowerCase() !== name)
  if (!list.length) return ''
  const shown = list.slice(0, 3).join(' · ')
  return list.length > 3 ? `${shown} +${list.length - 3}` : shown
})


const KIND_ORDER = ['album', 'ep', 'single', 'compilation', 'live', 'soundtrack', 'remix', 'demo', 'other']
const KIND_LABEL: Record<string, string> = {
  album: 'Albums',
  ep: 'EPs',
  single: 'Singles',
  compilation: 'Compilations',
  live: 'Live',
  soundtrack: 'Soundtracks',
  remix: 'Remixes',
  demo: 'Demos',
  other: 'Other',
}

const groupedDiscography = computed(() => {
  const byKind = new Map<string, AlbumView[]>()
  for (const al of albums.value) {
    const kind = (al.album_type || 'album').toLowerCase()
    const bucket = KIND_LABEL[kind] ? kind : 'other'
    if (!byKind.has(bucket)) byKind.set(bucket, [])
    byKind.get(bucket)!.push(al)
  }
  for (const list of byKind.values()) {
    list.sort((a, b) => {
      const ay = parseInt(a.year || '0', 10) || 0
      const by = parseInt(b.year || '0', 10) || 0
      return by - ay
    })
  }
  return KIND_ORDER
    .filter((k) => byKind.has(k))
    .map((kind) => ({ kind, label: KIND_LABEL[kind] ?? kind, albums: byKind.get(kind)! }))
})

// Per-album cover tone: the always-visible discography Play button wears each
// record's own sampled palette (semi-transparent glass over the art — see
// .discog-play). Cheap: sampleImageTone() memoizes per URL and the covers are
// already HTTP-cached by the Poster render, so a whole grid samples once.
// Declared AFTER groupedDiscography — the immediate watch reads it at setup.
const albumTones = reactive<Record<number, { main: string; ink: string }>>({})
function discogPlayStyle(album: { id: number }): Record<string, string> | undefined {
  const t = albumTones[album.id]
  return t ? { '--btn-tone': t.main, color: t.ink } : undefined
}
watch(groupedDiscography, (groups) => {
  if (!import.meta.client || !groups) return
  for (const g of groups) {
    for (const al of g.albums) {
      if (albumTones[al.id] || !albumPlayable(al)) continue
      const url = useAlbumCoverUrl(route.params.slug as string, al.slug)
      if (url) sampleImageTone(url).then((t) => { if (t) albumTones[al.id] = { main: t.main, ink: t.ink } })
    }
  }
}, { immediate: true })

// Birthplace can come through as a Wikidata QID we don't yet resolve; only
// show when it's a human-readable token.
const originLabel = computed(() => {
  const bp = artist.value?.birthplace ?? ''
  if (!bp) return ''
  if (/^Q\d+$/.test(bp)) return ''
  return bp
})

const lifecycleLabel = computed(() => {
  const a = artist.value
  if (!a) return ''
  const start = a.begin_year ? String(a.begin_year) : (a.begin_date || '')
  const end = a.deathday || a.end_date || (a.ended ? '?' : '')
  if (!start && !end) return ''
  if (a.artist_type === 'Group') {
    if (start && end) return `${start}–${end}`
    if (start) return `since ${start}`
    return end
  }
  if (start && a.deathday) return `${start} – ${a.deathday}`
  return start
})

// ── "Around the web" dropdown ───────────────────────────────────────────
// The MusicBrainz url-rel list is a long tail (60+ links, half of them
// labeled "other databases" / "purchase for download"). Grouped into a
// hero dropdown: rows are labeled by SITE (hostname), with the rel type
// as a muted suffix only where it adds meaning.

interface AtwLink { label: string; sub?: string; url: string }

function linkCategory(type: string): string {
  const t = type.toLowerCase()
  if (t.includes('official') || t.includes('fanpage') || t.includes('bbc') || t.includes('discography') || t === 'blog' || t === 'image') return 'Official'
  if (t.includes('streaming') || t.includes('purchase') || t.includes('soundcloud') || t.includes('youtube music')) return 'Listen & Buy'
  if (t.includes('social') || t === 'myspace' || t.includes('video channel') || t === 'youtube' || t.includes('online community')) return 'Social'
  if (t === 'bandsintown' || t === 'songkick' || t === 'setlistfm') return 'Live'
  return 'Reference'
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

// Types that describe the link better than the bare hostname does.
const ATW_MEANINGFUL_TYPES = new Set([
  'official homepage', 'fanpage', 'discography page', 'lyrics', 'image',
  'purchase for download', 'purchase for mail-order', 'free streaming', 'streaming',
])

const CATEGORY_ORDER = ['Official', 'Listen & Buy', 'Social', 'Live', 'Reference', 'Wikipedia']

const linkGroups = computed(() => {
  const buckets = new Map<string, AtwLink[]>()
  const seen = new Set<string>()
  for (const l of (artist.value?.urls ?? [])) {
    if (!l.url || seen.has(l.url)) continue
    seen.add(l.url)
    const type = l.type || 'link'
    const cat = linkCategory(type)
    const entry: AtwLink = {
      label: hostOf(l.url),
      sub: ATW_MEANINGFUL_TYPES.has(type.toLowerCase()) ? type : undefined,
      url: l.url,
    }
    if (!buckets.has(cat)) buckets.set(cat, [])
    buckets.get(cat)!.push(entry)
  }
  const wiki = Object.entries(artist.value?.wikipedia_links ?? {})
    .map(([lang, url]) => ({ label: lang.toUpperCase(), sub: 'wikipedia', url }))
    .sort((a, b) => a.label.localeCompare(b.label))
  if (wiki.length) buckets.set('Wikipedia', wiki)

  return CATEGORY_ORDER
    .filter((c) => buckets.has(c))
    .map((c) => ({
      label: c,
      links: buckets.get(c)!.sort((a, b) => a.label.localeCompare(b.label)),
    }))
})

const linkTotal = computed(() => linkGroups.value.reduce((n, g) => n + g.links.length, 0))

function formatBigInt(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return n.toLocaleString()
}

function trackFromAlbum(album: AlbumView, t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: album.title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.id}/stream`,
    album_id: album.id,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, album.slug) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAlbum(album: AlbumView, shuffle: boolean) {
  let tracks = album.tracks.filter(isTrackPlayable).map((t) => trackFromAlbum(album, t))
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

async function playAll(shuffle: boolean) {
  let tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) if (isTrackPlayable(t)) tracks.push(trackFromAlbum(al, t))
  }
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

function addAllToQueue() {
  const tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) if (isTrackPlayable(t)) tracks.push(trackFromAlbum(al, t))
  }
  queue.value = [...queue.value, ...tracks]
}

function topTrackToTrack(t: ArtistTopTrackRow): Track {
  return {
    id: t.local_track_id!,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    stream_url: `/api/music/tracks/${t.local_track_id}/stream`,
    album_id: t.local_album_id ?? 0,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, t.local_album_slug ?? '') ?? undefined,
  }
}

// --- Popular Tracks menu / phone action sheet -------------------------------
function ttMenuItems(t: ArtistTopTrackRow) {
  if (!t.local_track_id) return []
  return trackMenuActions.forTrack({
    id: t.local_track_id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    artist_slug: artistSlugForQueries.value || undefined,
    album_slug: t.local_album_slug,
    available: isTopTrackPlayable(t),
  })
}

function discogMenuItems(album: AlbumView) {
  return trackMenuActions.forAlbum({
    id: album.id,
    title: album.title,
    artist_id: artist.value?.id,
    artist_name: artist.value?.name ?? '',
    artist_slug: artistSlugForQueries.value || route.params.slug as string,
    album_slug: album.slug,
    available: albumPlayable(album),
  })
}

const ttSheetOpen = ref(false)
const ttSheetTrack = ref<ArtistTopTrackRow | null>(null)
function openTtSheet(t: ArtistTopTrackRow) {
  ttSheetTrack.value = t
  ttSheetOpen.value = true
}

// Row click = play, everywhere. The interactive children (title/album
// links, stars, hover-play, the phone ⋯) all @click.stop, so a click that
// reaches the row really did land on empty row space.
function onTtRowTap(t: ArtistTopTrackRow) {
  if (isTopTrackPlayable(t)) void playTopTrack(t)
}

async function playTopTrack(t: ArtistTopTrackRow) {
  if (!isTopTrackPlayable(t)) return
  const built = topTrackToTrack(t)
  queue.value = [built]
  await play(built)
}

async function playTopAll(shuffle: boolean) {
  let owned = topTracks.value.filter(isTopTrackPlayable).map(topTrackToTrack)
  if (!owned.length) return
  if (shuffle) owned = [...owned].sort(() => Math.random() - 0.5)
  queue.value = owned
  await play(owned[0]!)
}

if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload?.media_item_id === props.mediaId) {
      queryClient.invalidateQueries({ key: ['media', 'detail', props.slug] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'similar', artistSlugForQueries.value] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'sonic-similar', artistSlugForQueries.value, { limit: 12 }] })
      queryClient.invalidateQueries({ key: ['music', 'artist', 'top-tracks', artistSlugForQueries.value, { limit: 25 }] })
    }
  })
  onBeforeUnmount(() => { off() })
}
</script>

<style scoped>
.artist-page { padding-bottom: 80px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 32px 40px; }

/* Inner sections use side padding from `.page-pad` but skip the 80px bottom
   gap so the rails stack tight on this page. The page-level breathing room
   comes from `.artist-page { padding-bottom: 80px }`. */
.artist-section {
  padding: 18px 40px 0;
}
@media (max-width: 1100px) {
  .artist-section { padding: 16px 24px 0; }
}

/* Hero ============================================================ */
.hero {
  position: relative;
  min-height: 460px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
}
.hero-backdrop {
  position: absolute;
  inset: 0;
  background-size: cover;
  background-position: center 25%;
  z-index: 0;
  filter: saturate(1.05);
}
.hero-fade {
  position: absolute;
  inset: 0;
  /* scrim over the backdrop photo — stays literal black; already fades to
     the theme-aware var(--bg-0) where it meets the page canvas */
  background:
    linear-gradient(180deg, rgba(0,0,0,0.05) 0%, rgba(0,0,0,0.55) 55%, var(--bg-0) 100%),
    linear-gradient(90deg, rgba(0,0,0,0.45) 0%, transparent 60%);
  z-index: 1;
}
/* Ambient extension: the AmbientBackdrop layer shows this artist's backdrop
   full-page (see the background watcher), so the local copy hides — its
   different crop would seam at the hero edges — and the fade softens its
   transition into the page canvas so the artwork continues past the hero
   bottom instead of ending at solid var(--bg-0). The literal black scrim
   (legibility over the photo) stays put. */
.hero.ambient-extended .hero-backdrop { display: none; }
.hero.ambient-extended .hero-fade { display: none; }
/* Without a local hero image, the 460px band is mostly empty air pushing
   the content down — the ambient layer carries the art full-page anyway,
   so let the content start higher. Hero mode (ambient off) keeps the full
   stage for its backdrop. */
.hero.ambient-extended { min-height: 340px; }
/* Ambient desktop: top-align the whole stage on one line — the left
   column (pills/bio/stats) and the right identity block (logo/name) both
   start at 56px, just under the corner dropdown. Hero mode keeps the
   classic bottom-anchored-over-photo composition. */
@media (min-width: 720.02px) {
  /* flex-start on .hero itself too — its base align-items: flex-end would
     otherwise keep bottom-anchoring the content block and the two columns
     would top out ~30px apart. */
  .hero.ambient-extended { align-items: flex-start; }
  .hero.ambient-extended .hero-content {
    align-items: flex-start;
    padding-top: 56px;
  }
  .hero.ambient-extended .hero-left { align-self: flex-start; }
  .hero.ambient-extended .hero-brand { top: 56px; bottom: auto; }
}
.hero-content {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-end;
  gap: 28px;
  padding: 26px 40px 28px;
  width: 100%;
}
.hero-left { flex-shrink: 0; align-self: flex-end; }
.hero-poster {
  width: 200px;
  height: 200px;
  border-radius: 50%;
  box-shadow: 0 22px 48px rgb(var(--shade) / 0.7), 0 0 0 1px rgb(var(--ink) / 0.06);
}
.hero-meta { flex: 1; min-width: 0; }
.hero-title {
  font-size: clamp(44px, 6.6vw, 76px);
  font-weight: 800;
  color: var(--fg-0);
  line-height: 0.96;
  margin-bottom: 10px;
  letter-spacing: -0.025em;
  text-shadow: 0 2px 24px rgba(0,0,0,0.55); /* legibility shadow over hero photo — stays literal */
}
/* Logotype variant — the image IS the title. Height-capped so wide logos
   don't dwarf the meta column; drop-shadow stands in for the text halo
   (logos are transparent PNGs over arbitrary art). */
.hero-title-logo { text-shadow: none; }
.hero-logo {
  display: block;
  max-height: 120px;
  max-width: min(520px, 90%);
  width: auto;
  height: auto;
  object-fit: contain;
  filter: drop-shadow(0 2px 12px rgb(var(--shade) / 0.6));
}
.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}
.tag-chip {
  display: inline-flex;
  padding: 3px 10px;
  border-radius: 999px;
  /* badge painted over the hero photo — stays literal glass */
  background: rgba(255,255,255,0.08);
  border: 1px solid rgba(255,255,255,0.10);
  font-size: 11px;
  color: var(--fg-0);
  text-decoration: none;
  text-transform: lowercase;
  transition: all 0.12s;
  backdrop-filter: blur(6px);
}
.tag-chip:hover {
  background: var(--gold-soft);
  color: var(--gold);
  border-color: var(--gold-soft);
}
.hero-bio {
  color: var(--fg-1);
  line-height: 1.5;
  font-size: 13px;
  max-width: 72ch;
  margin: 0;
  text-shadow: 0 1px 8px rgba(0,0,0,0.5); /* legibility shadow over hero photo — stays literal */
}
.hero-bio.collapsed {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.hero-bio-toggle {
  display: inline-flex;
  align-items: center;
  margin-top: 4px;
  font-size: 12px;
  color: var(--gold);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
}
.hero-bio-toggle:hover { color: var(--gold-bright); }
.hero-bio-toggle::before { content: '▾ '; margin-right: 4px; opacity: 0.7; }

.hero-stats {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--fg-1);
  font-family: var(--font-mono);
  letter-spacing: 0.02em;
  text-shadow: 0 1px 8px rgba(0,0,0,0.5); /* legibility shadow over hero photo — stays literal */
}
.hero-stats-stars {
  display: inline-flex;
  margin-right: 4px;
}
.stat-dot { color: var(--fg-3); }
.stat { color: var(--fg-1); }
.hero-ext { margin-top: 10px; }

/* "Around the web" dropdown anchor — desktop: hero top-right corner.
   Phone flips it to an in-flow full-width bar (media block below). */
.hero-atw {
  position: absolute;
  top: 18px;
  right: 24px;
  z-index: 4;
}

/* Desktop identity block — logotype/name on the hero's right flank,
   holding clear of the floating action cluster at the bottom-right.
   Right-aligned so it reads as a brand mark, not a second column. */
.hero-brand {
  position: absolute;
  right: 32px;
  bottom: 110px;
  z-index: 3;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
  max-width: min(460px, 38%);
}
.hero-brand .hero-title { margin-bottom: 4px; }
.hero-brand .hero-aka { margin: 0; max-width: 100%; }

.hero-floating-actions {
  position: absolute;
  bottom: 28px;
  right: 32px;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 10px;
}
.hero-round {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  /* floating buttons painted over the hero photo — stays literal */
  border: 1px solid rgba(255,255,255,0.12);
  background: rgba(0,0,0,0.4);
  color: var(--fg-0);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
  transition: background 0.15s, transform 0.1s, color 0.15s;
}
.hero-round:hover { background: rgba(0,0,0,0.55); transform: scale(1.05); }
.hero-round:active { transform: scale(0.95); }
.hero-round-primary {
  width: 58px;
  height: 58px;
  /* Tone-follow: the inline heroToneStyle paints the artist's sampled
     palette over this gold fallback; the 0.9s glide covers the swap. */
  background: var(--gold);
  color: var(--bg-0);
  border-color: transparent;
  box-shadow: 0 10px 24px var(--gold-glow);
  transition: transform 0.1s, filter 0.15s,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
/* Brightness pop for hover (works on any sampled tone — the inline tone
   style outranks hover rules). The background re-assert is for the
   UN-toned fallback: without it, .hero-round:hover's dark wash (higher
   specificity than .hero-round-primary) would swallow the gold. */
.hero-round-primary:hover { background: var(--gold); filter: brightness(1.12); }
.hero-round:disabled { opacity: 0.4; cursor: default; pointer-events: none; }

/* Ambient-extended: the hero text sits on the theme's ambient wash, not on
   the raw photo — swap the literal-black legibility shadows for theme-aware
   halos, and re-coat chips/buttons in theme glass so light mode stops
   rendering them as dark smudges (they were locked-dark rgba). */
.hero.ambient-extended .hero-title { text-shadow: 0 2px 20px rgb(var(--shade) / 0.30), 0 0 14px var(--bg-1); }
.hero.ambient-extended .hero-bio,
.hero.ambient-extended .hero-stats,
.hero.ambient-extended .hero-kind { text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }
.hero.ambient-extended .tag-chip {
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  border-color: var(--border);
  box-shadow: var(--shadow-el);
}
.hero.ambient-extended .tag-chip:hover {
  background: var(--gold-soft);
  color: var(--gold);
  border-color: var(--gold-soft);
}
.hero.ambient-extended .hero-round:not(.hero-round-primary) {
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  border-color: var(--border);
  color: var(--fg-1);
  box-shadow: var(--shadow-el);
}
.hero.ambient-extended .hero-round:not(.hero-round-primary):hover {
  background: var(--bg-3);
  color: var(--fg-0);
}
.hero-missing {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--bad); margin-right: 6px;
}

/* `.hero-floating-actions` is position:absolute, so it and `.hero-meta`'s
   text never negotiate width with each other — on a long-enough stats line
   (or a narrow-enough hero) the text runs right under the button cluster
   and gets visually clipped by it (bug: gold Play circle sitting on top of
   "136.4K plays", "Born 1991" partially hidden). `.hero-stats` already
   flex-wraps; reserving the button cluster's footprint as padding-right
   makes it wrap *before* reaching that zone instead of running under it.
   The offset is the actions cluster's rendered width (5 round buttons +
   4 gaps ≈ 274px) minus the 8px delta between `.hero-content`'s
   padding-right (40) and the actions' `right` inset (32), plus a small
   visual gap — same reservation on `.hero-ext` since external-link chips
   can land in the same bottom-right band when the stats line is short.
   Desktop-only (>1200px): both phone (<=720px) and the foldable/compact band
   (720.02-1200px) stack the actions row below the meta text as static flow
   (see the two media blocks below), so there's nothing to collide with there
   and no reservation is needed. Above 1200px the actions stay absolute
   bottom-right and this reservation keeps the stats/links clear of them; it's
   inert at very wide widths where the text never reaches the cluster anyway. */
@media (min-width: 1201px) {
  .hero-stats,
  .hero-ext {
    padding-right: 290px;
  }
}

/* Stars onto their own line for phone AND the foldable/compact band
   (<=1200px). The star widget and the dot-separated stats used to share one
   wrapping flex row and shuffle unpredictably as the width changed ("punking
   each other around"); pinning the widget to a full row break makes the
   ratings a clean first line with the stats flowing beneath. The separator dot
   that immediately follows the widget would otherwise lead the stats line, so
   it's dropped. Desktop (>1200px) keeps stars + stats inline, untouched. */
@media (max-width: 1200px) {
  .hero-stats-stars { flex-basis: 100%; margin-right: 0; }
  .hero-stats-stars + .stat-dot { display: none; }
}

/* Foldable / compact band (720.02-1200px): the hero keeps poster-beside-meta,
   but the floating actions drop out of their absolute bottom-right anchor into
   a static full-width row of their own below the meta — same "buttons on their
   own line" treatment as phone, so play/shuffle/queue/radio/edit stop
   colliding with the stats. `.hero` flips to a column so the (still absolute)
   backdrop/fade stay put while content + actions stack in flow. */
@media (min-width: 720.02px) and (max-width: 1200px) {
  .hero { flex-direction: column; min-height: 0; }
  .hero-floating-actions {
    position: static;
    align-self: stretch;
    justify-content: flex-start;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 6px;
    padding: 0 40px 26px;
  }
}

/* Popular Tracks ================================================== */
.top-tracks {}
/* Home-rail header shape: title leads, count and actions huddle up next
   to it (flex-start beats heya.css's global space-between, which spread
   Play mid-page, Shuffle far-right, and left the count orphaned at the
   edge). */
.section-row-head {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  margin-bottom: 10px;
}
/* Section titles sit over the ambient-extended wash — home's title
   treatment: 700 weight + the triple halo, readable on any art. */
.artist-section :deep(.section-title-lg),
.artist-section .section-title-lg {
  font-weight: 700;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
/* Inline count chip beside the title (was pushed to the far edge by the
   global space-between — read as a stray number). */
.artist-section .section-row-head .more {
  margin-left: 0;
  font-size: 11px;
  color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.section-row-head .more {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin-left: auto;
}
.tt-head { margin-bottom: 8px; }

.pill-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 14px;
  border-radius: 999px;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: filter 0.12s,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.pill-btn:hover { filter: brightness(1.1); }
.pill-btn:disabled { opacity: 0.4; cursor: not-allowed; filter: none; }
.pill-btn-ghost {
  background: rgb(var(--ink) / 0.06);
  color: var(--fg-1);
}
.pill-btn-ghost:hover { background: rgb(var(--ink) / 0.10); }

.tt-list {
  list-style: none;
  margin: 0;
  display: flex;
  flex-direction: column;
  /* Glass panel — same surface TrackList wears, so the numbered rows stay
     readable over the ambient-extended artwork. */
  background: color-mix(in oklab, var(--bg-2) 76%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
  padding: 6px 8px;
}
.tt-row {
  display: grid;
  grid-template-columns: 36px 1fr auto 50px;
  align-items: center;
  gap: 14px;
  padding: 5px 10px;
  border-radius: var(--r-sm);
  transition: background 0.12s;
  cursor: pointer; /* whole row plays (onTtRowTap) */
  min-height: 32px;
}
.tt-row:hover { background: rgb(var(--ink) / 0.04); }
.tt-row:hover .tt-rank { opacity: 0; }
.tt-row:hover .tt-hover-play { opacity: 1; }
.tt-row-missing { opacity: 0.55; }
/* Currently-playing row — gold wash + gold title, matching TrackList's
   .tl-active treatment. The VuMeter in the leader already animates. */
.tt-row-active { background: var(--gold-soft); }
.tt-row-active:hover { background: var(--gold-soft); }
.tt-row-active .tt-title { color: var(--gold); }
.tt-vu { margin-left: auto; }
.tt-missing-icon { color: var(--bad); }
.tt-leader {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  height: 22px;
}
.tt-rank {
  font-family: var(--font-mono);
  color: var(--fg-3);
  font-size: 12px;
  transition: opacity 0.12s;
}
.tt-hover-play {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.12s, filter 0.12s;
}
.tt-hover-play:hover { filter: brightness(1.1); }
.tt-hover-play.tt-hover-play-disabled {
  background: rgb(var(--ink) / 0.06);
  color: var(--fg-3);
  cursor: default;
}
.tt-external .tt-title { color: var(--fg-2); }
.tt-external .tt-album { color: var(--fg-3); }
.tt-meta {
  min-width: 0;
  overflow: hidden;
  display: flex;
  align-items: baseline;
  gap: 6px;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.tt-title {
  font-size: 13px;
  color: var(--fg-0);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tt-title-link { text-decoration: none; }
.tt-title-link:hover { color: var(--gold); text-decoration: underline; }
.tt-album-sep { color: var(--fg-3); font-size: 11px; }
.tt-album {
  font-size: 12px;
  color: var(--fg-2);
  text-decoration: none;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tt-album:hover { color: var(--gold); }
.tt-album-missing { font-style: italic; color: var(--fg-3); opacity: 0.7; }
.tt-stars { display: inline-flex; }
/* Phone-only ⋯ (see the media query below) — desktop keeps stars + hover
   play and doesn't render an extra affordance. */
.tt-phone-more { display: none; }
.tt-duration {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
  text-align: right;
}
.tt-more {
  margin-top: 6px;
  background: none;
  border: none;
  color: var(--gold);
  cursor: pointer;
  font-size: 12px;
  padding: 4px 10px;
}
.tt-more:hover { color: var(--gold-bright); }

/* Members / Groups ================================================ */
.member-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.member-chip {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  border-radius: var(--r-sm);
  /* Glass, not ink-tint — the chips sit over the ambient art. */
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  min-width: 140px;
  text-decoration: none;
  color: inherit;
}
.member-linked { transition: background 0.15s, border-color 0.15s; }
.member-linked:hover { background: var(--bg-3); border-color: var(--border-strong); }
.member-linked:hover .member-name { color: var(--gold); }
.member-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  flex-shrink: 0;
}
.member-text { min-width: 0; }
.member-name { font-size: 13px; color: var(--fg-0); font-weight: 600; transition: color 0.15s; }
.member-years {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 1px;
  letter-spacing: 0.03em;
}

/* Discography ===================================================== */
.discog-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 14px;
}
.discog-tile { text-decoration: none; color: inherit; display: block; }
.discog-art-wrap { position: relative; }
.discog-art { border-radius: var(--r-md); box-shadow: 0 8px 18px rgb(var(--shade) / 0.45); }
.discog-missing .discog-art { filter: grayscale(1); opacity: 0.55; }
.discog-play {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 0;
  /* Always visible — was hover-reveal (opacity 0), which on touch needed a
     throwaway first tap to "hover" the button in before the real tap
     registered, so opening an album took two taps. Now it's a permanent
     affordance: a semi-transparent glass disc tinted with the album's own
     sampled cover colour (--btn-tone, set inline; --gold until the sample
     lands), so the artwork still reads through and each record wears its
     palette. Tap the disc = play; tap anywhere else on the tile = open. */
  background: color-mix(in srgb, var(--btn-tone, var(--gold)) 52%, transparent);
  color: var(--bg-0);
  -webkit-backdrop-filter: blur(8px) saturate(140%);
  backdrop-filter: blur(8px) saturate(140%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 1;
  transform: none;
  transition: background 0.18s, transform 0.15s, box-shadow 0.18s;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.38); /* over artwork — literal */
}
/* Pointer/keyboard affordance: solidify the tint and lift on hover/focus. */
.discog-tile:hover .discog-play,
.discog-play:focus-visible {
  background: color-mix(in srgb, var(--btn-tone, var(--gold)) 94%, transparent);
  transform: scale(1.08);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.45);
}
/* Now-playing album — gold ring on the art + gold title, with an animated
   VuMeter badge pinned top-left of the cover. */
.discog-active .discog-art { box-shadow: 0 8px 18px rgb(var(--shade) / 0.45), 0 0 0 2px var(--gold); }
.discog-active .discog-title { color: var(--gold); }
.discog-nowplaying {
  position: absolute;
  top: 8px;
  left: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 6px;
  border-radius: var(--r-xs);
  background: rgba(0,0,0,0.6); /* badge painted over the album cover — stays literal */
  backdrop-filter: blur(6px);
}
.discog-meta { margin-top: 8px; padding: 0 2px; }
.discog-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.discog-sub {
  font-size: 11px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  margin-top: 2px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.dot { color: var(--fg-3); }

/* Similar rails =================================================== */
.similar-row {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: 130px;
  gap: 16px;
  overflow-x: auto;
  padding-bottom: 8px;
  scroll-snap-type: x proximity;
}
.similar-tile {
  text-align: center;
  text-decoration: none;
  color: inherit;
  scroll-snap-align: start;
}
.similar-tile.similar-external { cursor: default; opacity: 0.7; }
.similar-tile.similar-external:hover { opacity: 1; }
.similar-tile-name {
  margin-top: 8px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.similar-tile-source {
  margin-top: 2px;
  font-size: 9px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
}

/* Aliases — hero aside, hanging slightly off the title's left edge like
   a signature line. */
.hero-aka {
  margin: -4px 0 10px 3ch;
  font-size: 12px;
  font-style: italic;
  color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hero-aka-label {
  font-family: var(--font-mono);
  font-style: normal;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-right: 4px;
}

/* Responsive: stack hero poster + meta on narrow screens. Aligned to the
   720px phone convention (docs/ui.md "Responsive conventions") — was 700px.
   Centering + the `.hero` min-height reset are the only additions beyond
   that rename; desktop and the rest of this component are untouched. */
@media (max-width: 720px) {
  /* `.hero-floating-actions` is a flex sibling of `.hero-content` (both
     direct children of `.hero`), not nested inside it — `.hero` itself
     needs to switch to a column too, or the actions float beside the
     content instead of wrapping below it. */
  .hero { min-height: 0; flex-direction: column; }
  /* Dropdown becomes an in-flow bar across the top of the hero. */
  .hero-atw {
    position: static;
    width: 100%;
    padding: 14px 20px 0;
    display: flex;
  }
  .hero-content { flex-direction: column; align-items: center; text-align: center; gap: 14px; padding: 20px 20px 22px; }
  .hero-left { align-self: center; }
  .hero-poster { width: 120px; height: 120px; }
  /* Centered phone hero: the aka signature indent reads as misalignment. */
  .hero-aka { margin-left: 0; }
  .hero-logo { margin: 0 auto; }
  .hero-meta { width: 100%; }
  .tag-row { justify-content: center; }
  .hero-stats { justify-content: center; }
  /* Stars sit on their own full-width line (shared <=1200px rule) — centre the
     widget within it to match the rest of the centred phone hero. */
  .hero-stats-stars { justify-content: center; }
  .hero-ext :deep(.ext-links) { justify-content: center; }
  .hero-floating-actions { position: static; justify-content: center; flex-wrap: wrap; margin-top: 4px; }
  .hero-floating-actions .hero-round { width: 44px; height: 44px; }
  .hero-floating-actions .hero-round-primary { width: 56px; height: 56px; }
  /* Desktop `.hero` bottom-aligns its row children (align-items: flex-end);
     after the column flip above that axis becomes horizontal, shoving this
     shelf-sized row against the right edge (the edit button rendered half
     off-screen). Center it on its own axis instead. */
  .hero-floating-actions { align-self: center; gap: 14px; }
  /* The metadata editor is a desktop-sized surface — no entry point on
     phones (same call as the album page). */
  .hero-edit { display: none; }

  /* Popular Tracks: the 5-star widget ate the title column (titles
     truncated to a few characters at 390px). Ratings are hidden on phone —
     the ⋯ ActionSheet / long-press menu carries Rate (plus play/queue) —
     and the freed column plus taller rows give the text room to breathe.
     Row tap plays (no hover-play on touch). */
  .tt-stars { display: none; }
  .tt-row { grid-template-columns: 32px 1fr max-content 44px; gap: 8px; padding: 8px 4px; min-height: 52px; }
  .tt-phone-more {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    background: transparent;
    border: 0;
    color: var(--fg-2);
    cursor: pointer;
  }
  .tt-phone-more:active { color: var(--gold); }

  /* Two-line rows: title on its own line, album underneath (in place of the
     desktop "Title · Album" single line) — more breathing room and the
     album no longer competes with the title for the same line width. The
     markup order (title → sep → album) is unchanged; only the phone-only
     layout of `.tt-meta` and its children changes here. */
  .tt-meta {
    flex-direction: column;
    align-items: flex-start;
    gap: 3px;
    white-space: normal;
  }
  .tt-title { white-space: nowrap; }
  .tt-album-sep { display: none; }
  .tt-album {
    display: block;
    width: 100%;
    font-size: 12px;
    color: var(--fg-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Discography: one desktop column (170px min) stretched full-width on a
     390px phone — drop to the same dense-grid convention as `.grid-posters`
     (docs/ui.md / heya.css) so multiple album tiles fit per row. */
  .discog-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
}
</style>

<!-- "Around the web" dropdown — the AppMenu content is portaled to <body>
     and the trigger renders inside the AppMenu child component, so none of
     these rules can live in the scoped block (docs/ui.md gotcha #2). -->
<style>
.atw-trigger {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 11px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  font-size: 11px;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  color: var(--fg-1);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.atw-trigger:hover,
.atw-trigger[data-state="open"] { background: var(--bg-3); color: var(--fg-0); }
.atw-count {
  font-size: 10px;
  color: var(--fg-2);
}

/* Long list — scroll inside the surface. */
.atw-scroll {
  max-height: min(55vh, 480px);
  overflow-y: auto;
}
.atw-item {
  display: flex;
  align-items: center;
  gap: 10px;
}
.atw-host {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.atw-type {
  flex-shrink: 0;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  letter-spacing: 0.04em;
}

/* Phone: the hero-atw wrapper is a full-width in-flow bar — stretch the
   trigger across it. (Unscoped like the rest: the trigger element is
   rendered by AppMenu, out of scoped-CSS reach.) */
@media (max-width: 720px) {
  .hero-atw .atw-trigger {
    width: 100%;
    justify-content: center;
  }
}
</style>
